# Goodman: Path to First Paid Pilot (Sep 30, 2026)

> **Audience:** the maintainer and any coding agent continuing this work.
> **Context:** `plan.md` was the zero-to-v0.1 build plan and is complete. This
> plan covers the next 11 weeks: everything here exists to convert a design
> partner into the first paid pilot. Work the phases **in order**; each has a
> Definition of Done (DoD).

## Strategic frame

Drift detection is an insurance product: a real supply-chain attack inside a
pilot cluster is rare, so a 30-day POV can be silent and read as "it does
nothing." The pilot needs an aspirin next to the insurance: daily visible value
from the same telemetry.

- **The moat** is attribution: kernel syscall to `package@version`.
- **The first revenue** is the runtime reachability report: which declared
  dependencies actually execute in production, joined with known
  vulnerabilities. Vulnerability prioritization has existing budget.
- **Pilot survival** depends on evidence-rich, low-noise alerts that produce
  value from minute one, not after a 24-hour learning window.

## Phase 1: Attack replay corpus

Reproduce famous npm supply-chain attacks as benign, self-contained replay
scenarios that drive the full collector pipeline (store, fingerprint, diff,
alert) and assert the expected CRITICAL alert. One scenario file per attack:

- **event-stream (2018):** injected dependency reads a cryptocurrency wallet
  file and exfiltrates it.
- **eslint-scope (2018):** postinstall-style credential theft of `.npmrc`.
- **ua-parser-js (2021):** hijacked version drops and executes a miner binary
  and reads credentials.
- **node-ipc (2022):** protestware overwrites files outside the package.

Each scenario is simultaneously a regression test, a live demo
(`make replay`), and a content-marketing artifact ("would Goodman have caught
X?" answers with a runnable command).

**DoD:** `make replay` runs all scenarios against a fresh collector with no
root and exits non-zero unless every scenario raises exactly the expected
CRITICAL alert with the expected matched rule. Documented in
`docs/replay-corpus.md`.

## Phase 2: Always-on high-risk rules + alert evidence

Two product gaps, one change surface (`internal/diff`):

1. **Baseline poisoning / cold start.** Behavior observed during the learning
   window is baselined as normal, and the product is silent until promotion.
   Fix: high-risk rules fire **regardless of baseline state**. Reading
   `.npmrc` or connecting to cloud metadata from inside `node_modules` alerts
   in minute one, learning window or not.
2. **Alert evidence.** Alerts must carry what an analyst needs to triage:
   which rule matched, the sensor that saw it, and when each new behavior was
   first observed. Today the alert has only behavior strings.

**DoD:** a high-risk behavior on a never-seen package raises an alert with no
baseline; every alert lists matched rule names; both SQL dialects migrated;
dashboard shows the rule; docs and CHANGELOG updated; `make smoke` and the
Phase 1 replay corpus still pass.

## Phase 3: Runtime reachability report

`goodmanctl report`: parse the customer's `package-lock.json`, join declared
packages against observed fingerprints from the collector, optionally enrich
with the OSV.dev API, and emit a markdown report:

- declared vs executed package counts (the "1,400 shipped, 240 run" headline)
- executed packages with known vulnerabilities, ranked first (they run!)
- declared-but-never-executed vulnerable packages (deprioritization list)
- learning/baseline coverage stats

This report is the artifact a pilot champion forwards to their boss.

**DoD:** `goodmanctl report -lockfile package-lock.json` produces a complete
markdown report offline; `-osv` adds OSV enrichment when the network allows;
unit tests cover lockfile parsing (v2/v3) and report assembly; documented in
`docs/api.md` and the README.

## Phase 4: NODE_OPTIONS admission webhook

Tier-1 attribution needs `NODE_OPTIONS=--perf-basic-prof
--interpreted-frames-native-stack` on workloads. Manual injection is the
number one install objection. Ship a mutating admission webhook (part of the
collector binary, enabled by Helm) that injects the env var into pods in
namespaces labeled `goodman.io/inject=enabled`, appending to any existing
NODE_OPTIONS.

**DoD:** `helm install` with `webhook.enabled=true` creates the
MutatingWebhookConfiguration with a generated CA; a labeled namespace gets
NODE_OPTIONS injected on pod create; unlabeled namespaces are untouched; unit
tests cover the JSON-patch logic; documented in `docs/deployment.md`.

## Phase 5: Noise controls

`new-outbound-connect` as a default CRITICAL rule will drown in CDN and DNS
rotation. Ship:

- per-rule `exclude` patterns (regexes that suppress a match), so operators
  tune rules without deleting them
- optional public-IP CIDR aggregation in the sensor canonicalizer
  (`CONNECT 52.84.0.0/16:443` instead of per-IP churn), private ranges stay
  exact

**DoD:** rules with excludes are honored by the diff engine and documented in
`docs/configuration.md`; CIDR aggregation is opt-in via sensor flag; unit
tests for both.

## Phase 6: Performance benchmark + attribution quality

Security teams will not deploy a privileged DaemonSet without overhead
numbers.

- `make bench`: measure collector ingest throughput and per-event cost
  (no-root, reproducible)
- document sensor-side methodology and measured numbers in
  `docs/performance.md`
- surface attribution quality (`package` vs `<app>` vs `<unknown>` rates) as
  the product KPI in docs and the metrics reference

**DoD:** `docs/performance.md` exists with real measured numbers and the exact
commands to reproduce them.

## Explicitly deferred (do not build yet)

Python/PyPI support (until a pilot demands it), blocking/enforcement,
multi-cluster fingerprint sharing, Redis, HA collector, Tier-2 (flagless)
attribution research. Each is real; none closes the first deal.
