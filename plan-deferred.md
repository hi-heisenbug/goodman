# Goodman: Deferred Work Plan (v0.2 → v0.3)

> **Audience:** the maintainer and any coding agent continuing this work.
> **Context:** `plan-pilot.md` Phases 1–9 are DONE and on `main`. This plan
> converts its "Explicitly deferred" list into an executable, phased sequence.
> Work the phases **in order**; each has a Definition of Done (DoD). Read
> `AGENTS.md` before touching anything — every invariant there applies here.

## Status (2026-07-13): Phases 0–3 DONE; Phase 2 build DONE; Phase 5a DONE

| Phase | Outcome |
|---|---|
| 0. Tier-2 research spike | **PARK (year-scale)** — `docs/research/tier2-attribution.md` |
| 1. Collector durability | PVC + sensor RAM spool + recovery test |
| 2. Python Tier-1 | **DONE** — shipped on `main`; `docs/attribution.md` + `docs/research/python-attribution-impl.md` |
| 3. enforce=warn | `action` on rules + `would_block` + Coverage/digest |
| 5a. Transactional fingerprint merge | **DONE** — `store.MergeFingerprint` + concurrency test; full HA still **PARK** — `docs/research/collector-ha.md` |
| 6 scaffold | **DONE** — `block` rule rejection, doctor LSM checks; kernel enforcement **PARK** — `docs/research/lsm-enforcement.md` |

Next ungated product work: Phases 4–6 (full builds) stay trigger-gated (`plan-deferred.md`).

## Sequencing rationale

Three forces order these phases:

1. **Risk to the first paid pilot.** A collector restart today loses in-flight
   sensor batches (`sendBatches` discards a batch on POST failure) and — with
   the default SQLite-on-emptyDir Helm config — the entire baseline database.
   That is the single most likely way a live pilot silently loses 30 days of
   learning. It gets fixed first among the build phases.
2. **Cheap precursors before expensive builds.** Sensor buffering + a PV is a
   week; true HA is a quarter. enforce=warn is a config field; eBPF LSM is a
   kernel-side project. Python Tier-1 reuses the existing perf-map machinery
   almost wholesale (CPython 3.12+ writes the *same* `/tmp/perf-<pid>.map`
   format V8 does). In every pair, the cheap half ships first and the
   expensive half stays trigger-gated.
3. **Safety sequencing for enforcement.** A false positive in detect mode is
   an annoying alert; in block mode it is a customer outage. Block mode
   (Phase 6) is gated on 60–90 days of enforce=warn evidence (Phase 3) across
   2–3 real environments. Starting the warn phase early starts that evidence
   clock early — that is why it is mid-plan and not last.

**Redis and hosted SaaS are deliberately not phases.** Redis may appear only
inside Phase 5 if measurement demands it. SaaS stays in "What NOT to build
yet."

### Phase order at a glance

| Phase | What | Size | Gate |
|---|---|---|---|
| 0 | Tier-2 (flagless) attribution research spike | 1 week, timeboxed | none — sanctioned by plan-pilot |
| 1 | Collector durability: PV + sensor spool + fast recovery | ~1 week | none — pilot risk |
| 2 | Python/PyPI Tier-1 (perf trampoline) | 3-day spike + ~1–2 weeks build | build half needs a named customer |
| 3 | enforce=warn (audit mode) | ~1 week | none — starts the safety evidence clock |
| 4 | Multi-cluster fingerprint sharing (per-customer) | ~1 week | customer with 3+ clusters |
| 5 | True HA collector (Postgres, N replicas; Redis only if measured) | 2–4 weeks | annual-contract security review demands it |
| 6 | eBPF LSM enforcement (block mode) | 3–4 weeks | Phase 3 evidence + volunteer staging namespace |

---

## Phase 0: Tier-2 (flagless) attribution research spike

**Why this is Phase 0 and not the Python spike:** attribution is the moat
(`plan-pilot.md` "Strategic frame"); Python is expansion. plan-pilot already
names Tier-2 as the one exception to the build-on-trigger rule ("run a
timeboxed one-week research spike right after the first pilot signs").
The spike produces a *decision document*, not product code, so it cannot
destabilize the pilot while GTM runs. Python, by contrast, has a named-customer
trigger that has not fired — and its cheap Tier-1 half is Phase 2 anyway.

**Timebox: 5 working days. Hard stop.** The output is a written answer to one
question: *is Tier-2 a quarter of work or a year?*

**Goal.** De-risk in-kernel V8 stack resolution (`JSFunction →
SharedFunctionInfo → ScopeInfo → source path`) enough to estimate it honestly.
No production code merges from this phase.

Scope of investigation (in order; stop when the timebox ends):

1. **User-space prototype first.** Before any eBPF, prove the pointer chase
   works at all: from outside the process, read a live Node pid's memory
   (`/proc/<pid>/mem`) and walk one on-stack `JSFunction` to its script name.
   If this is not doable in user space with a known V8 version, in-kernel is
   dead on arrival.
2. **V8 layout drift.** Enumerate the offsets needed and how they moved across
   the Node versions pilots actually run (18/20/22 LTS). Does V8 expose them
   (`v8_debug` metadata, postmortem data like `v8dbg_` constants used by
   llnode)? A per-version offset table shipped with the sensor may be
   acceptable; hand-maintained reverse engineering per release is not.
3. **eBPF feasibility.** Sketch the verifier budget: bounded pointer chases
   with `bpf_probe_read_user`, string extraction limits, GC-race tolerance
   (a torn read must degrade to `<unknown>`, never a wrong name — the
   never-misattribute invariant applies in-kernel too).
4. **Fallback interplay.** Confirm the design keeps perf-map Tier 1 as the
   permanent fallback and that Tier 2 can ship dark (flag-gated, default off).

**Decision criteria (write the answer down):**

- *GO (quarter-scale)* if: the user-space prototype resolves a source path on
  ≥2 Node LTS versions, offsets are obtainable from V8 metadata rather than
  reverse engineering, and the eBPF sketch fits verifier limits.
- *NO-GO / PARK (year-scale)* if: offsets require per-release manual work, or
  string/GC handling cannot guarantee never-misattribute. Parking is a valid
  outcome — the NODE_OPTIONS webhook already neutralizes most of the
  objection; record what would change the answer.

**File touch list**

- `docs/research/tier2-attribution.md` (new) — findings, offset tables,
  prototype notes, the GO/NO-GO decision and estimate.
- `hack/tier2-spike/` (new, gitignored or clearly marked non-product) — the
  throwaway user-space prototype. Not wired into the build.
- `docs/attribution.md` — one paragraph linking to the research doc.

**Verification**

```bash
make vet && make test && make smoke   # must stay green (no product code changed)
```

**Anti-patterns (from AGENTS.md)**

- Do not merge spike code into `internal/attribute/` or `bpf/`.
- Do not weaken never-misattribute to make the prototype "work".
- Do not let the spike run past the timebox because it is interesting.

**DoD:** `docs/research/tier2-attribution.md` exists with a GO/NO-GO decision,
an effort estimate with reasoning, and the exact prototype commands to
reproduce the findings. `make vet && make test && make smoke` untouched-green.

---

## Phase 1: Collector durability (the cheap 90% of HA)

**Goal.** A collector restart (OOM, node drain, upgrade) loses **zero
baselines** and **near-zero events**, without adding any new infrastructure a
pilot install could trip over. This is the "cheaper steps first" line from
plan-pilot, made concrete.

Three sub-steps, each independently shippable:

**1a. Persistent volume for the embedded SQLite store.** Today
`store.sqlitePath: /data/goodman.db` rides an emptyDir; a pod reschedule wipes
every baseline and alert. Add `store.persistence.{enabled,size,storageClass,
existingClaim}` to the Helm chart; mount a PVC at `/data` when enabled and
when `store.dsn` is empty. Document that Postgres remains the production
answer and the PVC is the pilot-grade default.

**1b. Sensor-side event buffering across collector restarts.** In
`cmd/sensor/main.go` `sendBatches`, a failed POST currently logs and drops the
batch. Change to: keep failed batches in a bounded in-memory spool (cap by
event count, e.g. 50k, configurable via `-spool-events` /
`GOODMAN_SPOOL_EVENTS`), retry with backoff on the next tick, evict oldest
first when full, and count evictions in a new
`goodman_sensor_spool_dropped_total` counter (plus a spool-depth gauge).
**The ring-buffer reader path does not change** — the buffered channel and
its drop counter stay exactly as they are; the spool lives entirely on the
batching side. No disk spool in this phase: a collector restart is seconds,
and 50k events of RAM covers minutes.

**1c. Fast recovery, verified.** Prove it: a test that starts a collector,
streams synthetic events through a real sensor batching loop (extract
`sendBatches` into a testable package or test it via `httptest` with a server
that goes down and comes back), kills the collector for 30s, restarts it, and
asserts every event arrived exactly once collector-side is not required —
at-least-once is fine because fingerprint aggregation is a set union and
ingest is idempotent at the behavior level. Assert: no baseline loss, spool
drains, drop counters stay zero.

**File touch list**

- `cmd/sensor/main.go` — spool + retry in `sendBatches`; new flag + env var;
  new metrics.
- `deploy/helm/goodman/templates/deployment.yaml`, new `pvc.yaml`,
  `deploy/helm/goodman/values.yaml` — persistence block.
- `docs/configuration.md` — new sensor flag/env + Helm values (all four
  surfaces: flag, docs, Helm values/templates; not secret-shaped, so no
  Secret change).
- `docs/deployment.md`, `docs/troubleshooting.md` — restart/recovery story.
- `CHANGELOG.md`.
- Tests: sensor batching test (new file, e.g. `cmd/sensor/spool_test.go` or a
  small `internal/spool` package with its own test), `make helm-lint`.

**Verification**

```bash
make vet && make test && make smoke
make helm-lint
```

No `bpf/` or `internal/loader/` change → no e2e needed; say so in the summary.

**Anti-patterns**

- Do not block the ring-buffer reader; the spool must sit behind the existing
  channel, never in front of it.
- Do not add a collector env var / Helm value without updating flag + docs +
  Helm together.
- Do not reach for Redis or a disk queue here — bounded RAM + PVC is the
  entire point of this phase.

**DoD:** kill-and-restart of the collector under load loses no baselines
(PVC) and no events within the spool budget (test proves it); spool metrics
visible; Helm persistence documented and lintable; `make vet && make test &&
make smoke` green.

---

## Phase 2: Python/PyPI Tier-1 attribution (perf trampoline) — **build DONE**

**Gate (historical):** a signed pilot with a meaningful Python footprint or 30%+ of
qualified pipeline stalling on "we're mostly Python." Do the 3-day spike the
moment the trigger looks likely; the build half only for a named customer.

**Why this is cheap (and why it's a spike, not a port).** The kernel side is
already done: `python`/`python3` are in `WatchedComms`
(`internal/loader/loader.go`), so syscalls + stacks are already captured.
CPython 3.12+ with `PYTHONPERFSUPPORT=1` writes `/tmp/perf-<pid>.map` in the
*same* `<addr> <size> <symbol>` format V8 uses — `PerfMap`, `PerfMapPath`,
`NSPID`, and the `/proc/<pid>/root` namespace handling in
`internal/attribute/perfmap.go` are reused as-is. What differs is everything
after the symbol lookup:

- Symbol shape: `py::<qualname>:<file>` vs V8's `LazyCompile:* <file>:<line>`.
  `sourcePathOf` (`resolve.go`) matches only `\.[cm]?js` paths today.
- Package boundary: `site-packages/<pkg>/` (or `dist-packages/`) instead of
  `/node_modules/<pkg>/`.
- Version source: `<pkg>-<version>.dist-info/` directory name (or its
  `METADATA` file) instead of `package.json`.
- Flag injection: `PYTHONPERFSUPPORT=1` env var instead of NODE_OPTIONS flags.

**Spike (3 days, timeboxed):** run a real CPython 3.12 workload with
`PYTHONPERFSUPPORT=1`, capture its perf map, and answer: does the trampoline
emit entries for the frames we care about (it covers *pure-Python* frames;
C-extension frames resolve via `/proc/<pid>/maps` like native addons do
today)? What do symbols look like for site-packages code in a container? What
fraction of a Django/Flask request path attributes correctly? Write findings
into `docs/research/python-attribution.md` with a build/no-build call.

**Build (on GO + named customer):**

1. `internal/attribute/resolve.go` — extend `sourcePathOf` with a Python
   symbol regex; keep the JS regex untouched and first.
2. `internal/attribute/package.go` — add `PathToPyPackage` (site-packages →
   `(pkg, version)` via dist-info), with the same "last segment wins" and
   cache discipline; sentinel returns on any ambiguity.
3. `internal/attribute/canonical.go` — collapse `site-packages` paths the way
   `node_modules` paths collapse today; sensitive paths stay verbatim.
4. `internal/attribute/attribute_test.go` — extend the simulated `/proc` tree
   with a Python pid fixture (perf map with `py::` symbols, site-packages +
   dist-info layout). This is the primary verification; no kernel needed.
5. `internal/admission/admission.go` — inject `PYTHONPERFSUPPORT=1` alongside
   NODE_OPTIONS (same idempotency + valueFrom-untouched rules; extend
   `admission_test.go`).
6. Docs: `docs/attribution.md` (Python section), `docs/configuration.md`,
   `docs/getting-started.md`, `CHANGELOG.md`.

**Verification**

```bash
make vet && make test && make smoke
```

No `bpf/` change (kernel side already captures Python) → no e2e required for
the attribution work itself, but note in the summary that a human should run
a live Python workload against `sudo make e2e`-style verification before a
Python-footprint customer installs.

**Anti-patterns**

- Never misattribute: a frame that parses oddly returns `<app>`/`<unknown>`,
  not a guessed package. Do not loosen the JS regex while adding the Python
  one.
- Do not fork the perf-map reader into a Python copy — one `PerfMap`, two
  symbol parsers.
- Do not build this speculatively without the named customer; the spike alone
  answers the sales objection ("yes, Q4, here's the research doc").

**DoD (build half):** the simulated-`/proc` test attributes a Python
site-packages frame to the right `pkg@version`; webhook injects
`PYTHONPERFSUPPORT=1` idempotently; docs updated; all no-root gates green.

---

## Phase 3: enforce=warn (audit mode) — start the safety evidence clock

**Goal.** The first, deliberately toothless step toward enforcement: rules
gain an `action` field; `action: "warn"` marks an alert as *would have been
blocked* — a visible chip, a counter, a log line — and changes nothing else.
Running warn mode for 60–90 days across 2–3 real environments *is* the
evidence that Phase 6 (real blocking) demands. That is why this phase ships
long before anyone asks for blocking.

Design (rules over `if`s, per AGENTS.md):

- `internal/diff/diff.go` `Rule` gains `Action string` (`"alert"` default,
  `"warn"` = would-block audit). Unknown values fail rule loading loudly.
- `model.Alert` gains a `WouldBlock bool` (or `EnforceAction string`) field;
  set when any matched rule has `action: warn`. Store migration adds the
  column — **both** `NNN_enforce_action.postgres.sql` and `.sqlite.sql`.
- New Prometheus counter on the collector:
  `goodman_enforce_would_block_total{rule}`.
- Dashboard: a "would block" chip on the alert row + a count on the Coverage
  tab next to the alert-budget KPI (that pairing is the Phase 6 evidence
  view: would-blocks vs false-positive rate over time).
- `docs/api.md` alert shape, `docs/configuration.md` rule schema,
  `deploy/rules.example.json` gains one commented warn example.
- The weekly digest (`internal/digest`) includes the would-block count — the
  champion sees the enforcement story building week over week.

**File touch list**

- `internal/diff/diff.go`, `internal/diff/diff_test.go`
- `internal/model/` alert type (NOT `RawEvent` — the wire struct is untouched)
- `internal/store/store.go`, `internal/store/migrations/005_enforce_action.postgres.sql` + `.sqlite.sql`
- `internal/api/api.go` (response shape), `docs/api.md`
- `internal/digest/`
- `dashboard/src/types.ts`, `dashboard/src/App.tsx` (+ `make dashboard`,
  commit `internal/api/ui/dist/`)
- `deploy/rules.example.json`, `docs/configuration.md`, `CHANGELOG.md`

**Verification**

```bash
make vet && make test && make smoke && make replay
make dashboard && (cd dashboard && npm audit --audit-level=moderate) && make build
```

Dashboard touched → verify visually at desktop and mobile widths against live
`/v1/*` data (e.g. `make demo`), not a screenshot of mock data.

**Anti-patterns**

- No blocking of any kind in this phase — no kernel change, no process kill,
  no LSM. `bpf/` stays untouched.
- No hard-coded warn conditionals; it is a rule field operators tune.
- Both SQL dialects or the migration doesn't merge.
- Don't forget `make dashboard` + committing `dist/`; stale hashed assets out,
  new ones in.

**DoD:** a replay scenario with a warn-action rule produces an alert flagged
would-block; the chip renders; the counter increments; digest mentions it;
both dialects migrate cleanly; all gates green.

---

## Phase 4: Multi-cluster fingerprint sharing (per-customer) — DONE

**Gate:** first customer with 3+ clusters complains about re-learning
baselines per cluster. Per-customer sharing only — cross-customer community
intelligence stays out (see "What NOT to build yet").

**Goal.** Baselines learned in cluster A can seed cluster B: export
promoted baselines from one collector, import them into another, with
provenance so an imported baseline is distinguishable from a locally learned
one (trust and debugging both need this).

Design — keep it boring, file-shaped, and operator-driven:

- `GET /v1/fingerprints/export` (operator-token class): streams promoted
  baselines as JSON.
- `POST /v1/fingerprints/import` (operator-token class): upserts baselines
  that don't exist locally; never overwrites a locally learned baseline;
  marks rows `origin = "imported"` (migration, both dialects).
- `goodmanctl fingerprints export > baselines.json` /
  `goodmanctl fingerprints import baselines.json` — the multi-cluster story
  is two CLI commands and a file the customer moves themselves. No
  collector-to-collector networking, no shared database, no sync daemon.
- Diff engine treats imported baselines exactly like promoted ones (drift
  against them alerts normally).
- Dashboard: small "imported" origin tag on fingerprint rows.

**DoD:** export from collector A, import into fresh collector B, replay a
drift scenario against B → alert fires without a learning window; imported
rows carry provenance in API and UI; both dialects migrate; all gates green.

---

## Phase 5: True HA collector (trigger-gated)

> **Sub-step 5a (transactional fingerprint merge) is DONE** — see
> `docs/research/collector-ha.md`. Full HA below remains **PARK** until the
> annual-contract security-review trigger fires.

**Gate:** a production (annual) contract's security review demands no single
point of failure. Do not start this on a calendar. Phase 1 already bought the
pilot-grade story ("restarts lose nothing, sensors buffer and retry, recovery
is seconds") — lead with that in security reviews first.

**Goal.** `collector.replicas: N` behind the existing Service, Postgres
required (SQLite is single-writer by design and stays the single-replica
path), and correct concurrent ingest.

Design direction (validate when the trigger fires):

- **Postgres-only for N>1.** Fail fast at startup if `replicas > 1` is
  implied and the DSN is SQLite.
- **Idempotent, transactional ingest.** Fingerprint aggregation is a set
  union keyed on `(service, package, version)`; move the read-modify-write in
  `internal/fingerprint` + `internal/store` into an atomic upsert
  (`ON CONFLICT ... DO UPDATE` with JSONB merge on Postgres) so two replicas
  ingesting the same package concurrently converge. Alert dedup already keys
  on a deterministic `alertID`; make emission `ON CONFLICT DO NOTHING`.
- **Singleton background work** (digest, reachability refresh, retention):
  Postgres advisory-lock leader election — no new dependency.
- **SSE** streams per-replica: acceptable (each replica tails the store) or
  documented limitation in round one.
- **Redis enters here only if** measured ingest on a real cluster exceeds
  what batched Postgres upserts sustain (`make bench` numbers first, then a
  cluster measurement). It would be a write-through cache for hot
  fingerprints — a dependency of this phase, never a milestone. Default
  assumption: not needed.

**File touch list (expected)**

- `internal/store/store.go` (+ possible Postgres-specific upsert path — keep
  the SQLite codepath working and tested)
- `internal/fingerprint/fingerprint.go`
- `internal/api/api.go` (readyz semantics under multi-replica)
- `cmd/collector/main.go` (leader election for background loops)
- `deploy/helm/goodman/values.yaml` + `templates/deployment.yaml` (PDB,
  anti-affinity, replicas guard)
- `docs/deployment.md` HA section, `docs/architecture.md`, `CHANGELOG.md`

**Verification**

```bash
make vet && make test && make smoke && make replay && make bench
make helm-lint
```

Plus a two-replica concurrent-ingest test against real Postgres (dockerized in
CI or documented as a human step).

**Anti-patterns**

- Do not break the one-codepath store: SQLite remains fully supported for
  single-replica; Postgres-only SQL must be explicitly separated and tested.
- Do not add Redis "while we're here" — measurement first.
- Do not let two replicas double-send digests/webhooks (leader-elect all
  side-effecting loops).

**DoD:** 2 replicas ingest a replay corpus concurrently with identical final
fingerprints and exactly one alert per scenario; killing either replica
mid-ingest loses nothing; single-replica SQLite path still green end to end.

---

## Phase 6: eBPF LSM enforcement (block mode)

> **Scaffold is DONE** — `action: "block"` fails rule loading with an explicit
> message, `make doctor` reports LSM capability, posture documented in
> `docs/configuration.md`. See `docs/research/lsm-enforcement.md`. Kernel
> work below remains **PARK** until all three gates fire.

**Gate — all three, no exceptions:**
1. 60–90 days of Phase 3 warn-mode evidence across 2–3 real environments with
   near-zero false positives (the Coverage-tab would-block vs alert-budget
   view is the artifact).
2. A customer volunteering a **staging** namespace.
3. Maintainer sign-off on the kill-switch design below.

**Goal.** For rules with `action: "block"`, deny the syscall in-kernel via
eBPF LSM hooks (`lsm/file_open`, `lsm/socket_connect`, `lsm/bprm_check` for
exec) — but only for pids/namespaces explicitly opted in, with an
always-available kill switch.

Safety design (the product is the sequencing, not the hook):

- **Opt-in scope map:** a new BPF map of enforced cgroups/pids, populated
  only for namespaces labeled `goodman.io/enforce=enabled`. Everything else
  is observe-only forever.
- **Kill switch:** a single BPF map flag the sensor can flip instantly
  (`goodmanctl enforce off`), plus fail-open on any sensor/collector outage —
  if user space is gone, the LSM hooks allow.
- **Decision path stays in-kernel simple:** the kernel consults a
  pre-computed deny map (behavior patterns compiled by user space), never
  does attribution in the hot path. User space decides *policy*; kernel
  enforces *verdicts*.
- Every deny raises a CRITICAL alert with the full evidence chain; a denied
  behavior that later proves benign must be un-blockable by rule exclude
  without redeploying.

**File touch list (expected)**

- `bpf/goodman.bpf.c`, `bpf/goodman.h` — LSM programs + maps. If `struct
  event` changes, `internal/model/types.go` changes **in the same commit**
  and `types_test.go` must pass unedited (the one rule).
- `internal/loader/loader.go` + `loader_test.go` (attach LSM programs; assert
  them in the no-root object test)
- `internal/diff/diff.go` (`action: "block"`), `internal/attribute/` (verdict
  compilation), `cmd/sensor/main.go`, `cmd/goodmanctl/` (enforce on/off)
- Helm: enforcement values default **off**; namespace label documented
- `docs/` new `enforcement.md` + configuration/deployment updates, `CHANGELOG.md`

**Verification**

```bash
make bpf && make build
make vet && make test && make smoke && make replay
sudo make e2e        # REQUIRED — bpf touched; human step on a real kernel
```

State explicitly in any summary if only the no-root path ran; kernel LSM
requires `CONFIG_BPF_LSM` + `lsm=bpf` boot param — add a `make doctor` check.

**Anti-patterns**

- Never ship block before the Phase 3 evidence window completes — this gate
  is the whole phase.
- Never edit `types_test.go` to make a layout change pass.
- Fail open on every ambiguity: unknown pid, torn map read, sensor restart →
  allow + alert, never deny.
- No blocking work in the ring-buffer reader path.

**DoD:** in a lab cluster, a replayed attack behavior in an enforce-labeled
namespace is denied and alerted; the same behavior in an unlabeled namespace
is alerted only; `goodmanctl enforce off` takes effect in <1s; `sudo make
e2e` green on a real kernel; `make doctor` reports LSM capability.

---

## What NOT to build yet

- **Redis as its own milestone.** Only as a measured-need dependency inside
  Phase 5. Every extra Helm dependency is one more way a pilot install fails.
- **Hosted SaaS dashboard.** Not before 2027, on repeated pull from *paying*
  customers. The embedded dashboard + `-public-url` deep links cover the
  pilot. SaaS drags in multi-tenancy, billing, and a trust boundary the
  product does not need to win its first renewals.
- **Install-time (postinstall/CI) detection.** A different product surface
  (registry/CI integration, no eBPF). Revisit only with customer pull, as a
  separate plan.
- **Cross-customer community intelligence.** Needs an anonymization story and
  10+ customers before the data means anything. Phase 4 deliberately stops at
  per-customer file export/import.
- **Tier-2 production build.** Phase 0 produces a decision, not a build. The
  build is 2027 work and only on a GO decision plus the two-deals-stalled
  trigger.
- **Sensor disk spool / durable queue.** Phase 1's bounded RAM spool covers
  collector restarts; a disk queue is complexity chasing a failure mode
  (multi-hour collector outage) that a 30-day POV does not have.

## Success criteria for v0.3

v0.3 is the "pilot survives contact with production" release. Tag it when:

1. **Durability:** collector kill-and-restart under load loses zero baselines
   and zero events within the spool budget, proven by a repeatable test
   (Phase 1).
2. **Moat decision made:** `docs/research/tier2-attribution.md` contains a
   GO/NO-GO with an effort estimate the maintainer believes (Phase 0).
3. **Enforcement clock running:** at least one real environment has warn-mode
   rules live, with would-block counts visible in the dashboard and weekly
   digest (Phase 3).
4. **Python answered:** the Python spike doc exists; if the named-customer
   trigger fired, simulated-`/proc` tests attribute `site-packages` frames to
   the right `pkg@version` (Phase 2).
5. **All no-root gates green on `main`:** `make vet && make test && make
   smoke && make replay && make demo-check && make helm-lint`; dashboard
   rebuilt and committed if touched; `sudo make e2e` run by a human if
   `bpf/`/`internal/loader/` changed (not expected before Phase 6).
6. **Docs tell the story:** configuration/deployment/api docs updated for
   every shipped phase; CHANGELOG entries per phase; the pilot runbook
   mentions the new durability + warn-mode posture.

Phases 4–6 are explicitly *not* required for v0.3 — they ship on their
triggers, and their sections above are ready when those triggers fire.
