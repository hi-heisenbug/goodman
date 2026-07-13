# Implementation plan: multi-cluster fingerprint sharing (Phase 4)

> **Status:** implementation plan (2026-07-13) for `plan-deferred.md` Phase 4.
> **Gate:** first customer with 3+ clusters complains about re-learning
> baselines per cluster.
> **Scope guard:** per-customer, file-shaped, operator-driven. No
> collector-to-collector networking, no shared database, no sync daemon, no
> cross-customer anything.

## Shape of the feature

Baselines learned in cluster A seed cluster B via a JSON file the operator
moves themselves:

```bash
# on cluster A
goodmanctl fingerprints export > baselines.json
# on cluster B
goodmanctl fingerprints import baselines.json
```

Imported rows carry provenance (`origin = "imported"`) so trust and debugging
can always distinguish "we observed this here" from "someone gave us a file".

## Why the diff engine needs zero changes

This is the load-bearing design fact. `diff.Engine.React`
(`internal/diff/diff.go`) consults exactly two baseline signals:

1. `fp.IsBaseline` on the incoming fingerprint update (trigger 2:
   same-version drift), and
2. `store.LatestBaseline(...)`, whose SQL is
   `WHERE service=$1 AND package=$2 AND version<>$3 AND is_baseline=TRUE`.

An imported row is written with `is_baseline=TRUE`, so it participates in
both triggers identically to a locally promoted baseline: drift against it
alerts normally, and a version bump in cluster B diffs against the imported
baseline with no learning window. Similarly `fingerprint.Engine.Ingest`
guards promotion with `if !fp.IsBaseline && e.qualifies(fp)` — an imported
baseline is never "re-promoted". **No code change in `internal/diff` and no
behavioral change in `internal/fingerprint`;** the only fingerprint-engine
consideration is that `model.Fingerprint` gains an `Origin` field which must
round-trip through `Ingest`'s load→merge→upsert cycle (a store/model change,
§ below).

## Export format

`GET /v1/fingerprints/export` streams:

```jsonc
{
  "schema": "goodman.fingerprints.export/v1",   // import validates this
  "exported_at": 1783453745485152329,            // unix ns
  "collector": "goodman-collector-0",            // informational only
  "fingerprints": [
    {
      "service": "web",
      "package": "left-pad",
      "version": "1.3.0",
      "behaviors": { "READ /app/node_modules/left-pad/**": {"count": 812, "first": 0, "last": 0} },
      "first_seen": 0, "last_seen": 0, "obs_count": 812,
      "is_baseline": true,
      "origin": "local"                          // provenance in the SOURCE cluster
    }
  ]
}
```

Rules:

- **Only `is_baseline=TRUE` rows are exported.** Learning rows are
  half-observed noise; sharing them would seed cluster B with an incomplete
  behavior set and cause false drift alerts.
- The fingerprint objects are `model.Fingerprint` verbatim (plus `origin`) —
  no second serialization format to maintain.
- `schema` is a hard version gate: import rejects unknown values with a 400
  rather than best-effort parsing a future format.

## Import conflict matrix

Key = `(service, package, version)`. The invariant: **local observation
outranks a file.** An import never destroys or masks anything this cluster
learned itself.

| Local state at key | Import action | Counted as |
|---|---|---|
| no row | insert with `origin='imported'`, `is_baseline=TRUE` | `imported` |
| row with `origin='local'` — learning **or** baseline | skip, keep local row untouched | `skipped_local` |
| row with `origin='imported'` | replace (idempotent re-import; the file being imported wins) | `replaced` |
| incoming row has `is_baseline=false` | never written | `ignored_non_baseline` |

The whole matrix is one SQL statement, valid in **both** dialects (this is
why `origin` lives in the row and not in a side table):

```sql
INSERT INTO fingerprints (service, package, version, behaviors, first_seen,
                          last_seen, obs_count, is_baseline, origin)
VALUES ($1,$2,$3,$4,$5,$6,$7,TRUE,'imported')
ON CONFLICT (service, package, version) DO UPDATE SET
  behaviors=EXCLUDED.behaviors, first_seen=EXCLUDED.first_seen,
  last_seen=EXCLUDED.last_seen, obs_count=EXCLUDED.obs_count,
  is_baseline=TRUE
WHERE fingerprints.origin = 'imported'
```

(Conditional `DO UPDATE ... WHERE` is supported by both Postgres and SQLite;
the `skipped_local` count falls out of rows-affected bookkeeping.)

`origin` is immutable after insert: live events merging into an imported
baseline do **not** flip it to `local`. Provenance answers "where did this
baseline originally come from", which never changes; the dashboard can show
live-observation stats next to the tag if operators ask later.

## Migration 006 (both dialects, per the store rule)

- `internal/store/migrations/006_fingerprint_origin.postgres.sql`

```sql
ALTER TABLE fingerprints ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';
```

- `internal/store/migrations/006_fingerprint_origin.sqlite.sql`

```sql
ALTER TABLE fingerprints ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';
```

Valid values `'local' | 'imported'`, enforced in Go at the import boundary
(matching how `status` and `severity` are handled today — no CHECK
constraint divergence between dialects).

## Code changes

**`internal/model/types.go`** — `Fingerprint` gains
`Origin string \`json:"origin"\`` . `RawEvent` is untouched (this is not a
wire-struct change; the one rule does not apply).

**`internal/store/store.go`**

- `GetFingerprint`, `ListFingerprints`, `LatestBaseline` select and scan
  `origin`.
- `UpsertFingerprint` writes `origin` (empty string normalized to `'local'`)
  so `fingerprint.Engine.Ingest`'s round-trip preserves provenance.
- New `ImportFingerprint(ctx, fp)` implementing the conflict-matrix SQL,
  returning which matrix row applied.
- New `ListBaselines(ctx)` (or reuse `ListFingerprints` + filter in the
  handler — decide at build time; prefer the SQL filter to avoid shipping
  learning rows through JSON for large fleets).

**`internal/api/api.go`** — two routes in `Router()`:

```go
r.Get("/v1/fingerprints/export", requireToken(s.Auth.APIToken, false, s.handleExportFingerprints))
r.Post("/v1/fingerprints/import", requireToken(s.Auth.APIToken, false, s.handleImportFingerprints))
```

Both are **operator-token class** (`APIToken`), consistent with the other
fingerprint endpoints. Import enforces a request-body limit (reuse the 64 MiB
`io.LimitReader` discipline from `handleIngest`), validates `schema`, and
returns the counts:

```json
{ "imported": 41, "skipped_local": 3, "replaced": 0, "ignored_non_baseline": 0 }
```

**`internal/api/auth_test.go`** — add both endpoints to the auth-class table:
401 without the API token, pass with it, and (explicitly) assert the ingest
token does *not* grant access. This is the AGENTS.md rule: no accidentally
open operator endpoints.

**`cmd/goodmanctl/main.go`** — `fingerprints` grows two subcommands,
dispatched before flag parsing (`goodmanctl fingerprints export|import`,
keeping plain `goodmanctl fingerprints` list behavior unchanged):

- `export [-collector URL] [-token T]` — GET, write body to stdout.
- `import <file> [-collector URL] [-token T]` — POST the file, print the
  counts summary. Non-zero exit on any HTTP error.

Update the `usage` string. Both honor `GOODMAN_API_TOKEN` /
`GOODMAN_COLLECTOR_URL` like every other subcommand.

**Dashboard** — `dashboard/src/types.ts` `Fingerprint` gains
`origin: "local" | "imported"`; `App.tsx` renders a small "Imported" tag next
to the existing `state-chip` (Baseline/Learning) on fingerprint rows. Then
`make dashboard` and commit `internal/api/ui/dist/` (stale hashed assets out,
new ones in). Verify desktop + mobile widths against live `/v1/*` data — the
tag must not cause horizontal scroll next to long package names.

**Docs** — `docs/api.md` (both endpoints, schema, conflict matrix summary),
`docs/deployment.md` (a "Multi-cluster: seeding baselines" runbook section:
export from the mature cluster, import into the new one, what the origin tag
means, that re-import refreshes imported rows only), `CHANGELOG.md`.

## Test plan

| Layer | Test |
|---|---|
| store | `store_test.go`: migration applies on both dialects; `ImportFingerprint` covers every conflict-matrix row (fresh insert / local-learning skip / local-baseline skip / imported replace / non-baseline ignore); `UpsertFingerprint` preserves `origin` through an Ingest round-trip |
| fingerprint | imported row (+ live events) is never re-promoted and `Origin` survives `Ingest` |
| diff | one test asserting drift against an imported baseline alerts identically to a promoted one (belt-and-braces for the "zero changes" claim) |
| api | handler tests: export emits only baselines + schema envelope; import validates schema, enforces body limit, returns counts; `auth_test.go` classes |
| end-to-end | export from collector A (seeded via the smoke/replay harness), import into a **fresh** collector B, run a drift scenario against B → alert fires with no learning window; imported rows show `origin:"imported"` in `/v1/fingerprints` |
| CLI | exercised by the end-to-end via `goodmanctl fingerprints export/import` |

## Verification

```bash
make vet && make test && make smoke && make replay
make dashboard && (cd dashboard && npm audit --audit-level=moderate) && make build
make helm-lint   # no chart changes expected, but keep the gate
```

No `bpf/`/`internal/loader/` change → no e2e; say so in the summary.

## Anti-patterns (restated from plan-deferred.md + AGENTS.md)

- Import never clobbers a locally learned baseline — local observation is
  higher-trust than a file.
- Both SQL dialects or the migration doesn't merge.
- New endpoints get explicit auth classes and `auth_test.go` cases.
- No central service, no anonymization work, no cross-customer sharing.
- Don't forget `make dashboard` + committing `dist/`.

## Effort

~1 week: 2 days store/model/migration + conflict matrix tests, 1 day
API + auth + CLI, 1 day dashboard tag + rebuild + visual check, 1 day
end-to-end + docs.
