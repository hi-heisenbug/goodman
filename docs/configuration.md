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
| `-retention` | `GOODMAN_RETENTION` | `0` | Prune resolved alerts older than this (`720h` = 30 days). `0` keeps them forever. Open/acknowledged alerts are never pruned. |

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
the dashboard, `goodmanctl`, and any API client use the API token. The SSE
stream additionally accepts `?token=<api-token>` because `EventSource` cannot
set headers. `/v1/healthz`, `/v1/readyz`, `/metrics`, and the static dashboard
assets are never token-protected. When the dashboard receives a 401 it shows a
token prompt and stores the entered token in the browser's localStorage.

**Alert notifications.** With `GOODMAN_WEBHOOK_URL` set, every alert at or above
`GOODMAN_WEBHOOK_MIN_SEVERITY` is POSTed to the webhook from a background
worker (bounded queue, three delivery attempts with backoff; ingestion is never
blocked by a slow endpoint). `generic` sends `{"type": "goodman.alert",
"alert": {…}}` with the full alert object; `slack` sends a Slack-compatible
`{"text": …}` message that also works with Mattermost and Rocket.Chat.

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

Each rule supports four fields:

| Field | Meaning |
|---|---|
| `name` | Shown on alerts as `matched_rules`; keep it short and stable. |
| `pattern` | Case-insensitive regex matched against the canonical behavior string. |
| `always_on` | Fire even when no baseline exists yet, learning window included. Reserve this for behaviors that are never legitimate learning (credential reads, cloud metadata). Without it, the rule only escalates drift against an established baseline. |
| `exclude` | Regexes that suppress a match without deleting the rule, for noise tuning ("new connects are critical, except to our CDN"). |

The built-in defaults (also in [`deploy/rules.example.json`](../deploy/rules.example.json)):

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
| `auth.enabled` | `true` | Create the `<release>-auth` Secret and require tokens on the API. |
| `auth.ingestToken` / `auth.apiToken` | `""` | Explicit tokens. Empty generates random tokens on first install (stable across upgrades). |
| `auth.existingSecret` | `""` | Use an existing Secret with keys `ingest-token` and `api-token` instead. |
| `collector.tls.secretName` | `""` | `kubernetes.io/tls` Secret to serve the API over HTTPS; sensors trust its `ca.crt`. |
| `notifications.webhookUrl` | `""` | Alert webhook URL (empty = disabled). |
| `notifications.format` | `generic` | `generic` or `slack`. |
| `notifications.minSeverity` | `WARN` | Lowest severity forwarded. |
| `notifications.token` | `""` | Bearer token sent to the webhook. |
| `retention` | `""` | Prune resolved alerts older than this Go duration (e.g. `720h`). |

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
