# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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
