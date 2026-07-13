# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Five-minute product wow (`make demo` / `goodmanctl demo` / `make demo-check`):
  seeds alerts and fingerprints, preloads Reachability at 1,400 declared /
  240 executed, prints a 60-second guided script, then live-replays the
  2018 event-stream / flatmap-stream attack with rule chips. README
  quickstart is this path.
- Always-on high-risk rules (`always_on` in the rules JSON): credential reads
  and cloud-metadata access now alert from the first observation, during the
  learning window and with no baseline, closing the baseline-poisoning gap.
- Per-rule `exclude` patterns for noise tuning without deleting a rule.
- Performance benchmarks (`make bench`) for the collector ingest pipeline and
  sensor canonicalization, and `docs/performance.md` documenting measured
  throughput (~16k events/sec on SQLite), sensor overhead methodology, and the
  attribution-quality KPI.
- Sensor CONNECT noise control: `-connect-cidr` / `GOODMAN_CONNECT_CIDR`
  aggregates public destination IPs to an IPv4 prefix (e.g. /16), collapsing
  CDN and DNS rotation into one behavior. Private, loopback, link-local, and
  cloud-metadata addresses stay exact.
- Alert evidence: every alert now carries `matched_rules` (which high-risk
  rules fired) and per-behavior `evidence` (rule names, reporting sensor,
  first-seen timestamp). Shown in the dashboard as rule chips and by
  `goodmanctl alerts`.
- Tracked schema migrations (`schema_migrations` table) so non-idempotent
  migrations run exactly once per database.
- Attack replay corpus (`make replay`): benign reproductions of the
  event-stream, eslint-scope, ua-parser-js, and node-ipc npm supply-chain
  attacks, each asserting Goodman raises the expected CRITICAL alert. See
  `docs/replay-corpus.md`.
- NODE_OPTIONS mutating admission webhook (`webhook.enabled=true`): injects the
  Tier-1 perf-map flags into pods in namespaces labeled
  `goodman.io/inject=enabled`, so no application manifest change is needed. It
  appends to an existing NODE_OPTIONS, is idempotent, leaves valueFrom vars
  alone, and serves over HTTPS with a chart-generated CA stable across upgrades.
- Dashboard Reachability tab: upload a package-lock.json in the browser to see
  declared-vs-executed packages and reachable vulnerabilities (reachable ranked
  first), backed by a new `POST /v1/report` collector endpoint.
- Persisted, self-refreshing reachability: `POST /v1/report?persist=1` stores
  the lockfile and snapshot; `GET /v1/report` returns the latest snapshot so
  the dashboard loads it instantly on open; and the collector recomputes stored
  snapshots on `GOODMAN_REACHABILITY_INTERVAL` (optionally OSV-enriched via
  `GOODMAN_REACHABILITY_OSV`) as fingerprints change.
- `goodmanctl report`: the runtime reachability report. Parses a
  `package-lock.json` (v1/v2/v3), joins declared dependencies against packages
  Goodman observed executing, and optionally enriches with OSV.dev. Ranks
  vulnerabilities in executing packages first and lists never-executed
  packages as pruning candidates.
- Bearer-token authentication for the collector API: `GOODMAN_INGEST_TOKEN`
  protects sensor ingestion and `GOODMAN_API_TOKEN` protects the
  alerts/fingerprints/stream API. The sensor, `goodmanctl`, and the dashboard
  (token prompt with localStorage persistence) all send tokens automatically.
- TLS serving on the collector (`GOODMAN_TLS_CERT`/`GOODMAN_TLS_KEY`) and
  private-CA trust in the sensor (`GOODMAN_TLS_CA`).
- Webhook alert notifications (`GOODMAN_WEBHOOK_URL`), with `generic` JSON and
  `slack` payload formats, severity filtering, and retried asynchronous
  delivery that never blocks ingestion.
- `GET /v1/readyz` readiness endpoint that verifies datastore connectivity;
  the Helm chart's readiness probe now uses it.
- Alert retention: `GOODMAN_RETENTION` prunes resolved alerts older than the
  window (open/acknowledged alerts are never pruned).
- Helm: auto-generated `<release>-auth` token Secret (stable across upgrades),
  `auth.*`, `collector.tls.secretName`, `notifications.*`, and `retention`
  values.
- Store unit tests covering fingerprints, baseline lookup, alert
  merge/escalation, status transitions, and retention pruning.

### Changed

### Deprecated

### Removed

### Fixed

### Security

## [0.1.0] - 2026-07-08

Initial public release.

### Added

- eBPF capture of `openat`, `connect`, and `execve` syscalls, including the
  user-space stack for each event.
- Tier-1 perf-map attribution that maps captured syscalls to the responsible
  `package@version`.
- Fingerprint aggregation with baseline promotion per (service, package,
  version).
- Config-driven drift diff engine, including high-risk drift rules.
- Collector service exposing a REST + Server-Sent Events (SSE) API.
- Embedded React dashboard served by the collector.
- `goodmanctl` command-line interface.
- Prometheus metrics for the sensor and collector.
- Persistent store with both Postgres and SQLite backends.
- Docker images for the sensor and collector.
- Helm chart for Kubernetes deployment.
- Benign-drift end-to-end test.

[Unreleased]: https://github.com/hi-heisenbug/goodman/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/hi-heisenbug/goodman/releases/tag/v0.1.0
