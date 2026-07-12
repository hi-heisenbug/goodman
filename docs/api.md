# API reference

The collector serves a small REST API, a Server-Sent-Events stream, Prometheus
metrics, and the dashboard. Base URL defaults to `http://<collector>:8844`.

All request/response bodies are JSON. Types are defined in `internal/model`.

## Authentication

When the collector is started with tokens (the Helm chart does this by
default), requests must carry a bearer token:

```
Authorization: Bearer <token>
```

| Endpoints | Token | 
|---|---|
| `POST /v1/events` | ingest token (`GOODMAN_INGEST_TOKEN`) |
| `/v1/alerts*`, `/v1/fingerprints`, `/v1/stream` | API token (`GOODMAN_API_TOKEN`) |
| `/v1/healthz`, `/v1/readyz`, `/metrics`, dashboard assets | none |

`GET /v1/stream` also accepts `?token=<api-token>` because `EventSource`
cannot set request headers. A missing or wrong token returns
`401 {"error": "unauthorized"}`. With no tokens configured (bare local runs),
all endpoints are open.

## Endpoints

### `GET /v1/healthz`

Liveness probe. Always `200` when the collector is up.

```json
{ "status": "ok" }
```

### `GET /v1/readyz`

Readiness probe. `200 {"status": "ready"}` when the datastore answers a ping;
`503 {"status": "unavailable", "error": "…"}` when it does not. Kubernetes uses
this to stop routing to a collector that cannot persist events.

### `POST /v1/events`

Sensor ingestion. Accepts an `EventBatch`; may be gzip-compressed
(`Content-Encoding: gzip`). Runs fingerprint aggregation and the diff engine, then
broadcasts to the SSE stream. Body limit 64 MiB.

```jsonc
// request
{
  "sensor": "node-01",
  "events": [
    {
      "service": "web",
      "package": "good-pkg",
      "version": "1.0.1",
      "type": 2,                       // 1=FILE_OPEN, 2=NET_CONNECT, 3=PROC_EXEC
      "behavior": "CONNECT 169.254.169.254:80",
      "timestamp": 1783453745485152329 // unix ns
    }
  ]
}
```

```json
// response
{ "ingested": 1, "alerts": 0 }
```

### `GET /v1/alerts?status=<open|acknowledged|resolved>`

List alerts, newest first (max 500). Omit `status` for all.

```json
[
  {
    "id": "3fa05cdd5cb4bfd1dce898f6",
    "service": "web",
    "package": "good-pkg",
    "old_version": "1.0.0",
    "new_version": "1.0.1",
    "severity": "CRITICAL",
    "new_behaviors": [
      "READ /tmp/goodman-fake-secrets/credentials",
      "CONNECT 169.254.169.254:80"
    ],
    "detected_at": 1783453745485152329,
    "status": "open"
  }
]
```

### `POST /v1/alerts/{id}/ack`

Mark an alert acknowledged. `200` on success, `404` if unknown.

```json
{ "status": "acknowledged" }
```

### `POST /v1/alerts/{id}/resolve`

Mark an alert resolved. `200` on success, `404` if unknown.

### `GET /v1/fingerprints?service=<s>&package=<p>`

List learned fingerprints, optionally filtered. Both params optional.

```json
[
  {
    "service": "web",
    "package": "good-pkg",
    "version": "1.0.0",
    "behaviors": {
      "READ /app/node_modules/good-pkg/**": { "count": 8, "first": 1783..., "last": 1783... },
      "CONNECT 10.0.0.5:5432":              { "count": 8, "first": 1783..., "last": 1783... }
    },
    "first_seen": 1783453740000000000,
    "last_seen": 1783453760000000000,
    "obs_count": 16,
    "is_baseline": true
  }
]
```

### `GET /v1/stream`

Requires the API token via header or `?token=` (see Authentication).

Server-Sent Events. Two event types are pushed as they happen:

- `event: events` — a JSON array of `Attributed` events just ingested.
- `event: alerts` — a JSON array of `Alert`s just created or updated.

A heartbeat comment (`: hb`) is sent every 15s. Slow consumers are dropped rather
than allowed to block ingestion. This is what `goodmanctl tail` and the
dashboard's live view consume.

```
event: alerts
data: [{"id":"3fa05cdd…","severity":"CRITICAL",…}]
```

### `GET /metrics`

Prometheus metrics (see [Observability](#observability)).

### `GET /*`

Everything else serves the embedded dashboard (SPA fallback to `index.html`).

## Observability

Both binaries expose Prometheus metrics.

**Collector** (`/metrics` on the API port):

| Metric | Type | Labels |
|---|---|---|
| `goodman_collector_events_ingested_total` | counter | — |
| `goodman_collector_alerts_total` | counter | `severity` |

**Sensor** (`/metrics` on `-metrics-addr`, default `:9478`):

| Metric | Type | Labels |
|---|---|---|
| `goodman_sensor_events_total` | counter | `type` (FILE_OPEN/NET_CONNECT/PROC_EXEC) |
| `goodman_sensor_attributed_total` | counter | `outcome` (package/app/unknown) |
| `goodman_sensor_channel_drops_total` | counter | — |
| `goodman_sensor_ringbuf_drops_total` | gauge | — |
| `goodman_sensor_watched_pids` | gauge | — |
| `goodman_sensor_batches_total` | counter | `result` (ok/error) |

`goodman_sensor_attributed_total{outcome="package"}` over the sum of all outcomes
is your live **attribution success rate**. Rising `*_drops_total` means events are
being shed under load — investigate before trusting completeness.

## CLI equivalents (`goodmanctl`)

| Command | Uses |
|---|---|
| `goodmanctl tail` | `GET /v1/stream` |
| `goodmanctl alerts [-status S] [-json]` | `GET /v1/alerts` |
| `goodmanctl ack <id>` | `POST /v1/alerts/{id}/ack` |
| `goodmanctl fingerprints [-service S] [-package P] [-json]` | `GET /v1/fingerprints` |
| `goodmanctl attribute -pid N [-stacks]` | loads eBPF directly (needs root) |

Point any of them at a remote collector with `-collector URL` or
`GOODMAN_COLLECTOR_URL`.
