# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Oversized collector, sensor, CLI, loader, API, and store modules are split by
  responsibility; production complexity, duplicate code, dead code, static
  analysis, vulnerabilities, and module tidiness now have one `make quality`
  gate enforced by CI.
- The demo-video workspace now uses one canonical Remotion/Chromium pipeline
  and a patched, audit-clean ESLint dependency chain.
- The minimum Go version is now 1.25 so Goodman can use patched pgx and
  `x/text` releases for reachable SQL-sanitization and text-processing
  vulnerabilities; setup, CI, and container builders were updated together.
- Enforcement verdicts are isolated per service and local cgroup through
  composite BPF keys; sibling services on the same node no longer share deny
  literals.
- File/exec detection now resolves `openat*` dirfds, cwd-relative paths,
  symlinks, and container mount namespaces to the same kernel path identity
  used by LSM enforcement. Unresolved paths remain alertable but fail open.
- LSM hooks now use typed `BPF_PROG` arguments and preserve earlier LSM return
  values, allowing live file, connect, and exec enforcement to load correctly.
- New-version drift is still evaluated when its first event batch crosses the
  learning threshold, and denied events retain short-lived package context so
  kernel blocks upgrade the correct alert.

### Added

- Portable one-command setup: `scripts/setup-everything.sh` selects local Go or
  Docker for the no-root demo and can install supported Linux prerequisites,
  optionally install OpenClaw, configure its user systemd service, and run both
  live eBPF proofs.
- Portable binaries now cross-build for macOS and Windows on amd64/arm64, and
  `make docker-e2e` runs both real-kernel proofs from a disposable privileged
  Linux container with the required host kernel filesystems mounted explicitly.
- OpenClaw integration: `scripts/integrate-openclaw.sh` creates a Tier-1 Node
  launcher and stable service config, supports optional user-local installation,
  persistent user systemd setup, and safe Kubernetes patches that preserve
  existing `NODE_OPTIONS`.
- Versioned ClawHub skill attribution from JavaScript stack frames backed by a
  ClawHub-accepted skill card marker plus matching `.clawhub/origin.json` and
  workspace lock metadata, plus an OpenClaw-shaped demo/replay and real eBPF
  contract test.
- `GET /v1/snapshot` and `goodmanctl snapshot` expose open alerts and
  fingerprints as the stable `goodman.snapshot/v1` bundle for SIEM and agent
  consumers.
- `GET /v1/export` and `goodmanctl export` expose every alert state, all
  fingerprints, persisted reachability, coverage, enforcement, and the explicit
  live-event delivery contract as `goodman.export/v1`.
- The default sensor watch list includes the current OpenClaw Gateway Linux comm
  (`openclaw-gatewa`).
- True HA collector (deferred Phase 5): `GOODMAN_HA_REPLICAS` / `-ha-replicas`
  with Postgres required when `>1`; `store.WithLeader` Postgres advisory locks
  for retention/reachability/digest loops; transactional `UpsertAlert` with
  concurrency test; Helm fails when `collector.replicas > 1` without
  `postgres.dsn`, sets HA env, PodDisruptionBudget, and pod anti-affinity.
  SSE per-replica limitation documented. Two-replica live Postgres proof is a
  human/CI follow-up — `docs/release.md`. Redis not added.
- Multi-cluster baseline export/import (deferred Phase 4): `GET
  /v1/fingerprints/export` and `POST /v1/fingerprints/import` (API token);
  `goodmanctl fingerprints export|import`; `origin` provenance on fingerprints
  (`local` | `imported`); dashboard "Imported" tag; import conflict matrix
  never clobbers locally learned baselines.
- Transactional fingerprint merge (deferred Phase 5a): `store.MergeFingerprint`
  wraps read-modify-write in a transaction (Postgres `SELECT FOR UPDATE`;
  SQLite tx-only). `fingerprint.Engine.Ingest` uses it so concurrent ingests
  union behaviors and sum `obs_count` instead of last-writer-wins. Concurrency
  test in `internal/fingerprint`. Full HA replicas + leader election shipped
  in Phase 5 — `docs/research/collector-ha.md`, `docs/deployment.md`.
- Phase 6 eBPF LSM enforcement (block mode): fail-open kernel denies via LSM
  hooks; `action: "block"` on rules; `GOODMAN_ENFORCE_ENABLED` master gate
  (default false); `goodmanctl enforce on|off|status`; dashboard `BLOCKED`
  chip; `docs/enforcement.md` and `docs/pilot-runbook.md`. Human `sudo make e2e`
  on an LSM-capable kernel still required for live deny proof.
- enforce=warn audit mode (deferred Phase 3): rules accept `action` (`alert`|
  `warn`); matching `warn` sets `would_block` on alerts, increments
  `goodman_enforce_would_block_total{rule}`, surfaces a dashboard chip +
  Coverage “Would block” KPI, and appears in the weekly digest.
- Python Tier-1 attribution (deferred Phase 2): CPython 3.12+ perf
  trampolines via `PYTHONPERFSUPPORT=1`; reuse perf-map resolution for `py::`
  symbols, `PathToPyPackage` + `*.dist-info` versions, admission webhook env
  injection, extended `WatchedComms`, and `-comms` / `GOODMAN_EXTRA_COMMS` for
  renamed worker comms. Research: `docs/research/python-attribution.md`.
- Collector durability (deferred Phase 1): Helm `store.persistence` mounts a
  PVC for SQLite by default; sensors buffer failed collector POSTs in a
  bounded RAM spool (`-spool-events` / `GOODMAN_SPOOL_EVENTS`, metrics
  `goodman_sensor_spool_*`) and drain on recovery.
- Tier-2 research decision (`docs/research/tier2-attribution.md`): PARK
  (year-scale); keep Tier-1 + admission webhook as the production path.
- Coverage and trust panel (Phase 9): `GET /v1/coverage` + dashboard Coverage
  tab with sensor health, attribution KPI, namespace injection gaps, and
  alert-budget burn rate. Sensors heartbeat on empty batches and POST
  in-cluster namespace coverage; Helm sensor RBAC includes `namespaces`.
- Pilot heartbeat (Phase 8): week-over-week reachability deltas on
  `GET /v1/report`, weekly digest delivery via the existing webhook
  (`-digest-interval`, Helm `notifications.digestInterval=168h`), and Slack
  alert messages enriched with matched rules, sensor, first-seen times, and
  dashboard deep links (`-public-url`).
- Five-minute product wow (`make demo` / `goodman-demo` / `make demo-check`):
  seeds alerts and fingerprints, preloads Reachability at 1,400 declared /
  240 executed, prints a 60-second guided script, then live-replays the
  2026 Mini-Shai-Hulud behavior profile with rule chips. README
  quickstart is this path.
- Always-on high-risk rules (`always_on` in the rules JSON): credential reads
  and cloud-metadata access now alert from the first observation, during the
  learning window and with no baseline, closing the baseline-poisoning gap.
- Per-rule `exclude` patterns for noise tuning without deleting a rule.
- Performance benchmarks (`make bench`) for the collector ingest pipeline and
  sensor canonicalization, and `docs/performance.md` documenting measured
  throughput (~10.7k events/sec on SQLite), sensor overhead methodology, and the
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

### Removed

- The unused static screenshot/slideshow demo pipeline and redundant demo shell
  wrapper were removed; the real interactive walkthrough is the sole source.

### Fixed

- Sensor enforcement polling now URL-escapes arbitrary node names correctly.
- Portable demo clients connect through loopback when the server binds a
  wildcard address, including valid bracketed IPv6 URLs.
- The production sensor image derives its eBPF target from Docker
  `TARGETARCH`, so amd64 and arm64 builds no longer share an x86-only object.

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
