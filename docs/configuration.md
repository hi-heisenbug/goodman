# Configuration

Everything is configurable by flag or environment variable, and by Helm value in
Kubernetes. Flags take precedence over environment variables; environment
variables take precedence over built-in defaults.

## Collector

`bin/collector [flags]`

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `-listen` | `GOODMAN_LISTEN` | `:8844` | HTTP listen address (API + dashboard + metrics). |
| `-dsn` | `GOODMAN_DSN` | `goodman.db` | Datastore. `postgres://…` for Postgres, a file path or `sqlite://…` for SQLite. |
| `-learn-obs` | `GOODMAN_LEARN_OBS` | `500` | Observations required before a fingerprint is promoted to a baseline. |
| `-learn-min-age` | `GOODMAN_LEARN_MIN_AGE` | `24h` | Wall-clock age (first→last seen) required before promotion. |
| `-rules` | `GOODMAN_RULES` | *(built-in)* | Path to a high-risk rules JSON file. Empty = built-in defaults. |

**Learning window.** A `(service, package, version)` fingerprint becomes a
baseline only when it has been observed `learn-obs` times **and** spans at least
`learn-min-age` of wall-clock time. Until then Goodman is still learning and will
not alert on it. The age gate exists so periodic jobs (hourly/daily) get a chance
to appear in the baseline. For local experiments, shorten both:

```bash
GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s ./bin/collector
```

**Datastore.** SQLite is the default (dev/pilot). For production, point at Postgres:

```bash
GOODMAN_DSN='postgres://goodman:secret@db:5432/goodman?sslmode=require' ./bin/collector
```

## Sensor

`bin/sensor [flags]` — must run as root (loads eBPF).

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `-collector` | `GOODMAN_COLLECTOR_URL` | `http://127.0.0.1:8844` | Collector base URL to POST events to. |
| `-proc-root` | `GOODMAN_PROC_ROOT` | `/proc` | Host proc mount. Set to `/host/proc` in the DaemonSet. |
| `-batch-interval` | `GOODMAN_BATCH_INTERVAL` | `1.5s` | How often to flush batched events to the collector. |
| `-metrics-addr` | `GOODMAN_METRICS_ADDR` | `:9478` | Prometheus metrics listen address (`""` disables). |
| `-comms` | `GOODMAN_EXTRA_COMMS` | *(none)* | Extra process names to watch, comma-separated (beyond built-in runtime comm names). |
| `-watch-interval` | — | `3s` | How often to rescan `/proc` for runtime processes. |
| `-stdout` | — | `false` | Print attributed events to stdout instead of sending to the collector (debugging). |
| `-raw` | — | `false` | With `-stdout`: also print raw events including stack depth. |
| `NODE_NAME` | — | hostname | Sensor identity in event batches (set to the k8s node name in the DaemonSet). |

Watched runtimes default to `node`, `nodejs`, Node's `MainThread`, `python`, and
`python3`. Add more with `-comms` / `GOODMAN_EXTRA_COMMS`.

## High-risk rules

Drift is escalated from `WARN` to `CRITICAL` when a new behavior matches a
high-risk rule. Rules are a config-driven list of case-insensitive regexes matched
against the canonical behavior string — never hard-coded — so you can tune them.

The built-in defaults (also in [`deploy/rules.example.json`](../deploy/rules.example.json)):

```json
[
  { "name": "secret-read",          "pattern": "^READ .*(secret|token|credential|password|shadow|\\.pem|\\.key|\\.aws|\\.ssh|\\.npmrc|\\.env|id_rsa)" },
  { "name": "cloud-metadata",       "pattern": "^CONNECT 169\\.254\\.169\\.254:" },
  { "name": "new-outbound-connect", "pattern": "^CONNECT " },
  { "name": "new-exec",             "pattern": "^EXEC " }
]
```

Point the collector at your own file:

```bash
./bin/collector -rules ./my-rules.json
```

In Kubernetes, set the `rules` Helm value (see below) — it's mounted as a
ConfigMap and passed to the collector.

## Helm values

Set with `--set key=value` or a values file (`-f values.yaml`). See the full list
in [`deploy/helm/goodman/values.yaml`](../deploy/helm/goodman/values.yaml).

| Key | Default | Meaning |
|---|---|---|
| `cluster` | `dev` | Deployment identity. |
| `registries` | `npm` | Comma-separated registries watched (informational in v1). |
| `sensor.image` / `collector.image` | `ghcr.io/goodman-sec/*:0.1.0` | Container images. |
| `sensor.extraComms` | `""` | Extra process names to watch. |
| `sensor.metricsPort` | `9478` | Sensor Prometheus port. |
| `collector.replicas` | `1` | Collector replica count. |
| `collector.service.type` / `.port` | `ClusterIP` / `8844` | Collector service. |
| `postgres.dsn` | `""` | Postgres DSN; empty → embedded SQLite. |
| `postgres.sqlitePath` | `/data/goodman.db` | SQLite path (when `dsn` is empty). |
| `learningWindow.obsCount` | `500` | Observations before baseline promotion. |
| `learningWindow.minAgeHours` | `24` | Wall-clock hours before promotion. |
| `attribution.tier` | `perfmap` | `perfmap` (Tier 1) or `v8native` (Tier 2, not in v1). |
| `attribution.requireNodeOptions` | `true` | Whether install notes remind you to inject `NODE_OPTIONS`. |
| `rules` | `""` | High-risk rules JSON (empty → built-in defaults). |
| `rbac.create` | `true` | Create the sensor ServiceAccount + ClusterRole. |

Example production install:

```bash
helm install goodman deploy/helm/goodman \
  --set cluster=prod \
  --set-string registries='npm\,pypi' \
  --set collector.image=ghcr.io/goodman-sec/collector:0.1.0 \
  --set sensor.image=ghcr.io/goodman-sec/sensor:0.1.0 \
  --set postgres.dsn='postgres://goodman:secret@db:5432/goodman?sslmode=require'
```
