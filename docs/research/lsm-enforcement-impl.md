# Phase 6 implementation plan: eBPF LSM enforcement (block mode)

> **Status:** implementation plan (2026-07-13). Implementation authorized by
> the maintainer, overriding the evidence gates in
> [`lsm-enforcement.md`](lsm-enforcement.md) for planning/build purposes. The
> two product invariants survive the override and are non-negotiable:
> **fail-open on every ambiguity** and **enforcement OFF by default** at every
> layer (Helm, collector, sensor, kernel map state).
>
> Plan only — no `bpf/` code changes ship with this document.

> **Implementation update (2026-07-14):** the original Phase 6 design below
> was subsequently hardened. `struct event` now carries `dirfd` for
> mount-namespace-aware relative-path resolution; deny maps use composite
> `{cgroup_id, literal}` keys; verdict JSON is keyed by service; and exec/file
> LSM hooks both use `bpf_d_path`. Where the historical plan conflicts with
> [`../enforcement.md`](../enforcement.md), the shipped documentation wins.

This plan is grounded in the code as it exists today: `bpf/goodman.bpf.c`
(three tracepoint families, ring buffer, `watched_pids`), `bpf/goodman.h`
(`struct event`, the wire-layout lockstep rule), `internal/loader/loader.go`
(cilium/ebpf v0.16.0, tracepoint attach, no-root object test),
`internal/diff/diff.go` (`action: alert|warn`, `block` rejected loudly,
`WouldBlock` + `goodman_enforce_would_block_total`), `cmd/sensor/main.go`
(non-blocking hot path, coverage loop that already lists namespaces/pods via
the in-cluster API), `internal/coverage/kube.go` (namespace-label listing we
will extend), and `scripts/preflight.sh` (existing warn-level LSM checks).

---

## 1. Original decision: `struct event` unchanged

Enforcement is implemented with **new maps and new programs only**. The wire
struct crossing the kernel/user boundary is untouched:

- No field is added, removed, or reordered in `struct event` /
  `model.RawEvent`. `internal/model/types_test.go` passes unedited.
- Deny telemetry reuses the existing ring buffer and the existing
  `struct event` shape. We add **three new `enum event_type` values**
  (`EVENT_DENY_FILE_OPEN = 4`, `EVENT_DENY_CONNECT = 5`,
  `EVENT_DENY_EXEC = 6`) mirrored in `internal/model/types.go`
  (`EventType` consts + `String()`), in the same commit. An enum value is a
  *value* of the existing `__u8 type` field — it is not a layout change and
  does not touch the size/offset assertions.
- The `Attributed` JSON type (sensor→collector HTTP, not the kernel wire
  struct) gains a `Denied bool` field. That is free to change.

Everything else enforcement needs — kill switch, scope, verdicts — lives in
new BPF maps written only by user space.

## 2. BPF maps (exact definitions)

All maps are defined in `bpf/goodman.bpf.c` next to the existing
`events`/`watched_pids`/`drops` maps. All are written **only** by the sensor;
the kernel programs only read them (except the drop counter). Every map is
sized small and fixed; keys are fixed-size so hash lookups are verifier-trivial.

```c
/* Dead-man kill switch. value = enforcement deadline in bpf_ktime_get_ns()
 * (CLOCK_MONOTONIC) nanoseconds. Enforcement is active only while
 * now < deadline. 0 (the load-time default for an ARRAY) = off.
 * The sensor heartbeats now+TTL (TTL ~10s) every ~1s while enforcement is
 * enabled; if the sensor dies, hangs, or the collector is unreachable, the
 * deadline lapses and every hook returns allow. */
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __type(key, __u32);
    __type(value, __u64);
    __uint(max_entries, 1);
} enforce_deadline SEC(".maps");

/* Opt-in scope: cgroup v2 ids (inode number of the cgroup dir) that are
 * subject to enforcement. Populated by the sensor only for pods in
 * namespaces labeled goodman.io/enforce=enabled. Absent entry -> allow. */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u64);   /* cgroup id */
    __type(value, __u8);  /* 1 */
    __uint(max_entries, 4096);
} enforced_cgroups SEC(".maps");

/* file_open deny verdicts: full resolved path, NUL-terminated and
 * zero-padded to PATH_MAX_LEN (256, already a power of two). Exact match
 * only — no prefixes, no hashes. */
struct deny_path {
    char path[PATH_MAX_LEN];
};
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, struct deny_path);
    __type(value, __u8);
    __uint(max_entries, 1024);
} deny_open SEC(".maps");

/* connect deny verdicts. port is host byte order; port == 0 is the any-port
 * wildcard (checked with a second lookup). addr is 4 bytes (v4, network
 * order) or 16 bytes (v6), zero-padded. */
struct deny_addr {
    __u8  family;   /* AF_INET / AF_INET6 */
    __u8  _pad;
    __u16 port;     /* host order; 0 = any port */
    __u8  addr[16];
};
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, struct deny_addr);
    __type(value, __u8);
    __uint(max_entries, 1024);
} deny_connect SEC(".maps");

/* exec deny verdicts: the exec filename exactly as the kernel holds it in
 * bprm->filename (absolute literal paths only), NUL-padded to 256. Reuses
 * struct deny_path. */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, struct deny_path);
    __type(value, __u8);
    __uint(max_entries, 1024);
} deny_exec SEC(".maps");

/* Denies whose telemetry event could not be emitted (ring buffer full),
 * per-CPU, index 0 — mirrors the existing `drops` map pattern. */
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, __u32);
    __type(value, __u64);
    __uint(max_entries, 1);
} deny_event_drops SEC(".maps");
```

Verifier notes (all patterns already proven in this codebase or standard):

- The 256-byte `deny_path` key lives on the BPF stack (512-byte limit; the
  handlers need nothing else large). It is `__builtin_memset` to zero before
  filling so the exact-match key is deterministic past the NUL.
- No LPM tries, no prefix matching, no per-element regex in v1. Prefix
  matching via `BPF_MAP_TYPE_LPM_TRIE` on path bytes is a documented future
  extension, deliberately excluded to keep verdicts literal.

## 3. LSM hooks (exact SEC names)

Three programs, one per behavior class Goodman already captures. All three
hook names are real, long-standing LSM hooks (see
`include/linux/lsm_hook_defs.h`); none is invented:

| Program | SEC | Kernel hook | Signature |
|---|---|---|---|
| `enforce_file_open` | `SEC("lsm/file_open")` | `security_file_open` | `BPF_PROG(enforce_file_open, struct file *file)` |
| `enforce_socket_connect` | `SEC("lsm/socket_connect")` | `security_socket_connect` | `BPF_PROG(enforce_socket_connect, struct socket *sock, struct sockaddr *address, int addrlen)` |
| `enforce_bprm_check` | `SEC("lsm/bprm_check_security")` | `security_bprm_check` | `BPF_PROG(enforce_bprm_check, struct linux_binprm *bprm)` |

Return `0` = allow, `-EPERM` = deny. The verifier restricts BPF LSM return
values to `[-MAX_ERRNO, 0]`; `-EPERM` is valid for all three.

Per-hook argument handling — this is where the safety of the design lives:

- **`file_open`:** use `bpf_d_path(&file->f_path, key.path, sizeof(key.path))`
  to get the fully resolved path. `security_file_open` is in the kernel's
  `btf_allowlist_d_path`, so `bpf_d_path` is legal here. This is strictly
  better than the tracepoint's `bpf_probe_read_user_str` of the syscall arg:
  the path is kernel-resolved (no symlink/relative-path games, no TOCTOU).
  `bpf_d_path` failure (`n <= 0`) → allow.
- **`socket_connect`:** the `address` argument at `security_socket_connect`
  is the **kernel copy** of the sockaddr (`move_addr_to_kernel` runs before
  the security hook in `__sys_connect`), so reading it with
  `bpf_probe_read_kernel` is TOCTOU-free — unlike the existing
  `sys_enter_connect` tracepoint which reads user memory. Extract
  family/addr/port, do two `deny_connect` lookups: exact port, then port 0
  (any-port wildcard, needed for e.g. cloud-metadata 169.254.169.254).
  Non-INET/INET6 family → allow.
- **`bprm_check_security`:** `bpf_d_path` is **not** allowlisted for this
  hook (this is a verified kernel restriction, not a guess). Instead read
  `bprm->filename` — a kernel-owned string (stable, not user memory) — via
  `BPF_CORE_READ` + `bpf_probe_read_kernel_str`, and exact-match it against
  `deny_exec`. Limitation, documented and fail-open: `bprm->filename` is the
  path as passed to exec, so a *relative*-path exec of a denied binary will
  not match and is allowed (the deny map holds absolute literals only). The
  attempt is still detected and alerted by the existing execve tracepoint.

**Certainty markers, per the maintainer's instruction:**

- `lsm/file_open`, `lsm/socket_connect`, `lsm/bprm_check_security`:
  **verified hook names**, present since BPF LSM landed (kernel 5.7).
- `bpf_d_path` in `file_open`: **verified allowlisted**; available since 5.10.
- `bpf_get_current_cgroup_id()` in LSM programs: available (LSM programs fall
  back to the tracing helper set); requires the cgroup v2 hierarchy.
- `bpf_get_stack(ctx, ..., BPF_F_USER_STACK)` from LSM context for deny-event
  attribution: **expected to work** (the tracing helper set includes
  `bpf_get_stack`, and current-task user regs are reachable from process
  context), but this is the one item marked *UNVERIFIED until step 4 runs on
  a real kernel*. Fallback if it proves unreliable: emit deny events with
  `stack_len = 0`; attribution returns `<unknown>`, the alert still carries
  the behavior/rule/sensor evidence, and nothing about enforcement or
  fail-open changes.

**Enforcement kernel floor: 5.10** (for `bpf_d_path`), while detection keeps
its 5.8 floor. On older kernels the loader degrades to detection-only (see §
loader below).

Common decision path, cheapest check first (these hooks run for *every*
process on the node, not just watched ones):

```c
static __always_inline int enforce_active(void)
{
    __u32 zero = 0;
    __u64 *deadline = bpf_map_lookup_elem(&enforce_deadline, &zero);
    if (!deadline || *deadline == 0)
        return 0;                              /* off / missing -> allow */
    return bpf_ktime_get_ns() < *deadline;     /* lapsed -> allow */
}

/* in each hook: */
if (!enforce_active()) return 0;
__u64 cg = bpf_get_current_cgroup_id();
if (!bpf_map_lookup_elem(&enforced_cgroups, &cg)) return 0;  /* not opted in */
/* ...build key, lookup deny map; miss -> return 0 ... */
/* hit: emit EVENT_DENY_* via the existing ring buffer (best effort;
 * ringbuf full -> bump deny_event_drops, still deny), return -EPERM */
```

A matched deny **denies even if the telemetry event cannot be emitted** —
the deny map entry is an explicit operator instruction; ring-buffer pressure
loses telemetry, never enforcement. (The reverse—allowing because telemetry
failed—would make enforcement flaky under load, which is worse than a dropped
event that `deny_event_drops` makes visible.)

## 4. User-space verdict compilation (new package `internal/enforce`)

The kernel never attributes, never pattern-matches, never guesses. User space
compiles **concrete, literal** verdicts; the kernel does exact lookups.

**Where verdicts come from.** A verdict is created when the collector's diff
engine matches an *observed behavior string* against a rule with
`action: "block"` (and no `exclude` suppresses it) — exactly the events that
today produce a `WouldBlock` alert for `warn` rules. The observed behavior is
already concrete (`READ /etc/shadow`, `CONNECT 169.254.169.254:80`,
`EXEC /bin/sh`), so no regex→literal conversion is ever attempted:

- `READ <path>` → `deny_open` entry, **only if** `<path>` is absolute,
  ≤ 255 bytes, and contains no aggregation/canonicalization placeholder.
  Anything else is skipped (fail-open) and surfaces in
  `goodmanctl enforce status` as an uncompilable verdict.
- `CONNECT <ip>:<port>` → `deny_connect` entry, **only if** the host part
  parses as a literal IPv4/IPv6 address. CIDR-aggregated behaviors produced
  under `-connect-cidr` (e.g. `CONNECT 1.2.3.0/24:443`) are **not**
  compilable and are skipped — a subnet is not a literal. Deployments that
  want connect enforcement should run enforced namespaces with exact IPs
  (`-connect-cidr 0`, today's default).
- `EXEC <path>` → `deny_exec` entry, absolute literal paths only.

There are **no hashed keys anywhere** — a hash collision that denies the
wrong path is a misattribution, which the product forbids. Keys are the
literal bytes.

**Honest semantics (documented in `docs/enforcement.md`):** enforcement is
*reactive per behavior*. The first occurrence of a behavior is what creates
the verdict (it is alerted, marked `WouldBlock`); *subsequent* occurrences
are denied by the kernel and the alert upgrades to `Blocked`. Well-known
always-on targets (cloud metadata) converge after one observation. This is
the only model that keeps the kernel free of pattern matching.

**Excludes recompile live.** The verdict set is a pure function
`CompileVerdicts(rules []diff.Rule, behaviors []string) VerdictSet` — rules'
`exclude` regexes are applied at compile time, and the collector recomputes
the set whenever rules reload or new matching behaviors arrive. Sensors
reconcile their maps to the received set (add missing, **delete removed**),
so an operator can un-block a benign behavior by adding an `exclude` — no
redeploy, matching the existing rules-over-ifs discipline.

**Distribution.** The collector exposes the verdict set + enabled state at
`GET /v1/enforce/state` (ingest-token protected, same class as `/v1/events`):

```json
{"enabled": true, "rev": 17,
 "verdicts": {"open": ["/etc/shadow"],
              "connect": [{"addr": "169.254.169.254", "port": 0}],
              "exec": ["/bin/sh"]},
 "skipped": [{"behavior": "CONNECT 1.2.3.0/24:443", "reason": "not a literal address"}]}
```

The sensor polls this every ~500ms (tiny JSON; `rev` short-circuits
reconciliation). **The same successful poll is the heartbeat**: only after a
`200` with `enabled: true` does the sensor extend `enforce_deadline`. Collector
down, token wrong, `enabled: false`, or sensor wedged → deadline lapses →
kernel allows. One mechanism gives kill switch, liveness, and fail-open.

## 5. Sensor: scope population + heartbeat

New sensor behavior, all gated behind the sensor flag `-enforce-enabled`
(env `GOODMAN_ENFORCE_ENABLED`, default **false** — when false the LSM
programs are not even attached, so non-enforcing deployments pay zero
per-syscall cost):

1. **Scope loop** (reuses the machinery in `internal/coverage/kube.go`):
   - List namespaces, select those labeled `goodman.io/enforce=enabled`
     (new `EnforceLabelKey` beside the existing `InjectLabelKey`).
   - List pods on this node (existing call), extend the parsed fields with
     `metadata.uid` and keep namespace.
   - For each pod in an enforce-labeled namespace, find its cgroup dirs under
     the host cgroup2 mount (`/sys/fs/cgroup`, hostPath-mounted into the
     DaemonSet) by matching the pod UID in the kubelet cgroup path
     (`…/kubepods…pod<uid>…`), covering both systemd (`.slice`/`.scope`) and
     cgroupfs drivers. `stat()` each pod/container cgroup directory — the
     inode number **is** the cgroup id `bpf_get_current_cgroup_id()` returns.
     Insert the pod dir and all descendant dirs (the helper returns the
     *leaf* id, so descendants must be present).
   - Reconcile `enforced_cgroups` (add new, delete stale) every few seconds.
     Any error at any step → the affected cgroup simply isn't in the map →
     allow.
   - Outside Kubernetes, the only scope source is a test-oriented flag
     `-enforce-cgroup` (repeatable, explicit cgroup2 paths) used by
     `make e2e`; it is documented as a lab/e2e mechanism, not a product
     surface.
2. **State poll + heartbeat loop:** poll `GET /v1/enforce/state` every 500ms.
   On `200 {enabled:true}`: write `enforce_deadline[0] =
   clock_gettime(CLOCK_MONOTONIC) + 10s` (the same clock as
   `bpf_ktime_get_ns`) and reconcile deny maps when `rev` changed. On
   `enabled:false`: write `0` immediately (this is how `goodmanctl enforce
   off` lands in <1s: CLI → collector state flips → next sensor poll ≤500ms →
   deadline zeroed). On any error: write nothing — the deadline lapses on its
   own within ≤10s.
3. **Deny telemetry:** `EVENT_DENY_*` records arrive on the existing ring
   buffer and flow through the existing attribute→batch path unchanged
   (no new blocking work in the reader). The sensor sets `Denied: true` on
   the outgoing `Attributed` and increments a new
   `goodman_sensor_denied_total{type}` counter. Denied events must **not**
   feed baseline learning — the collector routes `Denied` events to the
   alert/evidence path only, never into `fingerprint.Update` behavior sets
   (a denied attempt is not "normal behavior", and learning it would
   eventually baseline the attack).

Loader changes (`internal/loader/loader.go`):

- `New()` gains an `enforce bool` knob (or a `NewWithOptions`); when false it
  **deletes the three LSM programs from the CollectionSpec before
  `NewCollection`** — critical, because on kernels without `CONFIG_BPF_LSM`
  the mere *load* of a `BPF_PROG_TYPE_LSM` program fails and would take the
  whole collection (and detection) down with it.
- When enforcement is requested: probe support (`features.HaveProgramType
  (ebpf.LSM)` from cilium/ebpf, plus check `bpf` appears in
  `/sys/kernel/security/lsm` — if `bpf` is missing from the active LSM list
  the programs can load but their hooks never fire, which would be silent
  no-enforcement; the loader must detect and report that, not pretend).
  On any unsupported condition: log one clear line, drop the LSM programs,
  continue detection-only, and expose `Loader.EnforcementActive() bool` +
  reason for metrics/status.
- Attach via `link.AttachLSM(link.LSMOptions{Program: …})` for each program;
  attach failure degrades the same way (log, close partial links,
  detection-only).
- New accessors for the sensor: `SetEnforceDeadline(ns uint64)`,
  `ReconcileEnforcedCgroups(ids map[uint64]bool)`,
  `ReconcileDenyMaps(v VerdictSet)`, `DenyEventDrops() uint64`.

## 6. `goodmanctl enforce on|off|status`

`goodmanctl` stays a collector client (sensors have no command API):

```
goodmanctl enforce status [-collector URL] [-token T]
goodmanctl enforce on     [-collector URL] [-token T]
goodmanctl enforce off    [-collector URL] [-token T]
```

- `on`/`off` → `POST /v1/enforce/on|off` (API-token class). `on` fails with a
  clear error unless the collector was started with `-enforce-enabled`
  (master gate) — a CLI call alone must never be able to arm a cluster whose
  operator did not opt in at deploy time.
- `status` → `GET /v1/enforce` (API-token class): master gate, runtime
  state, verdict counts per class, uncompilable/skipped verdicts with
  reasons, last-heartbeat age per sensor, and each sensor's
  `EnforcementActive` (so "collector says on but node kernel can't" is
  visible in one command).
- `off` propagation: state flips in the collector immediately; sensors pick
  it up on the next ≤500ms poll and zero the deadline → **<1s**, matching
  the DoD. Even if a sensor never polls again, its deadline lapses in ≤10s.

## 7. `LoadRules` posture for `action: "block"` — accept always

Maintainer decision (supersedes the reject-loudly posture in
`lsm-enforcement.md`, which was correct only while enforcement did not
exist): **`CompileRules` accepts `block` unconditionally** once this ships.

- `internal/diff/diff.go`: add `ActionBlock = "block"` to the action consts;
  remove the special-case rejection; update the doc comment.
- Alerting semantics: a `block`-rule match behaves like `warn` **plus** —
  `WouldBlock: true` is set (it *would* be / *is targeted to be* blocked),
  `goodman_enforce_would_block_total` still increments, and the alert is
  CRITICAL as today. When a kernel deny event for that behavior arrives, the
  alert is upgraded with `Blocked: true` (new field; see migration). The
  dashboard shows "would block" until the first real deny flips it to
  "BLOCKED".
- The kernel denies **only** when all three are true: rule says `block` (so a
  verdict was compiled) AND `enforce_deadline` is live (master gate + runtime
  switch + heartbeat) AND the calling task's cgroup is in `enforced_cgroups`.
  A rule file alone can never switch a cluster into deny mode — writing
  `block` in rules with enforcement disabled yields exactly today's `warn`
  behavior plus honest UI.

## 8. Helm

`deploy/helm/goodman/values.yaml` gains:

```yaml
# Kernel enforcement (eBPF LSM block mode). Requires kernel >= 5.10 with
# CONFIG_BPF_LSM=y and "bpf" in the active lsm= list, and cgroup v2.
# Denies apply only inside namespaces labeled goodman.io/enforce=enabled,
# only for behaviors matching rules with action: "block", and only while the
# sensor heartbeat is live. Everything fails open.
enforce:
  enabled: false        # master gate — sets GOODMAN_ENFORCE_ENABLED on BOTH
                        # the sensor DaemonSet and the collector
```

Template changes:

- Sensor DaemonSet: env `GOODMAN_ENFORCE_ENABLED`, and (only when
  `enforce.enabled`) a read-only hostPath mount of `/sys/fs/cgroup` if the
  chart does not already mount it. The DaemonSet is already privileged; no
  new capability needed beyond what BPF loading already requires.
- Collector Deployment: env `GOODMAN_ENFORCE_ENABLED`.
- RBAC: unchanged — the sensor SA already lists namespaces and pods for
  coverage; the enforce label read uses the same verbs.
- `NOTES.txt`: when `enforce.enabled`, print the namespace-label command and
  the `goodmanctl enforce status` check.
- `helm_test.go`: assert default renders **without** the env var set truthy
  and without the cgroup mount; assert `enforce.enabled=true` renders both.

Per AGENTS.md, this is an env-var addition, so all four surfaces move
together: flag in `cmd/…/main.go`, `docs/configuration.md`, Helm
values+templates, and it is not secret-shaped (no Secret change).

## 9. `make doctor` (scripts/preflight.sh)

The LSM section already checks `CONFIG_BPF_LSM=y` and `bpf` in
`/sys/kernel/security/lsm` (warn-level). Extend the same section, still
warn-level only:

- **cgroup v2:** `/sys/fs/cgroup/cgroup.controllers` exists (unified
  hierarchy). Hint: enforcement scope is cgroup-id based and requires
  cgroup v2 (kubelet cgroup v1 clusters are detection-only).
- **kernel ≥ 5.10:** compare `uname -r`; hint that `bpf_d_path` (used by the
  file_open deny path) needs 5.10 while detection keeps working from 5.8.
- Reword the verdict line to "LSM enforcement (block mode): available / not
  available" once block mode ships (drop "future").

## 10. File-by-file change list

| File | Change |
|---|---|
| `bpf/goodman.bpf.c` | 6 new maps (§2), 3 LSM programs (§3), shared `enforce_active()`/deny-emit helpers |
| `bpf/goodman.h` | 3 new `enum event_type` values only — **`struct event` untouched** |
| `internal/model/types.go` | mirror `EventType` consts + `String()`; `Attributed.Denied bool`; `Alert.Blocked bool` |
| `internal/model/types_test.go` | **unedited** (layout unchanged — the one rule) |
| `internal/loader/loader.go` | enforce option; LSM support probe + spec pruning; `AttachLSM`; deadline/cgroup/deny-map accessors; `EnforcementActive()` |
| `internal/loader/loader_test.go` | object-spec assertions: 3 LSM programs (`ebpf.LSM`, correct `AttachTo`), 6 new maps with exact types/key/value sizes |
| `internal/enforce/` (new) | `VerdictSet`, `CompileVerdicts(rules, behaviors)` (pure), behavior-string→literal parsers, skip-reasons |
| `internal/enforce/enforce_test.go` (new) | table tests: literals compile, CIDR/relative/oversize/placeholder behaviors skip, excludes suppress, reconcile diffs |
| `internal/diff/diff.go` | `ActionBlock` accepted; block matches set `WouldBlock`; deny-event path sets `Blocked` |
| `internal/diff/diff_test.go` | update `block`-rejected test to `block`-accepted; `Blocked` transition test |
| `internal/fingerprint/` | route `Denied` events away from behavior-set learning |
| `internal/store/store.go` + `migrations/007_enforcement.{postgres,sqlite}.sql` | `alerts.blocked` column + single-row `enforce_state` table (runtime switch survives collector restart; **no RawEvent/event-shape migration — none needed**) |
| `internal/api/api.go` + `auth_test.go` | `GET /v1/enforce/state` (ingest token), `GET /v1/enforce` + `POST /v1/enforce/{on,off}` (API token); auth-class test cases |
| `cmd/sensor/main.go` | `-enforce-enabled` flag; scope loop; state-poll/heartbeat loop; `goodman_sensor_denied_total`, heartbeat-age + enforcement-active gauges |
| `internal/coverage/kube.go` | `EnforceLabelKey`; pod UID in pod listing; (new file) pod-UID→cgroup-dir scanner + unit tests with a fake cgroupfs tree |
| `cmd/collector/main.go` | `-enforce-enabled` flag/env; wire enforce state + verdict recompute |
| `cmd/goodmanctl/main.go` | `enforce on|off|status` subcommands |
| `deploy/helm/goodman/…` | `enforce.enabled` (default **false**), env + cgroup mount, NOTES, `helm_test.go` |
| `deploy/rules.example.json` | commented `block` example next to the existing `warn` one |
| `scripts/preflight.sh` | §9 additions |
| `dashboard/src/types.ts`, `App.tsx` | `blocked` on Alert; "BLOCKED" chip beside "would block" (+ `make dashboard`, commit `internal/api/ui/dist/`) |
| `docs/enforcement.md` (new), `docs/configuration.md`, `docs/deployment.md`, `docs/api.md`, `CHANGELOG.md`, `AGENTS.md` | operator guide, flags, label, API shapes, invariants |

**Migrations:** one pair (`007_enforcement`), both dialects, per store rules.
Nothing about the kernel wire format migrates because it does not change.

## 11. Test strategy

**No root (the everyday loop — `make vet && make test && make smoke`):**

- `internal/loader/loader_test.go`: parse the embedded `.o`; assert the three
  LSM programs exist with `ebpf.LSM` type and expected attach names, and the
  six maps exist with exact type/key-size/value-size. Catches a stale `.o`
  and accidental map-shape drift without loading anything.
- `internal/model/types_test.go` passing **unedited** proves the wire layout
  did not move.
- `internal/enforce`: pure verdict-compilation tests (the heart of "the
  kernel only sees literals") — including the fail-open skips.
- `internal/coverage`: cgroup scanner against a fixture tree (systemd and
  cgroupfs kubelet layouts, both pod-UID formats).
- `internal/diff`: block accepted; `WouldBlock` on block match;
  `Blocked` upgrade on a synthetic deny event; excludes still suppress.
- `internal/api/auth_test.go`: the three new routes in their auth classes.
- `make smoke` extension: start the collector with `-enforce-enabled`,
  inject a synthetic behavior matching a block rule, assert
  `/v1/enforce/state` serves the compiled literal verdict, `goodmanctl
  enforce off` flips `enabled:false`, and a synthetic `Denied` event upgrades
  the alert to `blocked`. This exercises the whole user-space enforcement
  pipeline with zero kernel involvement.
- Helm: `helm-lint` + `helm_test.go` default-off assertions.

**`sudo make e2e` (real kernel, human-run — must prove):**

1. LSM programs load and attach on a kernel with `CONFIG_BPF_LSM` +
   `lsm=…,bpf` (and the run *states* kernel version and LSM list).
2. Scoped deny: workload placed in a cgroup passed via `-enforce-cgroup`,
   verdict compiled from a real block-rule match; the workload's **second**
   attempt at the behavior gets `EPERM`; a CRITICAL alert with
   `blocked: true` and full evidence (behavior, rule, sensor, first-seen)
   appears.
3. Scope isolation: the identical operation from a process **outside** the
   scoped cgroup succeeds and only alerts.
4. Kill switch latency: `goodmanctl enforce off` → the previously denied
   operation succeeds within 1s.
5. Dead-man behavior: `kill -9` the sensor → the operation succeeds within
   the deadline TTL (≤10s), and detection alone resumes when the sensor
   restarts.
6. Degrade path: run the sensor with `-enforce-enabled` on a kernel/boot
   without `bpf` in the LSM list → sensor logs the degrade, keeps detecting,
   `goodmanctl enforce status` shows the node as enforcement-inactive.
   (This case may be covered in CI by booting the e2e VM twice or by a
   documented manual step; it must not be skipped silently.)

Any summary of work in this area must state explicitly whether only the
no-root path ran (AGENTS.md rule; unprivileged sandboxes cannot load BPF).

## 12. Fail-open matrix

Every row resolves to **allow** (detection keeps alerting in all cases):

| Condition | Kernel behavior |
|---|---|
| `enforce_deadline` value 0 (load default / `enforce off`) | allow |
| Deadline in the past (sensor dead, hung, or collector unreachable ≥ TTL) | allow |
| `enforce_deadline` lookup fails | allow |
| Calling task's cgroup id not in `enforced_cgroups` (unlabeled ns, scan error, pod not yet reconciled, cgroup v1) | allow |
| `bpf_d_path` returns error / path > 255 bytes | allow |
| `bprm->filename` read fails, or exec used a relative path | allow (tracepoint still alerts) |
| sockaddr family not INET/INET6, or read fails | allow |
| Deny-map lookup miss (no verdict) | allow |
| Verdict not compilable (CIDR-aggregated, placeholder, relative, oversize) | never enters a map → allow; surfaced in `enforce status` |
| LSM programs fail to load/attach, `CONFIG_BPF_LSM` absent, `bpf` not in `lsm=` list, kernel < 5.10 | programs pruned/detached; detection-only; status shows why |
| Collector started without `-enforce-enabled` | `/v1/enforce/state` reports disabled; sensors never arm the deadline |
| Sensor started without `-enforce-enabled` | LSM programs not even loaded |
| Sensor exits | maps and programs die with it → allow |
| Ring buffer full on a deny | **deny stands** (explicit operator verdict), telemetry drop counted — the one deliberate non-open row, and it is not an *ambiguity*: the verdict was certain |

## 13. Phased sub-steps (implementable in order; each lands green on `make vet && make test && make smoke`)

**Step 1 — map + event-type scaffold (no behavior change).**
`bpf/goodman.bpf.c` gains the six maps; `bpf/goodman.h` +
`internal/model/types.go` gain the three deny event types (mirrored, same
commit); `make bpf`; `loader_test.go` asserts the new maps. No programs, no
attach, nothing reads the maps. `types_test.go` unedited.

**Step 2 — LSM stub programs that always allow.**
Add the three `SEC("lsm/…")` programs returning 0 unconditionally; loader
support probe + spec-pruning degrade path + `AttachLSM` behind the (new,
default-false) enforce option; `EnforcementActive()`; object-test assertions
for the programs. Sensor flag exists but only controls attach. Human
checkpoint: `sudo make e2e` on an LSM-enabled kernel proves attach + degrade,
with hooks that cannot deny anything.

**Step 3 — kill switch + scope wiring (still cannot deny).**
Kernel: stubs gain the `enforce_active()` + cgroup-scope checks (still
`return 0` after them). User space: collector `-enforce-enabled` +
`enforce_state` persistence + `/v1/enforce*` endpoints + auth tests;
sensor state-poll/heartbeat + cgroup scope loop + `-enforce-cgroup`;
`goodmanctl enforce on|off|status`; migration 007; smoke extension for
state/heartbeat. Kill-switch latency is now measurable before any deny
exists.

**Step 4 — real denies.**
Kernel: deny-map lookups, deny-event emission, `-EPERM` returns. User space:
`internal/enforce` verdict compilation + collector recompute + sensor map
reconciliation; `ActionBlock` accepted in `CompileRules`; `Denied` event
routing (alert upgrade to `Blocked`, fingerprint learning bypass); metrics.
Human checkpoint: full `sudo make e2e` DoD list from §11.

**Step 5 — product surfaces + docs.**
Helm `enforce.enabled` (default false) + mounts + NOTES + helm tests;
dashboard `blocked` chip (`make dashboard`, commit `dist/`); doctor
extensions; `docs/enforcement.md` + configuration/deployment/api docs +
`rules.example.json` comment + `CHANGELOG.md`; AGENTS.md gains the new
invariants (deny maps are literal-only; enforcement fail-open rows; enum
additions are not layout changes but must be mirrored).

Steps 1–3 are shippable at any point with zero enforcement risk (nothing can
deny). Step 4 is the only step that can affect a workload, and only inside
`goodman.io/enforce=enabled` namespaces on clusters where an operator set
`enforce.enabled=true` **and** ran `goodmanctl enforce on` — three explicit
opt-ins deep, with a dead-man switch under all of it.
