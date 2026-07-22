# Research: true HA collector (Phase 5)

> **Status (2026-07-13):** Phase 5 HA **scaffolding shipped** — N replicas,
> Postgres advisory-lock leader election, transactional alert upsert, Helm PDB +
> anti-affinity. Sub-step 5a (`MergeFingerprint`) was the first brick. **Two-
> replica live proof** against real Postgres still needs a human or staging CI
> run — see `docs/release.md`.

## 5a. The fingerprint ingest race (exists today, single replica)

`fingerprint.Engine.Ingest` (`internal/fingerprint/fingerprint.go`) does a
non-transactional read-modify-write per `(service, package, version)`:

1. `store.GetFingerprint` — read the full row,
2. merge the batch into the in-memory `Behaviors` map / counters,
3. `store.UpsertFingerprint` — write the **full row back**
   (`ON CONFLICT ... DO UPDATE` sets `behaviors=EXCLUDED.behaviors`, i.e.
   last-writer-wins on the whole JSON blob).

`api.handleIngest` runs concurrently — one goroutine per sensor POST, no
serialization above the store. Two sensors (or one sensor's overlapping
batches) touching the same key interleave as: A reads, B reads, A writes,
B writes → **A's behaviors, counts, and even a just-set `is_baseline` flag
are silently overwritten**.

Dialect notes:

- **Postgres:** fully exposed; `database/sql` uses a connection pool and
  statements from concurrent requests interleave freely.
- **SQLite:** `db.SetMaxOpenConns(1)` serializes individual *statements*,
  not the read→write *sequence*, so the race exists there too — the window
  is just narrower.

Consequences: undercounted `obs_count` (delayed baseline promotion), lost
fresh behaviors (a drift alert deferred until the behavior recurs), and in
the worst interleaving a lost promotion. Nothing corrupts, but the product's
core loop is quietly lossy under concurrency.

### Fix (small, both dialects, one codepath)

Move the merge inside one database transaction with row-level locking:

```go
// store.MergeFingerprint runs merge() on the current row (or a fresh one)
// while holding the row lock, then writes it back, all in one tx.
func (s *Store) MergeFingerprint(ctx context.Context,
    service, pkg, version string,
    merge func(fp *model.Fingerprint)) (*model.Fingerprint, error)
```

- Postgres: `SELECT ... FOR UPDATE` inside the transaction — the dialect
  field the store already carries (`s.dialect`) gates appending the
  `FOR UPDATE` suffix; the rest of the SQL is shared.
- SQLite: no `FOR UPDATE` syntax; the single connection plus the transaction
  (write statements upgrade it; `busy_timeout(5000)` is already set in
  `Open`) provides the same mutual exclusion.
- `fingerprint.Engine.Ingest` moves its per-key merge body into the callback;
  its public behavior (`Update`, `FreshBehaviors`, `JustPromoted`) is
  unchanged, so `diff`, `api`, smoke, and replay are untouched.
- Insert-vs-update: when the row doesn't exist, `FOR UPDATE` locks nothing —
  keep the final write as the existing `ON CONFLICT ... DO UPDATE` upsert so
  a concurrent first-insert degrades to a retry/last-merge rather than an
  error. (Two concurrent *creations* of the same key remain a rare
  lose-one-batch case on Postgres round one; document it, and note the full
  HA phase closes it with a retry loop.)

**Test:** a store-level concurrency test — N goroutines ingesting disjoint
behavior sets for the same key through `fingerprint.Engine.Ingest`, assert
the final row is the exact union and `obs_count` is the exact sum. Run
against SQLite in CI (the harness default); the Postgres path shares the
codepath and gets the dockerized run when full HA lands.

**Not in 5a:** `UpsertAlert`'s get-then-insert (its deterministic `alertID`
plus single-replica deployment makes collisions merge-idempotent enough for
now; it becomes `ON CONFLICT DO NOTHING`-based in full HA), leader election,
Helm changes.

**Effort: 2–3 days** including the concurrency test and docs note.

**Verification:** `make vet && make test && make smoke && make replay`
(plus `make bench` to confirm no ingest regression — the transaction adds
one round trip per touched key per batch).

## Full HA (shipped codepath; live proof pending)

Recorded design — now implemented except the dockerized two-replica proof.

| Piece | Design | Status |
|---|---|---|
| N replicas | `collector.replicas: N` behind the existing Service; **Postgres required** — fail fast at startup when replicas>1 is implied and the DSN is SQLite (single-writer by design, stays the single-replica path) | **Shipped** — Helm guard, `GOODMAN_HA_REPLICAS`, collector fatals on SQLite+N>1 |
| Concurrent ingest | 5a's `MergeFingerprint` already correct cross-replica on Postgres (`FOR UPDATE`); `UpsertAlert` transactional merge keyed on deterministic `alertID` | **Shipped** — create-race retry loop in `UpsertAlert`; concurrency test on SQLite |
| Singleton background loops | `pruneLoop`, `reachabilityLoop`, `digestLoop` (`cmd/collector/maintenance.go`) must not double-fire (double webhooks/digests). Postgres advisory-lock leader election — no new dependency | **Shipped** — `store.WithLeader`, lock keys 1/2/3 |
| SSE | per-replica streams: a client sees events ingested by *its* replica only. Acceptable as a documented round-one limitation (the dashboard polls REST for state; SSE is live flavor) | **Shipped (docs)** — `docs/deployment.md` HA section |
| readyz | unchanged semantics (DB ping) work per-replica already | **Shipped** |
| Migrations | `migrate()` already tolerates concurrent replica startup (`ON CONFLICT (name) DO NOTHING` on `schema_migrations`) — verify with a two-replica startup test | **Shipped** (codepath); explicit two-replica startup test not added |
| Proof | two replicas ingest the replay corpus concurrently → identical final fingerprints, exactly one alert per scenario; kill either replica mid-ingest → nothing lost; SQLite single-replica path still green | **Human/CI follow-up** — `docs/release.md` |

**Total: 2–4 weeks**, matching plan-deferred. DoD as written there.

## Redis: NO-GO (pending measurement)

Decision: **do not add Redis now, and do not add it as part of full HA by
default.** It enters only as a measured-need dependency *inside* Phase 5,
per plan-deferred's "Redis and hosted SaaS are deliberately not phases."

The measurement bar, in order:

1. `make bench` — the existing no-root benchmarks
   (`internal/fingerprint`, `internal/attribute`) establish the in-process
   ceiling for ingest merge + canonicalization.
2. After 5a lands, bench the transactional path against a real (dockerized)
   Postgres: batched upserts, realistic key cardinality, 2 concurrent
   writers. This is the number that matters.
3. Only if a real cluster's sustained event rate exceeds what (2) sustains —
   with batching already tuned (`-batch-interval`) — consider Redis as a
   write-through cache for hot fingerprints. It would be a dependency of the
   HA phase, never a milestone.

Rationale: fingerprint writes are already batched and per-key; the working
set (distinct `(service, package, version)` keys) is small; and every extra
Helm dependency is one more way a pilot install fails. Default assumption:
not needed. Record the bench numbers in this file when they are produced.

## What would change these decisions

- A signed annual contract whose security review demands no-SPOF → execute
  the full-HA table above.
- Bench evidence (step 2) showing Postgres cannot sustain a real cluster's
  ingest → open the Redis question with numbers attached.
- 5a's concurrency test flaking on SQLite → revisit the
  single-connection-as-lock assumption before shipping.
