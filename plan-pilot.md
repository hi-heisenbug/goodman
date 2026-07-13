# Goodman: Path to First Paid Pilot (Sep 30, 2026)

> **Audience:** the maintainer and any coding agent continuing this work.
> **Context:** `plan.md` was the zero-to-v0.1 build plan and is complete. This
> plan covers the next 11 weeks: everything here exists to convert a design
> partner into the first paid pilot. Work the phases **in order**; each has a
> Definition of Done (DoD).

## Status (2026-07-13): Phases 1–7 DONE; next is Phase 8

Phases 1–6 (detection quality) and Phase 7 (five-minute wow) are built, tested,
and on `main`. Latest local tip before this phase: `6af6c5d`.

| Phase | Shipped as |
|---|---|
| 1. Attack replay corpus | `make replay`, 4/4 scenarios pass |
| 2. Always-on rules + alert evidence | diff engine + store migrations + dashboard rule chips |
| 3. Runtime reachability report | `goodmanctl report` + OSV enrichment |
| 4. NODE_OPTIONS admission webhook | `webhook.enabled` in Helm |
| 5. Noise controls | per-rule excludes + sensor CIDR aggregation |
| 6. Perf benchmark + attribution KPI | `make bench` + `docs/performance.md` |
| 7. Five-minute wow | `make demo` / `goodmanctl demo` / `make demo-check` |

Shipped beyond the plan this cycle:

- Enterprise hardening layer: API auth, TLS, alert webhooks, `readyz`,
  retention, Helm and docs updates, dashboard TokenGate.
- Full correctness review with five fixes.
- Dashboard Reachability tab: `POST /v1/report` endpoint, lockfile upload,
  OSV toggle, reachability-sorted vulnerability table.
- Persist and auto-refresh reachability reports.

### Remaining to hit the goal

1. **Release gate** (see section below): push, tag `v0.2.0`, `sudo make e2e`
   on a real kernel.
2. **Phases 8–9 below**: pilot heartbeat + coverage/trust panel.
3. **GTM execution.** Outreach, design-partner conversations, and the pilot
   agreement, tracked in the separate `gtm/` repo.

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

## Phase 1: Attack replay corpus (DONE)

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

## Phase 2: Always-on high-risk rules + alert evidence (DONE)

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

## Phase 3: Runtime reachability report (DONE)

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

## Phase 4: NODE_OPTIONS admission webhook (DONE)

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

## Phase 5: Noise controls (DONE)

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

## Phase 6: Performance benchmark + attribution quality (DONE)

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

---

Phases 1-6 solved detection quality and proof. Phases 7-9 solve the pilot's
lifecycle: the first five minutes (demo friction), the thirty quiet days in
the middle (no heartbeat), and the trust question that decides renewal ("was
it even watching everything?"). Work them in order; each is sized for days,
not weeks, because GTM runs in parallel.

## Phase 7: Five-minute wow (demo mode) (DONE)

A solo founder has no sales engineer. Every discovery call and every
"try it yourself" link must land on a dashboard that already shows the
product working, not an empty state. Shipped as `make demo` /
`goodmanctl demo` / `make demo-check`:

- starts a local collector + dashboard with seeded fingerprints and alerts,
  no root and no cluster required (`internal/demo`)
- replays the event-stream attack scenario live (~12s after ready) so a
  CRITICAL `flatmap-stream` alert with `secret-read` + `new-outbound-connect`
  chips appears while the viewer watches
- preloads a synthetic lockfile so the Reachability tab shows
  **1,400 declared / 240 executed** on first load (no upload)
- prints the dashboard URL and a 60-second guided script to the terminal
- README quickstart is `make doctor && make build && make demo`

**DoD:** `make demo-check` asserts the reachability headline and the live
CRITICAL alert; unit tests keep the event-stream fixture locked to the replay
corpus; getting-started documents the guided path.

## Phase 8: Pilot heartbeat (scheduled reachability + weekly digest)

The strategic frame above names the risk: a silent 30-day POV reads as "it
does nothing." Make the product speak on a schedule from day one:

- collector computes and persists a reachability snapshot per service on a
  schedule (builds on the persisted self-refreshing reachability already
  shipped), so the dashboard loads current numbers with week-over-week
  deltas and no manual lockfile upload
- weekly digest generated by the collector and delivered via the existing
  webhook layer (Slack-formatted) and as markdown: new packages executed,
  new reachable CVEs, alert count vs the noise budget, coverage stats
- Slack alert formatting with evidence: rule name, sensor, first-seen times,
  deep link to the alert in the dashboard

**DoD:** a fresh install emits its first digest with zero manual steps; the
Reachability tab shows a trend, not just a point-in-time table; a CRITICAL
alert in Slack contains enough evidence to triage without opening the
dashboard.

## Phase 9: Coverage and trust panel

Pilots die on silent gaps discovered late ("the service that mattered was
never instrumented"). Give the champion a page that answers "is Goodman
watching my workloads and can I trust its numbers" at a glance:

- per-node sensor health (running, event rate, last seen)
- per-namespace injection coverage: pods with and without NODE_OPTIONS, with
  the webhook label state, so gaps are visible and fixable
- live attribution KPI: `package` vs `<app>` vs `<unknown>` rates, plus the
  top unattributed processes so `<unknown>` is actionable
- alert budget: alerts per day against a stated target (default < 5), the
  number a security lead uses to justify keeping the DaemonSet

**DoD:** the dashboard has a Coverage tab; a deliberately uninstrumented
namespace shows up as a gap; the attribution KPI matches the numbers in
`docs/performance.md`; screenshots of this tab go into the pilot deck.

## Release gate (before the first pilot install)

- `git push` all local commits; image build goes green
- tag `v0.2.0`; `helm install` from the tagged chart works on a fresh cluster
- `sudo make e2e` on a real kernel (human step; eBPF/loader untouched this
  cycle but the live path must be confirmed before a customer runs it)
- `helm upgrade` from the previous release applies store migrations cleanly
- pilot runbook in `docs/`: install, tune noise in week one, weekly digest
  expectations, and the 30-day POV success criteria the champion signs up to

## Explicitly deferred (build on trigger, not on calendar)

Each item below is real and none closes the first deal. Pre-revenue, every
one has a cheaper substitute (a doc, a config, a manual step). Build each
the day its trigger fires and not before.

- **Python/PyPI support.** Trigger: a signed pilot has a meaningful Python
  footprint, or 30%+ of qualified pipeline stalls on "we're mostly Python."
  Likely Q4 2026 as the first post-pilot expansion. Note: CPython 3.12+ uses
  the perf trampoline (`PYTHONPERFSUPPORT=1`), not V8 perf-map flags, so
  this is a spike, not a port. Build for a named customer only.
- **Tier-2 (flagless) attribution.** Trigger: two or more deals stall
  specifically on the NODE_OPTIONS objection (cannot restart pods, perf-flag
  overhead fears, non-K8s). Exception to the trigger rule: this is the moat
  deepener, so run a timeboxed one-week research spike right after the first
  pilot signs to learn whether it is a quarter of work or a year. Real build
  2027.
- **HA collector.** Trigger: a production (annual) contract's security
  review demands no single point of failure. Likely Q4 2026 to Q1 2027.
  Cheaper steps first: sensor-side event buffering across collector
  restarts, persistent volume, fast recovery. A 30-day POV never needs true
  HA.
- **Redis.** Never a milestone of its own. Arrives only as a dependency of
  the HA collector, or when a real cluster's measured ingest rate exceeds
  the current store. Adopting it early is one more thing in the Helm chart
  that can fail a pilot install.
- **Multi-cluster fingerprint sharing.** Trigger: first customer with 3+
  clusters complains about re-learning baselines per cluster. Likely Q1 2027
  in its per-customer form. Cross-customer community intelligence is the
  long-term network-effect play and needs an anonymization story plus 10+
  customers before the data means anything.
- **Blocking/enforcement.** Trigger: 60-90 days of near-zero false positives
  across 2-3 real environments (the Phase 9 alert-budget KPI is the
  evidence), plus a customer volunteering a staging namespace. Q2 2027 at
  the earliest. Sequencing here is a safety property: a false positive in
  detect mode is an annoying alert; in block mode it is a customer outage.
  First step is enforce=warn (audit mode), then eBPF LSM denial.
- **Hosted SaaS dashboard, install-time (postinstall/CI) detection.**
  Trigger: repeated pull from paying customers. Not before 2027.
