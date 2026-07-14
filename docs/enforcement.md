# Kernel enforcement (eBPF LSM block mode)

Goodman can **deny** syscalls in-kernel for behaviors that match rules with
`action: "block"`. Detection always continues; enforcement is a separate,
explicitly opt-in layer that **fails open** on every ambiguity.

## Three opt-ins

Enforcement arms only when **all** of the following are true:

1. **Deploy master gate** — `enforce.enabled=true` in Helm (sets
   `GOODMAN_ENFORCE_ENABLED` on collector and sensor), or
   `-enforce-enabled` / `GOODMAN_ENFORCE_ENABLED=true` for bare binaries.
2. **Runtime switch** — `goodmanctl enforce on` (persisted in the collector;
   `goodmanctl enforce off` disarms within ~1s via sensor poll + deadline zero).
3. **Namespace scope** — workloads run in namespaces labeled
   `goodman.io/enforce=enabled` (sensor maps pod cgroups into
   `enforced_cgroups`).

Enforcement currently requires a **single collector replica**. Detection and
Postgres-backed ingestion support HA, but verdict compilation and the runtime
enforcement revision are intentionally not advertised as HA-safe yet. The Helm
chart rejects `enforce.enabled=true` with `collector.replicas>1` instead of
allowing sensors to flap between divergent replica state.

A `block` rule alone never denies anything — it compiles verdicts and sets
`would_block` on alerts exactly like `warn`, plus kernel denies once armed.

### Service and cgroup isolation

The collector keeps behaviors and compiled verdicts keyed by service. Each
sensor maps local pod cgroup IDs to the pod service identity and expands
verdicts into composite `{cgroup_id, path|address}` BPF keys. Two enforced
services on the same node can therefore use the same literal independently;
one service's verdict never matches the other's cgroup.

During a verdict/scope change the sensor removes all entries from
`enforced_cgroups`, reconciles the composite deny maps, then re-arms the current
scopes only after every map succeeds. A partial update therefore fails open.

### Path identity

Detection carries the `openat*` dirfd in `RawEvent` and resolves relative paths,
cwd paths, symlinks, and container mount namespaces through `/proc/<pid>/root`.
Exec detection follows the same process-root resolver. Kernel enforcement uses
`bpf_d_path` for both `file_open` and `bprm_check_security`, so observed and
enforced paths use the same absolute, symlink-resolved identity. If user space
cannot resolve a dirfd/path confidently, it records an `<unresolved>` behavior
that may alert but never compiles into a deny key.

## Fail-open matrix (summary)

| Condition | Kernel |
|---|---|
| Master gate off, runtime off, deadline lapsed, cgroup not scoped | **allow** |
| Verdict not compilable (CIDR connect, collapsed/placeholder/unresolved path) | **allow** (surfaced in `goodmanctl enforce status`) |
| LSM unavailable (`CONFIG_BPF_LSM`, `bpf` not in `lsm=`, attach error) | **allow** (sensor logs degrade; detection continues) |
| Ring buffer full on deny | **deny stands** (telemetry may drop; `deny_event_drops` counter) |

See [`docs/research/lsm-enforcement-impl.md`](research/lsm-enforcement-impl.md) for the full matrix.

## Reactive semantics

The **first** observation of a block-rule behavior alerts with
`would_block: true`. The collector compiles a **literal** verdict (exact path,
IP:port, or exec path). The **second** attempt inside a scoped cgroup is
denied by the kernel; the alert upgrades to `blocked: true`.

Cloud-metadata and other always-on targets converge after one observation.

## Verdict compilation

User space compiles concrete literals only — the kernel never regex-matches:

| Behavior | Verdict map | Notes |
|---|---|---|
| `READ /etc/shadow` | `deny_open` | `{cgroup_id, absolute resolved path}` |
| `CONNECT 169.254.169.254:80` | `deny_connect` | `{cgroup_id, literal IP, port}`; port `0` = any port |
| `EXEC /usr/bin/sh` | `deny_exec` | `{cgroup_id, absolute resolved path}` |

Skipped (fail-open): CIDR-aggregated connects (`-connect-cidr`), collapsed
paths (`**`), placeholders, and `<unresolved>` paths.

## Kill switch / heartbeat

Sensors poll `GET /v1/enforce/state` every ~500ms. On `enabled: true`, the
sensor extends `enforce_deadline` to now+10s (CLOCK_MONOTONIC). Collector
down, token failure, runtime off, or sensor exit → deadline lapses → **allow**
within ≤10s even if the sensor never polls again.

## Requirements

- Kernel **≥ 5.10** (for `bpf_d_path` in `file_open`; detection stays 5.8+)
- `CONFIG_BPF_LSM=y` and `bpf` in active `lsm=` list
- **cgroup v2** unified hierarchy (`/sys/fs/cgroup/cgroup.controllers`)

Run `make doctor` for a warn-level checklist.

## Operator commands

```bash
goodmanctl enforce status   # master gate, runtime state, verdict counts, sensor heartbeats
goodmanctl enforce on       # fails if collector master gate is off
goodmanctl enforce off      # disarms within ~1s
```

Label a namespace for scope:

```bash
kubectl label namespace my-app goodman.io/enforce=enabled
```

## Lab / e2e (non-k8s)

Sensors accept repeatable
`-enforce-cgroup SERVICE=/sys/fs/cgroup/...` scopes for `make e2e` — not a
supported production surface. Bare paths are rejected because they cannot be
associated safely with service-scoped verdicts.

## Human verification

`sudo make e2e` on an LSM-enabled kernel must prove attach, scoped deny,
scope isolation, kill-switch latency, and dead-man behavior. CI and agents
verify the user-space pipeline via `make smoke` only.
