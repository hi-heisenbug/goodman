# Research: eBPF LSM enforcement (Phase 6) — NO-GO on kernel work; scaffold only

> **Status:** research decision (2026-07-13) for `plan-deferred.md` Phase 6.
> **Decision: NO-GO on any `bpf/` change** until all three gates fire:
> (1) 60–90 days of Phase 3 `action: warn` evidence across 2–3 real
> environments with near-zero false positives, (2) a customer volunteering a
> **staging** namespace, (3) maintainer sign-off on the kill-switch design in
> this document. The evidence clock started when Phase 3 shipped
> (`would_block` alerts + `goodman_enforce_would_block_total` + the Coverage
> tab pairing of would-block counts against the alert budget); nothing here
> may ship before that window completes.
>
> What *is* sanctioned now is the **scaffold**: this document, `make doctor`
> LSM capability checks, and an explicit, documented posture for
> `action: "block"` in rule files. No LSM hook, no deny path, no new BPF map.

## The `action: "block"` posture — decision

Two options were on the table for what `diff.LoadRules` should do when an
operator writes `action: "block"` before enforcement exists:

- **A. Accept-and-downgrade:** accept `block`, treat it as `warn`
  (alert + `WouldBlock`) until enforcement ships.
- **B. Reject loudly:** `block` fails rule loading with a clear error, as
  every unknown action already does.

**Decision: B — reject loudly.** This is also the *current* behavior:
`CompileRules` (`internal/diff/diff.go`) errors on any action other than
`alert`/`warn`, and its doc comment codifies "Unknown actions fail rule
loading loudly; there is no silent ignore."

Reasoning: option A creates a false sense of security — an operator who
writes `block` believes traffic is being denied when nothing is. In a
security product, config that overstates the protection actually delivered
is the worst failure mode available; a collector that refuses to start with
`rule "x": action "block" is not available yet (enforcement has not shipped;
use "warn" to build the audit evidence)` is annoying for five minutes and
honest forever. It also preserves a clean upgrade story: when Phase 6 ships,
`block` becoming *valid* is an unambiguous signal, rather than a silent
semantics change to rules that were already deployed.

Scaffold work for this decision (no behavior change, all no-root):

- Improve the `CompileRules` unknown-action error to special-case `"block"`
  with the message above (today it reads as a generic unknown action).
- A `diff_test.go` case asserting `action: "block"` fails `LoadRules` with
  that message.
- A commented note in `deploy/rules.example.json` next to the existing
  `warn` example: `warn` is how you build the evidence that unlocks `block`.
- `docs/configuration.md` rule-schema section states the posture and links
  here.

When enforcement does ship, `block` additionally requires the collector/
sensor to be started with enforcement explicitly enabled — a rule file alone
must never be able to switch a cluster into deny mode.

## `make doctor` LSM checks (scaffold, ships now)

`scripts/preflight.sh` already checks BTF, `CONFIG_BPF*`, and
unprivileged-BPF posture. Add a new subsection, **warn-level, never
fail-level** (enforcement is optional; detection must not look broken on
kernels without LSM):

- `CONFIG_BPF_LSM=y` in `/boot/config-$(uname -r)` / `/proc/config.gz`
  (same `$reader` pattern the existing CONFIG checks use).
- `bpf` present in the active LSM list — `/sys/kernel/security/lsm` — with a
  hint when missing: add `lsm=...,bpf` to the kernel boot parameters
  (GRUB `GRUB_CMDLINE_LINUX`), reboot; many distros compile the config in
  but do not enable bpf in the default `lsm=` list.
- Print the verdict as "LSM enforcement (future block mode): available /
  not available" so `make doctor` output is meaningful before the feature
  exists.

## Safety design for the eventual build (recorded now, built later)

The product is the sequencing, not the hook. Everything below is
**fail-open**: on any ambiguity — unknown pid/cgroup, torn map read, sensor
or collector outage, verdict map missing — the LSM programs return allow,
and detection keeps alerting. If user space is gone, the kernel enforces
nothing.

**Hooks.** `lsm/file_open`, `lsm/socket_connect`, `lsm/bprm_check_security`
(exec) — mirroring the three tracepoints Goodman already captures. New
programs in `bpf/goodman.bpf.c`; `internal/loader/loader_test.go` (the
no-root object test) gains assertions that they exist and are typed
correctly. If `struct event` changes at all, `internal/model/types.go`
changes in the same commit and `types_test.go` must pass unedited — the one
rule.

**Opt-in scope: cgroup deny maps.** Enforcement applies only to workloads
explicitly opted in:

- A BPF hash map of enforced cgroup ids (`enforced_cgroups`), populated by
  the sensor only for pods in namespaces labeled
  `goodman.io/enforce=enabled`. Absent entry → allow, always. Everything
  else in the cluster is observe-only forever.
- A separate pre-computed deny map: user space (which owns attribution and
  policy) compiles matched `action: block` rule verdicts into concrete
  kernel-checkable entries (e.g. path prefixes / destination addr+port /
  exec basenames scoped to a cgroup). The kernel consults the deny map only;
  it never attributes, never regex-matches, never blocks on a guess. User
  space decides *policy*; the kernel enforces *verdicts*.

**Kill switch.** One global BPF map flag (`enforce_enabled`), checked first
by every LSM program:

- `goodmanctl enforce off` flips it via the sensor in <1s, no redeploy, no
  pod restart. `goodmanctl enforce status` reports it.
- The flag defaults to off at load; the sensor must affirmatively enable it
  at startup **only** when its own enforcement flag is set (Helm value
  defaults off). Sensor exit → map is gone with the program → allow.
- A denied-but-benign behavior must be un-blockable without redeploying:
  rule `exclude` patterns recompile the deny map live (the same
  config-driven rules discipline as detection).

**Every deny alerts.** A kernel deny raises a CRITICAL alert with the full
evidence chain (behavior, rule, sensor, first-seen), so an outage caused by
a bad rule is diagnosable from the dashboard in seconds.

## File touch list (for the eventual build — not now)

- `bpf/goodman.bpf.c`, `bpf/goodman.h` — LSM programs + `enforced_cgroups`,
  deny, and `enforce_enabled` maps
- `internal/loader/loader.go` + `loader_test.go` — attach LSM programs
  (attach must degrade gracefully on kernels without `lsm=bpf`: log, disable
  enforcement, keep detection running), object-test assertions
- `internal/diff/diff.go` — `ActionBlock` becomes valid; verdict extraction
- `internal/attribute/` or a new `internal/enforce/` — verdict compilation
- `cmd/sensor/main.go`, `cmd/goodmanctl/` — enforcement flag, `enforce on|off|status`
- Helm — enforcement values default **off**; namespace label documented
- `docs/enforcement.md` (new), configuration/deployment updates, `CHANGELOG.md`

**Verification then:** `make bpf && make build`, all no-root gates, and
`sudo make e2e` is **required** (bpf touched — human step on a real kernel
with `CONFIG_BPF_LSM` + `lsm=bpf`). Any summary must state explicitly if
only the no-root path ran.

## Effort estimate

- Scaffold (now): **1–2 days** — doctor checks, `block` error message +
  test, rules-example comment, docs. No `bpf/` change, no e2e needed.
- Full Phase 6 (gated): **3–4 weeks** — LSM programs + verifier work ~1.5 wk,
  verdict compilation + kill switch + goodmanctl ~1 wk, Helm/docs/e2e/lab
  validation ~1 wk. Matches plan-deferred.

## DoD

- **Scaffold:** `make doctor` reports LSM capability with actionable hints;
  `action: "block"` fails rule loading with the enforcement-not-shipped
  message and a test proves it; posture documented in configuration docs;
  `make vet && make test && make smoke` green; `bpf/` untouched.
- **Full phase (later):** as in `plan-deferred.md` — replayed attack denied
  and alerted in an enforce-labeled namespace, alert-only elsewhere;
  `goodmanctl enforce off` takes effect <1s; `sudo make e2e` green on a real
  kernel.
