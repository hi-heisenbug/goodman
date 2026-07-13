# Goodman pilot runbook

Short operational guide for a first production-style Goodman deployment.

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
  collector restarts; not multi-replica HA.
- **Production:** set `postgres.dsn`; use `collector.replicas>1` for HA.
- **Sensor spool:** `GOODMAN_SPOOL_EVENTS` retains attributed events when the
  collector is unreachable (RAM-only; sized in Helm `sensor.spoolEvents`).

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
