# Goodman pilot runbook

Operational guide for a first production-style Goodman deployment (detection-first,
warn-before-block, optional enforcement). Pair with [`docs/deployment.md`](deployment.md)
and [`docs/configuration.md`](configuration.md).

## Pre-flight

```bash
make doctor          # BTF, optional LSM/cgroup v2 for enforcement
make vet && make test && make smoke && make replay   # backend gates (no root)
```

On the cluster: confirm nodes expose BTF (`/sys/kernel/btf/vmlinux`), cgroup v2
unified, and (if you plan block mode) `CONFIG_BPF_LSM` + `lsm=bpf` on at least
one staging node. Goodman sensors need privileged DaemonSet scheduling.

## Install

1. Confirm toolchain: `make doctor` (BTF, optional LSM/cgroup v2 for enforcement).
2. Install via Helm (default: detection-only, auth on, SQLite PVC):

   ```bash
   helm upgrade --install goodman deploy/helm/goodman -n goodman --create-namespace
   ```

3. Label app namespaces for injection (Tier-1 attribution):

   ```bash
   kubectl label namespace <app-ns> goodman.io/inject=enabled
   ```

4. Open the dashboard (port-forward or ingress), set `GOODMAN_API_TOKEN` if auth
   is enabled.

## Noise tuning

- Start with default rules; tune `exclude` regexes on noisy rules (CDN connects,
  health checks) — see `deploy/rules.example.json`.
- Use Coverage tab **alert budget** (`digest-alert-budget` / soft target) to
  track burn rate.
- Promote baselines only after the learning window (`learn-obs` / `learn-min-age`).
- For connect drift noise, avoid broad `-connect-cidr` aggregation in enforced
  namespaces if you plan block rules (CIDR behaviors do not compile to verdicts).

## Warn vs block posture

| Mode | Rule `action` | Effect |
|---|---|---|
| Detect | `alert` | CRITICAL when matched; no enforcement fields |
| Audit | `warn` | Same + `would_block` chip (nothing denied) |
| Enforce | `block` | Same + compiled verdicts; kernel denies when armed |

Recommended path: run **`warn`** during pilot; review would-block counts vs alert
budget; enable **`block`** + enforcement only on a volunteer namespace.

Enforcement requires: Helm `enforce.enabled=true`, namespace
`goodman.io/enforce=enabled`, and `goodmanctl enforce on`.

## Weekly digest

With `notifications.webhookUrl` and `digestInterval` (default 168h), the collector
posts open-alert count, executed-package reachability summary, and would-block
count. Use it as a POV heartbeat even when quiet.

## Durability

- **Pilot default:** SQLite on a PVC (`store.persistence.enabled=true`) — survives
  collector restarts; **not** multi-replica HA.
- **Production HA:** set `postgres.dsn` first, then `collector.replicas>1`.
  Two collectors against one SQLite file is invalid (single-writer). Proof:
  `make ha-smoke` when Docker is available, or staging Helm — see
  [`docs/release.md`](release.md).
- **Sensor spool:** `GOODMAN_SPOOL_EVENTS` retains attributed events when the
  collector is unreachable (RAM-only; sized in Helm `sensor.spoolEvents`).

## Monitoring during pilot

- Dashboard **Coverage** tab: sensor heartbeats, attribution KPI, namespace
  injection gaps, alert-budget burn rate.
- Prometheus: `goodman_enforce_would_block_total` while running warn-mode rules.
- Weekly digest (webhook + `digestInterval`): open alerts, reachability delta,
  would-block count — configure before week one so a quiet POV still speaks.

## Multi-cluster baselines (optional)

If the customer runs 3+ clusters, export promoted baselines from cluster A and
import on B (`goodmanctl fingerprints export|import`). Imported rows carry
`origin=imported` and never overwrite locally learned baselines. See
[`docs/deployment.md`](deployment.md#multi-cluster-baseline-sharing).

## Enforcement checks

```bash
goodmanctl enforce status
kubectl label namespace <pilot-ns> goodman.io/enforce=enabled
goodmanctl enforce on    # after enforce.enabled=true at deploy
goodmanctl enforce off # immediate disarm path for incidents
```

If `enforce status` shows sensors with `enforcement_active: false` while the
master gate is on, check `make doctor` LSM lines — the node runs detection-only.

## Incident response

1. `goodmanctl enforce off` — fastest disarm (<1s with live sensors).
2. Remove `goodman.io/enforce=enabled` label if needed (scope shrinks on next reconcile).
3. Add rule `exclude` patterns for benign behavior — verdicts recompile without redeploy.

Dead-man: killing the sensor allows traffic within ≤10s even if step 1 fails.

Full enforcement design: [`docs/enforcement.md`](enforcement.md). Live LSM proof
requires `sudo make e2e` on a real kernel — not runnable from unprivileged CI.

## Rollback / exit

1. `goodmanctl enforce off` and remove enforce namespace labels.
2. `helm uninstall goodman -n goodman` (or scale sensor DaemonSet to zero).
3. Baselines on the PVC or Postgres DSN can be retained for a re-install;
   export fingerprints first if you need an offline copy.
