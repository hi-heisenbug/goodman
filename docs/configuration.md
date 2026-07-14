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
| `-ingest-token` | `GOODMAN_INGEST_TOKEN` | *(empty)* | Bearer token required from sensors on `POST /v1/events`. Empty leaves ingestion open (local dev only). |
| `-api-token` | `GOODMAN_API_TOKEN` | *(empty)* | Bearer token required on the alerts/fingerprints/stream API. Empty leaves the API open (local dev only). |
| `-tls-cert` | `GOODMAN_TLS_CERT` | *(empty)* | PEM certificate; with `-tls-key`, the collector serves HTTPS. |
| `-tls-key` | `GOODMAN_TLS_KEY` | *(empty)* | PEM private key for `-tls-cert`. |
| `-webhook-url` | `GOODMAN_WEBHOOK_URL` | *(empty)* | POST every alert to this webhook. Empty disables notifications. |
| `-webhook-format` | `GOODMAN_WEBHOOK_FORMAT` | `generic` | Webhook payload: `generic` JSON or `slack` (Slack-compatible `text` message). |
| `-webhook-token` | `GOODMAN_WEBHOOK_TOKEN` | *(empty)* | Bearer token sent to the webhook endpoint. |
| `-webhook-min-severity` | `GOODMAN_WEBHOOK_MIN_SEVERITY` | `WARN` | Lowest severity forwarded (`INFO`, `WARN`, `CRITICAL`). |
| `-public-url` | `GOODMAN_PUBLIC_URL` | *(empty)* | Dashboard base URL for Slack deep links (alerts + weekly digest). |
| `-retention` | `GOODMAN_RETENTION` | `0` | Prune resolved alerts older than this (`720h` = 30 days). `0` keeps them forever. Open/acknowledged alerts are never pruned. |
| `-reachability-interval` | `GOODMAN_REACHABILITY_INTERVAL` | `0` | Recompute stored reachability reports on this cadence as fingerprints change (`0` = disabled). |
| `-reachability-osv` | `GOODMAN_REACHABILITY_OSV` | `false` | Enrich scheduled reachability recomputes with OSV.dev (needs egress). |
| `-osv-endpoint` | `GOODMAN_OSV_ENDPOINT` | public OSV.dev | Override the OSV querybatch endpoint for an internal proxy or air-gapped mirror; advisory-detail requests use the same base URL. |
| `-digest-interval` | `GOODMAN_DIGEST_INTERVAL` | `0` | Emit a weekly digest to the webhook on this cadence (`168h` = weekly). `0` = disabled. Requires `-webhook-url`. Fires once at startup. |
| `-digest-alert-budget` | `GOODMAN_DIGEST_ALERT_BUDGET` | `5` | Soft open-alert noise target quoted in the digest. |
| `-ha-replicas` | `GOODMAN_HA_REPLICAS` | `1` | Expected collector replica count. When `>1`, Postgres is required and singleton background loops use advisory-lock leader election. Helm sets this from `collector.replicas`. |
| `-enforce-enabled` | `GOODMAN_ENFORCE_ENABLED` | `false` | Master gate for kernel LSM enforcement. When false, `/v1/enforce/on` fails and sensors never arm the deadline. |

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

**Authentication.** Production deployments should set both tokens; the Helm
chart generates them automatically. Sensors authenticate with the ingest token;
the dashboard, `goodmanctl`, and any API client use the API token. Browser SSE
clients mirror that token into a SameSite cookie scoped to `/v1/stream` because
`EventSource` cannot set headers; query-string tokens are rejected. `/metrics`
also requires the API token. Only `/v1/healthz`, `/v1/readyz`, and the static
dashboard assets remain public. When the dashboard receives a 401 it shows a
token prompt and stores the entered token in the browser's localStorage.

**Alert notifications.** With `GOODMAN_WEBHOOK_URL` set, every alert at or above
`GOODMAN_WEBHOOK_MIN_SEVERITY` is POSTed to the webhook from a background
worker (bounded queue, three delivery attempts with backoff; ingestion is never
blocked by a slow endpoint). `generic` sends `{"type": "goodman.alert",
"alert": {…}}` with the full alert object; `slack` sends a Slack-compatible
`{"text": …}` message that includes matched rules, per-behavior sensor /
first-seen evidence, and (when `GOODMAN_PUBLIC_URL` is set) a deep link into
the dashboard. The same webhook also receives the weekly digest when
`-digest-interval` is set.

## Sensor

`bin/sensor [flags]` — must run as root (loads eBPF).

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `-collector` | `GOODMAN_COLLECTOR_URL` | `http://127.0.0.1:8844` | Collector base URL to POST events to. |
| `-proc-root` | `GOODMAN_PROC_ROOT` | `/proc` | Host proc mount. Set to `/host/proc` in the DaemonSet. |
| `-batch-interval` | `GOODMAN_BATCH_INTERVAL` | `1.5s` | How often to flush batched events to the collector. |
| `-metrics-addr` | `GOODMAN_METRICS_ADDR` | `:9478` | Prometheus metrics listen address (`""` disables). |
| `-comms` | `GOODMAN_EXTRA_COMMS` | *(none)* | Extra process names to watch, comma-separated (beyond built-in runtime comm names). |
| `-ingest-token` | `GOODMAN_INGEST_TOKEN` | *(empty)* | Bearer token sent with every event batch (must match the collector's ingest token). |
| `-tls-ca` | `GOODMAN_TLS_CA` | *(empty)* | PEM CA bundle to trust for an `https://` collector (private CA / self-signed). Empty uses system roots. |
| `-connect-cidr` | `GOODMAN_CONNECT_CIDR` | `0` | Aggregate public destination IPs to this IPv4 prefix (8-32) in `CONNECT` behaviors. `0` keeps exact IPs. See noise control below. |
| `-spool-events` | `GOODMAN_SPOOL_EVENTS` | `50000` | Max attributed events retained in RAM when the collector is unreachable. Oldest are evicted first; see `goodman_sensor_spool_*` metrics. |
| `-enforce-enabled` | `GOODMAN_ENFORCE_ENABLED` | `false` | Load LSM enforcement programs and poll `/v1/enforce/state` (master gate at deploy; default off). |
| `-cgroup-root` | `GOODMAN_CGROUP_ROOT` | `/sys/fs/cgroup` | Host cgroup v2 mount for enforcement scope (DaemonSet mounts when `enforce.enabled`). |
| `-enforce-cgroup` | — | *(none)* | Repeatable `SERVICE=/cgroup2/path` scopes for lab/e2e. The service must match Goodman's event service; bare paths are rejected fail-open. |
| `-watch-interval` | — | `3s` | How often to rescan `/proc` for runtime processes. |
| `-stdout` | — | `false` | Print attributed events to stdout instead of sending to the collector (debugging). |
| `-raw` | — | `false` | With `-stdout`: also print raw events including stack depth. |
| `NODE_NAME` | — | hostname | Sensor identity in event batches (set to the k8s node name in the DaemonSet). |

Built-in watched comm names (`internal/loader/loader.go` `WatchedComms`): Node
`node`, `nodejs`, `MainThread`; Python `python`, `python3`, `python3.12`,
`python3.13`; and `gunicorn`, `celery`, `uwsgi`, `uvicorn`. The sensor only
attaches to processes whose `/proc/<pid>/comm` matches one of these (plus
`-comms` extras).

`-comms` / `GOODMAN_EXTRA_COMMS` adds comma-separated comm names beyond that
list. Use this when a Python (or Node) process renames itself — e.g. Gunicorn or
Celery workers after `setproctitle` — so Goodman still watches the worker pid.

## High-risk rules

Drift is escalated from `WARN` to `CRITICAL` when a new behavior matches a
high-risk rule. Rules are a config-driven list of case-insensitive regexes matched
against the canonical behavior string — never hard-coded — so you can tune them.

Each rule supports these fields:

| Field | Meaning |
|---|---|
| `name` | Shown on alerts as `matched_rules`; keep it short and stable. |
| `pattern` | Case-insensitive regex matched against the canonical behavior string. |
| `always_on` | Fire even when no baseline exists yet, learning window included. Reserve this for behaviors that are never legitimate learning (credential reads, cloud metadata). Without it, the rule only escalates drift against an established baseline. |
| `exclude` | Regexes that suppress a match without deleting the rule, for noise tuning ("new connects are critical, except to our CDN"). |
| `action` | Enforcement posture: `"alert"` (default) raises a normal alert; `"warn"` also sets `would_block` on the alert (audit — nothing denied). `"block"` sets `would_block` and compiles literal kernel verdicts when enforcement is armed. See [`docs/enforcement.md`](enforcement.md). Unknown values fail collector startup. |

The built-in defaults (also in [`deploy/rules.example.json`](../deploy/rules.example.json); the example sets `new-exec` to `"action": "warn"` to show the field — built-in defaults with empty `-rules` stay `"alert"`):

```json
[
  { "name": "secret-read",          "always_on": true, "pattern": "^READ .*(secret|token|credential|password|shadow|wallet|\\.pem|\\.key|\\.aws|\\.ssh|\\.npmrc|\\.env|id_rsa)" },
  { "name": "cloud-metadata",       "always_on": true, "pattern": "^CONNECT 169\\.254\\.169\\.254:" },
  { "name": "new-outbound-connect", "pattern": "^CONNECT " },
  { "name": "new-exec",             "pattern": "^EXEC " }
]
```

`secret-read` and `cloud-metadata` are always-on: a package reading `.npmrc`
or calling the cloud metadata service alerts the first time it is ever seen,
even during the learning window. This closes the baseline-poisoning gap where
a package that is malicious from day one would otherwise be learned as
normal. `new-outbound-connect` and `new-exec` stay drift-only because nearly
every package legitimately connects or execs at some point; "new vs baseline"
is the only meaningful signal for them.

An example of noise tuning with `exclude`:

```json
{ "name": "new-outbound-connect", "pattern": "^CONNECT ",
  "exclude": ["^CONNECT 10\\.", "^CONNECT 151\\.101\\."] }
```

### Taming CONNECT noise

`CONNECT` behaviors canonicalize to `ip:port`, so a dependency talking to a CDN
or any DNS-rotated host produces a "new" behavior on every address change and
can flood the fingerprint set and alerts. Two independent controls help:

- **Sensor CIDR aggregation** (`-connect-cidr` / `GOODMAN_CONNECT_CIDR`):
  collapse public destination IPs to a prefix at capture time, so
  `52.84.12.7:443` and `52.84.250.9:443` both become
  `CONNECT 52.84.0.0/16:443`. Private, loopback, link-local, and cloud-metadata
  addresses always stay exact, so internal calls and metadata access remain
  precise. `/16` is a good starting point for large CDNs.
- **Rule excludes** (above): suppress connects you have vetted (a known CDN
  range, your database subnet) without disabling the whole rule.

Aggregation happens on the sensor before events leave the node, so it also
reduces the volume shipped to the collector.

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
| `sensor.image` / `collector.image` | `ghcr.io/hi-heisenbug/*:0.1.0` | Container images. |
| `sensor.extraComms` | `""` | Extra process names to watch. |
| `sensor.spoolEvents` | `50000` | Sensor in-memory retry spool when the collector is down. |
| `sensor.metricsPort` | `9478` | Sensor Prometheus port. |
| `collector.replicas` | `1` | Collector replica count. Values `>1` require `postgres.dsn`, set `GOODMAN_HA_REPLICAS`, render a PodDisruptionBudget (`minAvailable: 1`), and prefer spreading collectors across nodes. SQLite PVC persistence is disabled when replicas `>1`. |
| `collector.service.type` / `.port` | `ClusterIP` / `8844` | Collector service. |
| `postgres.dsn` | `""` | Postgres DSN; empty → embedded SQLite. |
| `postgres.sqlitePath` | `/data/goodman.db` | SQLite path (when `dsn` is empty). |
| `store.persistence.enabled` | `true` | Mount a PVC at `/data` for SQLite (ignored when `postgres.dsn` is set). |
| `store.persistence.size` | `10Gi` | PVC size when the chart creates the claim. |
| `store.persistence.storageClass` | `""` | StorageClass; empty uses the cluster default. |
| `store.persistence.existingClaim` | `""` | Use an existing PVC name instead of creating one. |
| `learningWindow.obsCount` | `500` | Observations before baseline promotion. |
| `learningWindow.minAgeHours` | `24` | Wall-clock hours before promotion. |
| `attribution.tier` | `perfmap` | `perfmap` (Tier 1) or `v8native` (Tier 2, not in v1). |
| `attribution.requireNodeOptions` | `true` | Whether install notes remind you to inject `NODE_OPTIONS`. |
| `rules` | `""` | High-risk rules JSON (empty → built-in defaults). |
| `rbac.create` | `true` | Create the sensor ServiceAccount + ClusterRole. |
| `auth.enabled` | `true` | Create the `<release>-auth` Secret and require tokens on the API. |
| `auth.ingestToken` / `auth.apiToken` | `""` | Explicit tokens. Empty generates random tokens on first install (stable across upgrades). |
| `auth.existingSecret` | `""` | Use an existing Secret with keys `ingest-token` and `api-token` instead. |
| `collector.tls.secretName` | `""` | `kubernetes.io/tls` Secret to serve the API over HTTPS; sensors trust its `ca.crt`. |
| `notifications.webhookUrl` | `""` | Alert webhook URL (empty = disabled). |
| `notifications.format` | `generic` | `generic` or `slack`. |
| `notifications.minSeverity` | `WARN` | Lowest severity forwarded. |
| `notifications.token` | `""` | Bearer token sent to the webhook. |
| `notifications.digestInterval` | `168h` | Weekly digest cadence when `webhookUrl` is set (`""` / `0` disables). Fires once at startup. |
| `notifications.digestAlertBudget` | `5` | Soft open-alert noise target quoted in the digest. |
| `publicUrl` | `""` | Dashboard base URL for Slack deep links. |
| `reachability.interval` | `1h` | Recompute stored reachability reports (`""` / `0` disables). |
| `reachability.osv` | `false` | Enrich scheduled recomputes with OSV.dev. |
| `reachability.osvEndpoint` | `""` | Optional internal/proxied OSV querybatch endpoint. |
| `retention` | `""` | Prune resolved alerts older than this Go duration (e.g. `720h`). |
| `webhook.enabled` | `false` | Enable the NODE_OPTIONS mutating admission webhook (injects the perf-map flags into pods in namespaces labeled `goodman.io/inject=enabled`). |
| `webhook.port` | `8443` | Port the collector serves the webhook on. |
| `webhook.failurePolicy` | `Ignore` | `Ignore` never blocks pod creation if the collector is down; `Fail` is stricter. |

Example production install:

```bash
helm install goodman deploy/helm/goodman \
  --set cluster=prod \
  --set-string registries='npm\,pypi' \
  --set collector.image=ghcr.io/hi-heisenbug/collector:0.1.0 \
  --set sensor.image=ghcr.io/hi-heisenbug/sensor:0.1.0 \
  --set postgres.dsn='postgres://goodman:secret@db:5432/goodman?sslmode=require' \
  --set notifications.webhookUrl='https://hooks.slack.com/services/T000/B000/XXXX' \
  --set notifications.format=slack \
  --set retention=720h
```

Read the generated API token after install (needed by the dashboard and
`goodmanctl`):

```bash
kubectl get secret goodman-auth -o jsonpath='{.data.api-token}' | base64 -d
```
