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
    "matched_rules": ["cloud-metadata", "new-outbound-connect", "secret-read"],
    "would_block": false,
    "blocked": false,
    "evidence": [
      {
        "behavior": "READ /tmp/goodman-fake-secrets/credentials",
        "rules": ["secret-read"],
        "sensor": "node-01",
        "first_seen": 1783453745485152329
      }
    ],
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
    "is_baseline": true,
    "origin": "local"
  }
]
```

### `GET /v1/fingerprints/export`

Export promoted baselines for multi-cluster seeding. Requires the API token.
Only rows with `is_baseline=true` are included; learning fingerprints are
omitted.

```json
{
  "schema": "goodman.fingerprints.export/v1",
  "exported_at": 1783453745485152329,
  "collector": "goodman-collector-0",
  "fingerprints": [
    {
      "service": "web",
      "package": "good-pkg",
      "version": "1.0.0",
      "behaviors": { "READ /app/node_modules/good-pkg/**": { "count": 8, "first": 0, "last": 0 } },
      "first_seen": 0,
      "last_seen": 0,
      "obs_count": 8,
      "is_baseline": true,
      "origin": "local"
    }
  ]
}
```

`schema` is a hard version gate: import rejects unknown values with `400`.

### `POST /v1/fingerprints/import`

Import baselines from an export file. Requires the API token. Request body is
the same envelope as export. Conflict rules (key = `service` + `package` +
`version`):

| Local state | Result |
|---|---|
| no row | insert with `origin=imported` → `imported` |
| row with `origin=local` (learning or baseline) | skip, keep local row → `skipped_local` |
| row with `origin=imported` | replace (idempotent re-import) → `replaced` |
| incoming `is_baseline=false` | never written → `ignored_non_baseline` |

Local observation always outranks a file; import never clobbers a locally
learned baseline.

```json
{ "imported": 41, "skipped_local": 3, "replaced": 0, "ignored_non_baseline": 0 }
```

### `POST /v1/report`

Build the runtime reachability report from an uploaded npm lockfile. The
request body is the raw `package-lock.json` (v1/v2/v3). The collector joins the
declared dependencies against the fingerprints it has observed and returns the
report as JSON. Query params:

- `service` (optional): limit executed-package matching to one service.
- `osv=1` (optional): enrich with OSV.dev advisories (needs collector egress).

```jsonc
// response
{
  "service": "web",
  "declared_count": 1400,
  "executed_count": 240,
  "vuln_rows": [                     // packages with advisories, reachable first
    { "name": "lodash", "declared_version": "4.17.20", "executed": true,
      "behaviors": 3, "vulns": [ { "id": "GHSA-35jh-r3h4-6jhm", "severity": "HIGH" } ] }
  ],
  "rows": [ /* every declared package, with executed/behaviors */ ]
}
```

Add `persist=1` to store the uploaded lockfile and this snapshot; the collector
then keeps that snapshot fresh on its `-reachability-interval` cadence and the
dashboard can load it instantly (see `GET /v1/report`). The service scope is the
`service` query param (empty = all services).

This backs the dashboard's Reachability tab and mirrors `goodmanctl report`.

### `GET /v1/report?service=<s>`

Return the most recently stored reachability snapshot for a service scope
(`404` when none has been uploaded with `persist=1`). Lets the dashboard show
current numbers on load without re-uploading a lockfile. When a previous
snapshot exists (after a scheduled refresh or a second upload), the response
includes a week-over-week `delta`.

```jsonc
// response
{
  "computed_at": 1783921240344971762, // unix ns of the snapshot
  "osv": true,                         // whether it was OSV-enriched
  "report": { /* the same Report shape as POST returns */ },
  "previous": {                        // omitted on the first snapshot
    "computed_at": 1783316440344971762,
    "report": { /* prior Report */ }
  },
  "delta": {                           // omitted on the first snapshot
    "executed": 12,
    "declared": 0,
    "reachable_vulns": -1,
    "new_executed_packages": ["left-pad@1.3.0"],
    "new_reachable_vuln_ids": ["GHSA-…"],
    "previous_computed_at": 1783316440344971762
  }
}
```

### `GET /v1/coverage`

Requires the API token.

Returns the Coverage and trust panel snapshot: per-node sensor health,
attribution KPI (`package` / `<app>` / `<unknown>`), namespace injection gaps,
and alert-budget burn rate (alerts in the last 24h vs target).

```jsonc
{
  "sensors": [
    {"name": "node-a", "status": "running", "last_seen": 1720000000000000000, "events_per_sec": 12.4, "events_total": 900}
  ],
  "attribution": {
    "package": 1200, "app": 80, "unknown": 40,
    "success_rate": 0.909,
    "top_unknown": [{"service": "payments", "count": 22}]
  },
  "namespaces": [
    {"name": "staging", "inject_label": false, "pods_total": 3, "pods_with_node_options": 0, "pods_without": 3}
  ],
  "alert_budget": {"target_per_day": 5, "alerts_last_24h": 2, "would_block_last_24h": 1}
}
```

### `POST /v1/coverage`

Requires the ingest token. Sensors POST namespace injection coverage:

```json
{"sensor": "node-a", "namespaces": [{"name": "staging", "inject_label": false, "pods_total": 3, "pods_with_node_options": 0, "pods_without": 3}]}
```

### `GET /v1/enforce/state`

Requires the ingest token. Sensors poll this (~500ms) for verdicts and as a
heartbeat. Optional query params: `sensor`, `enforcement_active=true|false`.

```json
{"enabled": true, "rev": 3,
 "verdicts": {"open": ["/etc/shadow"], "connect": [{"addr":"169.254.169.254","port":0}], "exec": []},
 "skipped": []}
```

When `enabled` is false, sensors zero the kernel deadline immediately on the
next poll.

### `GET /v1/enforce`

Requires the API token. Operator status: master gate, runtime switch, verdict
counts, skipped uncompilable behaviors, per-sensor heartbeat and
`enforcement_active`.

### `POST /v1/enforce/on` / `POST /v1/enforce/off`

Requires the API token. Toggle the runtime enforcement switch (persisted). `on`
returns `409` if the collector master gate (`-enforce-enabled`) is off.

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
| `goodmanctl fingerprints export` | `GET /v1/fingerprints/export` |
| `goodmanctl fingerprints import <file>` | `POST /v1/fingerprints/import` |
| `goodmanctl report -lockfile package-lock.json [-service S] [-osv] [-o FILE]` | `GET /v1/fingerprints` + OSV.dev (the dashboard Reachability tab uses `POST /v1/report`) |
| `goodmanctl attribute -pid N [-stacks]` | loads eBPF directly (needs root) |

### `goodmanctl report`

Builds the **runtime reachability report**: it parses an npm
`package-lock.json` (v1/v2/v3), joins the declared dependencies against the
packages Goodman observed executing, and (with `-osv`) enriches them with
OSV.dev advisories. The markdown output ranks vulnerabilities in
**executing** packages first, since those are reachable at runtime, and lists
declared-but-never-executed packages separately as deprioritization / pruning
candidates.

```bash
goodmanctl report -lockfile package-lock.json -service web -osv -o reachability.md
```

`-osv` needs outbound network to `api.osv.dev`; without it the report still
shows executed-vs-declared reachability. This is the artifact to hand a
security team: it turns "you have N vulnerable dependencies" into "these few
actually run, patch them first."

Point any of them at a remote collector with `-collector URL` or
`GOODMAN_COLLECTOR_URL`.
