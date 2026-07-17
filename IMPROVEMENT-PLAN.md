# Goodman Improvement Plan (2026-07-14)

Consolidated output of a six-track audit: build/test verification, Go backend
review, eBPF layer review, dashboard review, live demo audit, and market/web
research. **No code was changed** — this file is the plan. Every finding below
was verified against the actual code or by running the system.

## TL;DR

- **Health**: `go build/vet/test`, dashboard `tsc`+vite build, helm lint, and
  the backend+enforcement smoke test all pass. Only 5 files fail `gofmt`
  (whitespace).
- **The demo works today.** `make demo` passes every step; all four dashboard
  tabs render real data (verified with browser screenshots). See "Demo
  recording" section for the exact commands and the 7 polish gaps.
- **Two critical eBPF findings**: LSM block mode for file opens is almost
  certainly a **silent no-op** (`bpf_d_path` key-residue bug), and
  `emit_deny`'s `bpf_get_stack(ctx,…)` misuse should fail the verifier in
  enforce mode. Detection mode (the pilot default) is unaffected.
- **Biggest detection gap**: forked children are never traced — `node`
  spawning `curl` via `child_process` (the classic exfil pattern) produces no
  EXEC event.
- **Backend**: 23 verified findings; worst are SSE streams force-closed every
  60s, enforcement state broken under HA, and scheduled OSV failures being
  persisted as "all vulnerabilities fixed."
- **Dashboard**: reachability auto-load bug fully root-caused (service-scope
  exact match with no fallback); minimal fix identified.
- **Market research**: package-level syscall attribution + behavioral drift is
  genuinely novel; reachability alone is commoditized (Oligo, Kodem, Sysdig,
  Endor, Datadog all claim it). Flagship demo should replay Mini-Shai-Hulud
  (Apr–May 2026) behaviors.

---

## P0 — Fix before the next customer-facing milestone

These either break the product's core claims or would embarrass a live demo.

### P0.1 LSM `deny_open` is silently dead (CRITICAL, eBPF)
`bpf/goodman.bpf.c:342-347`. `bpf_d_path` renders the path at the end of the
buffer and memmoves it forward **without zeroing the tail**, so the hash-map
key contains residue; userspace inserts zero-padded keys
(`internal/loader/loader.go:333-337`). Exact-match lookup never hits →
file-open block mode does nothing, silently, with no metric.
**Fix**: render into a per-CPU scratch buffer and copy exactly `n` bytes into
a zeroed key (or zero the tail after `bpf_d_path`). Add an e2e test that
exercises a real file_open deny — current tests never verified one.
`deny_exec` and `deny_connect` are unaffected.

### P0.2 `bpf_get_stack` called with non-ctx pointer in LSM programs (CRITICAL)
`bpf/goodman.bpf.c:143` via callers at :348, :407/:411, :432 — passes
`file`/`sock`/`bprm` pointers as `ctx`. Expected verifier rejection; because
all programs load in one collection (`loader.go:90`), `-enforce-enabled`
would `log.Fatalf` the sensor — enforce mode kills detection instead of
degrading. **Fix**: drop the stack capture in `emit_deny` or use
`bpf_get_task_stack(current)`.

### P0.3 LSM failure handling detaches/kills detection (HIGH)
- `loader.go:141-151`: a partial LSM attach failure closes **all** links,
  including the five detection tracepoints → sensor runs with zero event
  sources. Fix: roll back only LSM links.
- `loader.go:84-97`: LSM programs load **before** `lsmSupportReason()` runs,
  so `-enforce-enabled` on a kernel without BPF LSM fatals instead of
  degrading to detection-only. Fix: load LSM programs in a second collection
  after the probe passes.

These three together mean the README's "fail-open, off by default" claim only
holds because block mode is off by default. Fix before anyone flips it on.

### P0.4 Fork-follow gap: children of watched processes are invisible (HIGH)
`watched_pids` is populated only by a 3-second `/proc` comm scan
(`loader.go:417-469`). No `sched_process_fork` hook → when node/python
spawns a child (`child_process.exec("curl …")`), the child's tgid is never
watched and `trace_execve` drops it. This is the **primary supply-chain exfil
pattern** and the exact behavior the 2026 worm wave exhibits (see research).
Short-lived processes (<3s) are also never seen.
**Fix**: add `tracepoint/sched/sched_process_fork` propagating the watched
bit to children + `sched_process_exit` for cleanup; demote the comm scan to
bootstrap-only. Verify the attack-replay corpus still passes and add a
fork-exec scenario to it.

### P0.5 SSE streams force-closed every 60 seconds (HIGH, backend)
`internal/api/api.go:92` applies `middleware.Timeout(60s)` to the whole
router including `/v1/stream` (:583). Every SSE stream dies at exactly 60s:
`goodmanctl tail` exits silently (no reconnect loop,
`cmd/goodmanctl/main.go:140-177`); the dashboard reconnects with a gap and
the "Live" indicator can flicker during a >60s recording take.
**Fix**: route-group `/v1/stream` outside the timeout middleware; also set
`ReadHeaderTimeout` on the `http.Server` (currently none — slowloris).

### P0.6 Scheduled OSV failure persists a false "all fixed" report (HIGH)
`internal/report/refresh.go:43-47` swallows OSV errors but still saves the
snapshot with `osv: true` → next delta reports every previously reachable
vuln as gone; weekly digest broadcasts a false all-clear.
**Fix**: persist `osv=false`/a `degraded` flag on OSV failure. Also
`refresh.go:34-36`: one corrupt lockfile aborts refresh for all later
services — continue per-service and surface per-service errors.

### P0.7 Reachability dashboard auto-load: scope mismatch + no-vuln rendering
Root cause of the known bug, now fully understood:
- Snapshots are keyed by exact `service` string; the dashboard only ever
  reads/writes scope `""` (`dashboard/src/App.tsx:656`, `api.ts:78-85`), and
  `handleGetReport` (`internal/api/api.go:516`) 404s with no fallback. A
  snapshot persisted with `?service=checkout` is invisible to the UI.
- Separately, when a snapshot has zero vulns (demo seeder never sets `osv=1`,
  `internal/demo/client.go:93`), there is **no all-packages table** — only
  the vuln table exists (App.tsx:772), so "the package table doesn't render"
  is a design gap, not a load failure.
- `fetchStoredReport().catch(() => {})` (App.tsx:664-666) swallows real
  errors into the innocent "No lockfile analyzed yet" empty state.

**Fix (minimal)**: in `handleGetReport`, when `service==""` and not found,
fall back to the first stored lockfile's service — reuse the `pickService`
pattern already implemented in `internal/digest/digest.go:107-114`. Surface
non-404 errors in the UI; optionally add a `rows`-backed all-packages table.

---

## P1 — Before the pilot install (correctness & security)

### Backend (from 23 verified findings)
1. **HA enforcement is broken** (`internal/enforce/state.go`): `enabled` read
   once at startup, never re-read; per-replica `rev` and `behaviors`
   divergence clobbers the shared `enforce_state` row; sensors polling
   different replicas see enforcement flap on/off and thrash kernel deny maps
   with different verdict sets. If HA + enforcement is out of pilot scope,
   document that; otherwise the state needs to be DB-sourced with a
   subscription/poll.
2. **Resolved alerts silently swallow re-drift** (`store.go:513-530`,
   `diff.go:264`): merging into a resolved alert never reopens it — dashboard
   shows nothing while the webhook fires. Reopen on merge.
3. **Migrations race / non-atomic** (`store.go:169-208`, migration 007 lacks
   `IF NOT EXISTS`): two Postgres replicas cold-starting can crash-loop;
   SQLite crash between ALTER and the migrations insert bricks boot. Wrap
   each migration in a transaction + Postgres advisory lock.
4. **`MergeFingerprint` first-write race** (`store.go:242-295`): `FOR UPDATE`
   locks nothing pre-insert; concurrent replicas can drop first-seen
   behaviors from the baseline (evidence loss).
5. **Ingest-path hazards**: `RecordBehavior` holds a mutex across a
   timeout-less DB write and its behaviors map grows unboundedly with per-IP
   connect strings (`enforce/state.go:54-65`); `ReactDenied` does a 500-row
   scan per denied event and misses alerts older than the newest 500
   (`diff.go:275-300`).
6. **`/v1/alerts` hard-caps at 500 with N+1 fingerprint queries**
   (`store.go:631`, `api.go:336-351`): alerts beyond 500 are unreachable
   forever. Add pagination + batched enrichment.
7. **Reachability "week-over-week" delta is actually interval-over-interval**
   (`store.go:135-147`): every refresh tick shifts `previous_*`, so with
   `-reachability-interval=1h` the digest's WoW numbers compare to one hour
   ago. Snapshot previous_* on a weekly cadence or store history.
8. **Security niggles**: `/metrics` unauthenticated even with APIToken set
   (`api.go:115`); raw `err.Error()` leaked in 500 bodies; `?token=` on
   `/v1/stream` lands the bearer token in proxy logs (issue a stream-scoped
   token); OSV `resolve` hardcodes `api.osv.dev` ignoring `c.Endpoint`
   (`osv.go:159-160`) — breaks air-gapped/proxied deploys.
9. **Config foot-guns**: env parsers silently fall back on typos
   (`collector/main.go:298-314`) — `GOODMAN_RETENTION=30days` silently
   disables retention. Warn loudly on parse failure. SQLite DSN with existing
   query params breaks (`store.go:40-43`).

Verified non-issues (don't re-chase): pgx multi-statement migrations work;
gzip ingest is bounded post-decompression; SSE broadcast and leader lock are
correct; spool/notifier queues bounded; resolver thread-context is
single-goroutine safe.

### eBPF (beyond P0)
- **Detection/enforcement path-string mismatch**: detection records the raw
  `openat` argument (relative, symlinked, TOCTOU-able); verdicts compiled
  from it are matched in-kernel against canonical `d_path` output — symlinks
  like `/var/run → /run` mean observe-then-block silently never blocks.
  Resolve dirfd/canonicalize on the detection side, or tag relative paths.
- **Deny verdicts are global across all enforced cgroups** — one package's
  bad behavior in service A blocks the same path/IP for every enforced pod on
  the node. Composite key `{cgroup_id, path}`.
- **PID/TID reuse** within the 3s scan window can misattribute;
  `threadContext` map never evicted (unbounded with thread churn).
- **Silent data loss counters**: undersized ringbuf records are dropped with
  no counter (`loader.go:478`); `perCPUMapSum` returns 0 on error so real
  kernel drops can read as zero. Make drops a counter and export discards.
- **arm64**: `sys_enter_open` doesn't exist; all five tracepoints are
  mandatory → sensor dies. Attach-what-exists + per-arch BPF objects.
- Perf: `binary.Read` reflection per event; `bpf_get_stack` on every open by
  a watched pid (no in-kernel dedup/sampling). Matters for the <2% CPU
  benchmark buyers expect (see research).

### Dashboard (beyond P0.7)
- **Dead-stream masking**: no view wires `subscribe`'s `onError`; the header
  pill always shows green "Monitoring" even when the EventSource is
  permanently closed (App.tsx:1183-1187). Wire onError → visible state.
- **Triple SSE connections + duplicated polling** (App, AlertsView,
  FingerprintsView each open EventSource + interval): one SSE frame triggers
  up to 4 simultaneous fetches; two tabs can exhaust the browser's 6-conn
  limit. Share one subscription at App level.
- **Fetch races**: no in-flight guard in `load()` — a stale response can
  overwrite fresher state (resolved alert visually reappears). Sequence
  counter or AbortController.
- **Rollback button emits an invalid kubectl command** (App.tsx:209):
  `kubectl set image deployment/svc lodash=lodash@4.17.20` is not a valid
  image ref — a customer pasting it live gets an error. Label as example or
  derive real container/image.
- Hash vs query-param convention mixed for `?static`; sidebar tabs don't
  update `location.hash` (deep links diverge); no loading spinner during the
  stored-report fetch; token-in-localStorage caveat worth a docs note.

### Build hygiene
- `gofmt -w` on: `cmd/sensor/main.go`, `internal/model/types.go`,
  `internal/loader/loader_test.go`, `internal/api/api_test.go`,
  `internal/store/store_test.go`. Add a `make fmt`/CI gofmt gate.
- `make clean` doesn't remove `goodman_demo.db*` at repo root (Makefile:122).
- Consider whether `demo_build/goodman_demo.mp4` + screenshots (build
  outputs, ~1 MB) should stay tracked.

---

## P2 — Positioning & product moves (from web research)

1. **The wedge is attribution + drift, not reachability.** Runtime
   reachability / "in-use packages" is now table stakes (Oligo, Kodem, Sysdig
   Risk Spotlight, Endor, Datadog all claim it). Nobody publicly attributes
   individual syscalls to the exact npm/PyPI package+version inside a shared
   process and baselines drift per version. Lead every pitch with the one
   screen no competitor has: *"this `connect()` to 169.254.169.254 came from
   `left-pad@1.3.0`, not your app code."*
2. **Flagship demo scenario: Mini-Shai-Hulud (Apr–May 2026).** Its behaviors
   map 1:1 to Goodman's hooks: reads `~/.aws/credentials`, `~/.ssh/id_*`,
   `~/.npmrc`, `.env*`, even `~/.claude/mcp.json` (open); hits IMDS
   `169.254.169.254` (connect); C2 to `zero.masscan[.]cloud` (connect);
   `preinstall` spawns node/Bun (execve — **requires the P0.4 fork fix to
   catch honestly**). Other hooks: TrapDoor (May 2026), node-ipc (May 2026),
   Microsoft `durabletask` PyPI (May 2026), Hades `.pth`-hook campaign (Jun
   2026, the future PyPI showcase). Marketing line: "every 2026 worm did the
   same four things at runtime; Goodman sees all four and names the package."
3. **Publish the overhead benchmark**: buyers' bar is <1–2% CPU (Tetragon
   sub-1%, ARMO 1–2.5%). `make bench` exists — turn it into a public
   `docs/performance.md` number per node and keep it honest.
4. **V8 perf-map is the #1 technical risk to keep managing**: plain
   `--perf-basic-prof` disables code compaction (permanent memory growth) —
   ensure the webhook injects `--perf-basic-prof-only-functions`; handle
   re-JIT stale map entries; have an honest story for bundled/minified deps.
5. **Disclose blind spots rather than silently missing**: io_uring bypasses
   syscall hooks entirely; frame-pointer-omitted runtimes break Python
   unwinding. Detect and warn (a trust play, and Datadog documented the same
   lessons publicly).
6. **Pilot shape**: one ~$15–30k, 90-day paid pilot, single service/cluster,
   written success criteria (seeded-attack detection, <2% overhead ceiling,
   false-alert budget, one unknown dependency network call surfaced). SOC 2
   Type 1 in parallel via Vanta/Drata — don't let it gate the pilot; a
   security whitepaper + fail-open architecture answers week-one questions.
7. **Positioning one-liners**: vs Socket — "the runtime half you don't
   have"; vs Sysdig/Datadog — "package-granular, no APM adoption required";
   vs DIY Tetragon — "attribution + baselining + OSV reachability Tetragon
   can't do."

---

## Demo recording — status and runbook

**It works today.** The full `make demo` flow passed every step in a live
audit: collector up in <2s, all seeds succeed, the flatmap-stream attack
replay fires a CRITICAL alert with evidence, `make demo-check` passes, and
all four tabs (Alerts, Reachability, Coverage, Fingerprints) render full data
(verified via real browser screenshots).

### Commands

```bash
cd ~/Desktop/a/startup/Heisenbug/code/ebpf/goodman
make demo                          # builds + starts on http://127.0.0.1:8844
# open http://127.0.0.1:8844/#alerts, follow the printed 60s talk track
# Ctrl-C stops everything

./bin/goodmanctl demo -attack-delay 45s   # time the attack for the camera
make demo-check                            # non-interactive pre-flight
```

Tips: reload the tab once after "Goodman demo is ready" before hitting
record (first paint shows zeros for <1s). The attack replay is one-shot — to
re-fire, restart the demo.

### Polish gaps before re-recording (ordered)

1. `demo_build/goodman_demo.mp4` is from Jul 8 (54s, 720p) and predates LSM
   enforcement, HA, and v0.2 docs — re-record.
2. Reachability tab shows "Reachable vulns: 0 / No known vulnerabilities" —
   seed 2–3 OSV vulns in `internal/demo/reachability.go` (or keep the camera
   on the 1,400/240 headline). This undercuts the "patch these first" pitch.
3. Malformed 6th alert: `jsonwebtoken ""→9.0.0` (empty old_version) in the
   seed (`internal/demo/product.go`) — fix or don't scroll to it.
4. Sensor flips to a yellow "stale" badge ~7 min after start once seeded
   ingest stops — record the Coverage tab early or trickle a heartbeat.
5. Fingerprints shows "LIBRARY QUALITY: WARNING — 2%" because the 240
   `demo-dep-NNNN` filler packages sit in "learning" — off-message on camera;
   the synthetic names also show if search is opened.
6. SSE 60s timeout (P0.5) can flicker the "Live" indicator during a >60s
   take (UI auto-recovers; fix P0.5 to eliminate).
7. Screenshot tooling: one-shot `chrome --headless --screenshot` captures the
   pre-fetch empty state — `demo_build/capture_screens.py` should be
   re-checked before regenerating the video; use CDP/interactive capture.
   (System playwright install is broken; raw CDP works.)

### What makes the recording land (research-backed)
- Name the malware: frame the replay as "this is what Mini-Shai-Hulud /
  event-stream did," not a synthetic toy.
- The "aha" is attribution, not detection — hold on the screen that says
  *which package* made the syscall.
- Show drift live: baseline → poisoned version install → alert with the
  behavior diff.
- Say "block mode is off by default and fail-open" out loud — it neutralizes
  the outage fear that stalls enforcement deals.

---

## Suggested sequencing

| Order | Work | Why first |
|---|---|---|
| 1 | Demo polish gaps 2–5 + re-record video | Customer-facing, hours of work |
| 2 | P0.5 SSE timeout, P0.7 reachability scope fallback, gofmt | Small, high-visibility fixes |
| 3 | P0.6 OSV degraded flag + per-service refresh errors | Protects pilot trust (false all-clear) |
| 4 | P0.4 fork-follow | Core detection claim; needed for an honest Shai-Hulud replay |
| 5 | P0.1–P0.3 LSM fixes + real deny e2e | Before anyone enables block mode |
| 6 | P1 backend batch (alerts pagination, migrations tx, reopen-on-merge, /metrics auth) | Pilot hardening |
| 7 | P1 dashboard batch (shared SSE, onError wiring, rollback command) | Pilot hardening |
| 8 | Benchmark publication + blind-spot docs (P2.3, P2.5) | Sales collateral |

HA-specific findings (enforce state, fingerprint merge race, migration race)
can be deferred if the pilot is single-replica — but then say so in the Helm
values and pilot runbook.
