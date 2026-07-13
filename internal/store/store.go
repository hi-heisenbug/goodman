// Package store persists fingerprints and alerts. Production uses
// PostgreSQL (dsn "postgres://..."); the local dev harness uses SQLite
// (dsn "sqlite:///path/to.db" or a bare file path). One SQL codepath
// serves both — $N placeholders are valid in both drivers.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/hi-heisenbug/goodman/internal/model"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db      *sql.DB
	dialect string // "postgres" | "sqlite"
}

func Open(dsn string) (*Store, error) {
	var driver, dialect, connStr string
	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		driver, dialect, connStr = "pgx", "postgres", dsn
	case strings.HasPrefix(dsn, "sqlite://"):
		driver, dialect, connStr = "sqlite", "sqlite", strings.TrimPrefix(dsn, "sqlite://")
	default:
		driver, dialect, connStr = "sqlite", "sqlite", dsn
	}
	if dialect == "sqlite" {
		// Single writer + busy timeout: SQLite is the dev harness, keep it robust.
		connStr += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open(driver, connStr)
	if err != nil {
		return nil, err
	}
	if dialect == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	s := &Store{db: db, dialect: dialect}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect %s: %w", dialect, err)
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ping verifies the backing database is reachable (readiness probes).
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// PruneResolvedAlerts deletes resolved alerts detected before cutoff and
// returns how many rows were removed. Open and acknowledged alerts are never
// pruned; an operator still has to act on them.
func (s *Store) PruneResolvedAlerts(ctx context.Context, cutoff time.Time) (int64, error) {
	cutoffNs := cutoff.UnixNano()
	if cutoffNs < 0 { // pre-epoch cutoff would wrap the uint64 and delete everything
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM alerts WHERE status=$1 AND detected_at < $2`,
		model.AlertResolved, uint64(cutoffNs))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// StoredLockfile is a persisted package-lock.json for one service scope.
type StoredLockfile struct {
	Service    string
	Content    string
	UploadedAt uint64
}

// SaveLockfile persists (or replaces) the lockfile for a service scope so the
// reachability report can be recomputed on a schedule as fingerprints change.
func (s *Store) SaveLockfile(ctx context.Context, service, content string, uploadedAt uint64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO lockfiles (service, content, uploaded_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (service) DO UPDATE SET content=EXCLUDED.content, uploaded_at=EXCLUDED.uploaded_at`,
		service, content, uploadedAt)
	return err
}

// ListLockfiles returns every stored lockfile (all service scopes).
func (s *Store) ListLockfiles(ctx context.Context) ([]StoredLockfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT service, content, uploaded_at FROM lockfiles ORDER BY service`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredLockfile
	for rows.Next() {
		var lf StoredLockfile
		if err := rows.Scan(&lf.Service, &lf.Content, &lf.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, lf)
	}
	return out, rows.Err()
}

// StoredReachability is one persisted reachability snapshot, plus the
// immediately previous snapshot used for week-over-week deltas.
type StoredReachability struct {
	Report             string
	OSV                bool
	ComputedAt         uint64
	PreviousReport     string
	PreviousComputedAt uint64
}

// SaveReport stores the latest computed reachability report for a service,
// shifting the prior snapshot into previous_* so the API can return deltas.
func (s *Store) SaveReport(ctx context.Context, service, reportJSON string, osv bool, computedAt uint64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reachability_reports (service, report, osv, computed_at, previous_report, previous_computed_at)
		VALUES ($1,$2,$3,$4,'',0)
		ON CONFLICT (service) DO UPDATE SET
			previous_report=reachability_reports.report,
			previous_computed_at=reachability_reports.computed_at,
			report=EXCLUDED.report,
			osv=EXCLUDED.osv,
			computed_at=EXCLUDED.computed_at`,
		service, reportJSON, osv, computedAt)
	return err
}

// GetReport returns the stored report snapshot for a service. found is false
// when no snapshot exists yet.
func (s *Store) GetReport(ctx context.Context, service string) (StoredReachability, bool, error) {
	var out StoredReachability
	row := s.db.QueryRowContext(ctx, `
		SELECT report, osv, computed_at, previous_report, previous_computed_at
		FROM reachability_reports WHERE service=$1`, service)
	err := row.Scan(&out.Report, &out.OSV, &out.ComputedAt, &out.PreviousReport, &out.PreviousComputedAt)
	if err == sql.ErrNoRows {
		return StoredReachability{}, false, nil
	}
	if err != nil {
		return StoredReachability{}, false, err
	}
	return out, true, nil
}

// migrate applies pending migration files in name order. Applied files are
// recorded in schema_migrations so non-idempotent statements (ALTER TABLE on
// SQLite) run exactly once per database.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	suffix := "." + s.dialect + ".sql"
	for _, e := range entries { // ReadDir returns names sorted
		if !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		var applied int
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE name=$1`, e.Name()).Scan(&applied)
		if err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		sqlText, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, string(sqlText)); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		// ON CONFLICT DO NOTHING so concurrently-starting replicas do not
		// crash on a duplicate-key race (valid in both Postgres and SQLite).
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO schema_migrations (name, applied_at) VALUES ($1,$2) ON CONFLICT (name) DO NOTHING`,
			e.Name(), time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func normalizeOrigin(origin string) string {
	if origin == "" {
		return model.OriginLocal
	}
	return origin
}

// GetFingerprint loads one fingerprint; returns nil when absent.
func (s *Store) GetFingerprint(ctx context.Context, service, pkg, version string) (*model.Fingerprint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT behaviors, first_seen, last_seen, obs_count, is_baseline, origin
		 FROM fingerprints WHERE service=$1 AND package=$2 AND version=$3`,
		service, pkg, version)
	fp := model.Fingerprint{Service: service, Package: pkg, Version: version}
	var behaviorsJSON []byte
	err := row.Scan(&behaviorsJSON, &fp.FirstSeen, &fp.LastSeen, &fp.ObsCount, &fp.IsBaseline, &fp.Origin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
		return nil, err
	}
	fp.Origin = normalizeOrigin(fp.Origin)
	return &fp, nil
}

// MergeFingerprint runs merge on the current row (or a fresh one) inside a
// transaction, then writes it back. Postgres uses SELECT FOR UPDATE for
// row-level locking; SQLite relies on the single connection plus the tx.
func (s *Store) MergeFingerprint(ctx context.Context, service, pkg, version string, merge func(*model.Fingerprint)) (*model.Fingerprint, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	q := `SELECT behaviors, first_seen, last_seen, obs_count, is_baseline, origin
	      FROM fingerprints WHERE service=$1 AND package=$2 AND version=$3`
	if s.dialect == "postgres" {
		q += ` FOR UPDATE`
	}
	fp := &model.Fingerprint{Service: service, Package: pkg, Version: version, Behaviors: map[string]model.BehaviorStat{}}
	var behaviorsJSON []byte
	err = tx.QueryRowContext(ctx, q, service, pkg, version).Scan(
		&behaviorsJSON, &fp.FirstSeen, &fp.LastSeen, &fp.ObsCount, &fp.IsBaseline, &fp.Origin)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
			return nil, err
		}
		if fp.Behaviors == nil {
			fp.Behaviors = map[string]model.BehaviorStat{}
		}
		fp.Origin = normalizeOrigin(fp.Origin)
	}

	merge(fp)

	behaviorsJSON, err = json.Marshal(fp.Behaviors)
	if err != nil {
		return nil, err
	}
	origin := normalizeOrigin(fp.Origin)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO fingerprints (service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (service, package, version) DO UPDATE SET
		  behaviors=EXCLUDED.behaviors, last_seen=EXCLUDED.last_seen,
		  obs_count=EXCLUDED.obs_count, is_baseline=EXCLUDED.is_baseline,
		  first_seen=EXCLUDED.first_seen`,
		fp.Service, fp.Package, fp.Version, string(behaviorsJSON),
		fp.FirstSeen, fp.LastSeen, fp.ObsCount, fp.IsBaseline, origin)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	fp.Origin = origin
	return fp, nil
}

// UpsertFingerprint writes the full fingerprint state. Origin is set on insert
// only; live merges never flip imported provenance back to local.
func (s *Store) UpsertFingerprint(ctx context.Context, fp *model.Fingerprint) error {
	behaviorsJSON, err := json.Marshal(fp.Behaviors)
	if err != nil {
		return err
	}
	origin := normalizeOrigin(fp.Origin)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO fingerprints (service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (service, package, version) DO UPDATE SET
		  behaviors=EXCLUDED.behaviors, last_seen=EXCLUDED.last_seen,
		  obs_count=EXCLUDED.obs_count, is_baseline=EXCLUDED.is_baseline,
		  first_seen=EXCLUDED.first_seen`,
		fp.Service, fp.Package, fp.Version, string(behaviorsJSON),
		fp.FirstSeen, fp.LastSeen, fp.ObsCount, fp.IsBaseline, origin)
	return err
}

// ImportOutcome is which row of the import conflict matrix applied.
type ImportOutcome string

const (
	ImportImported           ImportOutcome = "imported"
	ImportSkippedLocal       ImportOutcome = "skipped_local"
	ImportReplaced           ImportOutcome = "replaced"
	ImportIgnoredNonBaseline ImportOutcome = "ignored_non_baseline"
)

// ImportFingerprint upserts one baseline using the multi-cluster conflict
// matrix: local rows are never clobbered; imported rows may be replaced.
func (s *Store) ImportFingerprint(ctx context.Context, fp *model.Fingerprint) (ImportOutcome, error) {
	if !fp.IsBaseline {
		return ImportIgnoredNonBaseline, nil
	}
	existing, err := s.GetFingerprint(ctx, fp.Service, fp.Package, fp.Version)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.Origin == model.OriginLocal {
		return ImportSkippedLocal, nil
	}
	behaviorsJSON, err := json.Marshal(fp.Behaviors)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO fingerprints (service, package, version, behaviors, first_seen,
		                          last_seen, obs_count, is_baseline, origin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,TRUE,$8)
		ON CONFLICT (service, package, version) DO UPDATE SET
		  behaviors=EXCLUDED.behaviors, first_seen=EXCLUDED.first_seen,
		  last_seen=EXCLUDED.last_seen, obs_count=EXCLUDED.obs_count,
		  is_baseline=TRUE
		WHERE fingerprints.origin = $9`,
		fp.Service, fp.Package, fp.Version, string(behaviorsJSON),
		fp.FirstSeen, fp.LastSeen, fp.ObsCount, model.OriginImported,
		model.OriginImported)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return ImportReplaced, nil
	}
	return ImportImported, nil
}

// ListBaselines returns promoted fingerprints only (for export).
func (s *Store) ListBaselines(ctx context.Context) ([]model.Fingerprint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin
		FROM fingerprints WHERE is_baseline=TRUE
		ORDER BY service, package, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Fingerprint
	for rows.Next() {
		var fp model.Fingerprint
		var behaviorsJSON []byte
		if err := rows.Scan(&fp.Service, &fp.Package, &fp.Version, &behaviorsJSON,
			&fp.FirstSeen, &fp.LastSeen, &fp.ObsCount, &fp.IsBaseline, &fp.Origin); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
			return nil, err
		}
		fp.Origin = normalizeOrigin(fp.Origin)
		out = append(out, fp)
	}
	return out, rows.Err()
}

// ListFingerprints returns fingerprints filtered by optional service/package.
func (s *Store) ListFingerprints(ctx context.Context, service, pkg string) ([]model.Fingerprint, error) {
	q := `SELECT service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin
	      FROM fingerprints WHERE 1=1`
	var args []any
	if service != "" {
		args = append(args, service)
		q += fmt.Sprintf(" AND service=$%d", len(args))
	}
	if pkg != "" {
		args = append(args, pkg)
		q += fmt.Sprintf(" AND package=$%d", len(args))
	}
	q += " ORDER BY service, package, version"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Fingerprint
	for rows.Next() {
		var fp model.Fingerprint
		var behaviorsJSON []byte
		if err := rows.Scan(&fp.Service, &fp.Package, &fp.Version, &behaviorsJSON,
			&fp.FirstSeen, &fp.LastSeen, &fp.ObsCount, &fp.IsBaseline, &fp.Origin); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
			return nil, err
		}
		fp.Origin = normalizeOrigin(fp.Origin)
		out = append(out, fp)
	}
	return out, rows.Err()
}

// LatestBaseline returns the most recently seen baseline fingerprint for a
// (service, package), excluding the given version. nil when none exists.
func (s *Store) LatestBaseline(ctx context.Context, service, pkg, excludeVersion string) (*model.Fingerprint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT version, behaviors, first_seen, last_seen, obs_count, origin
		 FROM fingerprints
		 WHERE service=$1 AND package=$2 AND version<>$3 AND is_baseline=TRUE
		 ORDER BY last_seen DESC LIMIT 1`,
		service, pkg, excludeVersion)
	fp := model.Fingerprint{Service: service, Package: pkg, IsBaseline: true}
	var behaviorsJSON []byte
	err := row.Scan(&fp.Version, &behaviorsJSON, &fp.FirstSeen, &fp.LastSeen, &fp.ObsCount, &fp.Origin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
		return nil, err
	}
	fp.Origin = normalizeOrigin(fp.Origin)
	return &fp, nil
}

// UpsertAlert inserts a new alert or merges new behaviors into an existing
// open alert with the same deterministic id. The merge runs inside a
// transaction (Postgres SELECT FOR UPDATE) so concurrent replicas do not
// lose merged behaviors.
func (s *Store) UpsertAlert(ctx context.Context, a *model.Alert) (created bool, err error) {
	const maxAttempts = 4
	for i := 0; i < maxAttempts; i++ {
		var retry bool
		created, retry, err = s.upsertAlertTx(ctx, a)
		if err != nil || !retry {
			return created, err
		}
	}
	created, _, err = s.upsertAlertTx(ctx, a)
	return created, err
}

func (s *Store) upsertAlertTx(ctx context.Context, a *model.Alert) (created bool, retry bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback() //nolint:errcheck

	existing, err := s.getAlertForUpdate(ctx, tx, a.ID)
	if err != nil {
		return false, false, err
	}
	if existing != nil {
		mergeIntoAlert(existing, a)
		if err := s.updateAlertRow(ctx, tx, a); err != nil {
			return false, false, err
		}
		if err := tx.Commit(); err != nil {
			return false, false, err
		}
		return false, false, nil
	}

	if err := s.insertAlertRow(ctx, tx, a); err != nil {
		if isUniqueViolation(err) {
			return false, true, nil
		}
		return false, false, err
	}
	if err := tx.Commit(); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (s *Store) getAlertForUpdate(ctx context.Context, tx *sql.Tx, id string) (*model.Alert, error) {
	q := `SELECT id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, detected_at, status
	      FROM alerts WHERE id=$1`
	if s.dialect == "postgres" {
		q += ` FOR UPDATE`
	}
	return scanAlert(tx.QueryRowContext(ctx, q, id))
}

func mergeIntoAlert(existing, incoming *model.Alert) {
	incoming.WouldBlock = existing.WouldBlock || incoming.WouldBlock
	incoming.MatchedRules = mergeBehaviors(existing.MatchedRules, incoming.MatchedRules)
	incoming.NewBehaviors = mergeBehaviors(existing.NewBehaviors, incoming.NewBehaviors)
	incoming.Evidence = mergeEvidence(existing.Evidence, incoming.Evidence)
	incoming.Severity = maxSeverity(existing.Severity, incoming.Severity)
}

func (s *Store) updateAlertRow(ctx context.Context, tx *sql.Tx, a *model.Alert) error {
	nbJSON, _ := json.Marshal(a.NewBehaviors)
	ruJSON, _ := json.Marshal(orEmpty(a.MatchedRules))
	evJSON, _ := json.Marshal(orEmptyEvidence(a.Evidence))
	_, err := tx.ExecContext(ctx,
		`UPDATE alerts SET new_behaviors=$1, severity=$2, matched_rules=$3, evidence=$4, would_block=$5 WHERE id=$6`,
		string(nbJSON), a.Severity, string(ruJSON), string(evJSON), a.WouldBlock, a.ID)
	return err
}

func (s *Store) insertAlertRow(ctx context.Context, tx *sql.Tx, a *model.Alert) error {
	nbJSON, _ := json.Marshal(a.NewBehaviors)
	ruJSON, _ := json.Marshal(orEmpty(a.MatchedRules))
	evJSON, _ := json.Marshal(orEmptyEvidence(a.Evidence))
	_, err := tx.ExecContext(ctx, `
		INSERT INTO alerts (id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, detected_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		a.ID, a.Service, a.Package, a.OldVersion, a.NewVersion, a.Severity,
		string(nbJSON), string(ruJSON), string(evJSON), a.WouldBlock, a.DetectedAt, a.Status)
	return err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "23505")
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyEvidence(e []model.Evidence) []model.Evidence {
	if e == nil {
		return []model.Evidence{}
	}
	return e
}

// mergeEvidence unions evidence lists by behavior, keeping the earliest
// first-seen entry for each behavior.
func mergeEvidence(a, b []model.Evidence) []model.Evidence {
	byBehavior := map[string]int{}
	out := make([]model.Evidence, 0, len(a)+len(b))
	for _, e := range append(append([]model.Evidence{}, a...), b...) {
		i, seen := byBehavior[e.Behavior]
		if !seen {
			byBehavior[e.Behavior] = len(out)
			out = append(out, e)
			continue
		}
		if e.FirstSeen != 0 && (out[i].FirstSeen == 0 || e.FirstSeen < out[i].FirstSeen) {
			out[i].FirstSeen = e.FirstSeen
			out[i].Sensor = e.Sensor
		}
		out[i].Rules = mergeBehaviors(out[i].Rules, e.Rules)
	}
	return out
}

func (s *Store) GetAlert(ctx context.Context, id string) (*model.Alert, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, detected_at, status
		 FROM alerts WHERE id=$1`, id)
	return scanAlert(row)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAlert(row rowScanner) (*model.Alert, error) {
	var a model.Alert
	var nbJSON, ruJSON, evJSON []byte
	var oldVersion sql.NullString
	err := row.Scan(&a.ID, &a.Service, &a.Package, &oldVersion, &a.NewVersion,
		&a.Severity, &nbJSON, &ruJSON, &evJSON, &a.WouldBlock, &a.DetectedAt, &a.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.OldVersion = oldVersion.String
	if err := json.Unmarshal(nbJSON, &a.NewBehaviors); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(ruJSON, &a.MatchedRules); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(evJSON, &a.Evidence); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) ListAlerts(ctx context.Context, status string) ([]model.Alert, error) {
	q := `SELECT id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, detected_at, status
	      FROM alerts`
	var args []any
	if status != "" {
		q += " WHERE status=$1"
		args = append(args, status)
	}
	q += " ORDER BY detected_at DESC LIMIT 500"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// CountAlertsSince returns how many alerts (any status) were detected at or
// after sinceNs. Used by the Coverage panel's alert-budget burn rate.
func (s *Store) CountAlertsSince(ctx context.Context, sinceNs uint64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM alerts WHERE detected_at >= $1`, sinceNs).Scan(&n)
	return n, err
}

// CountWouldBlockSince returns how many alerts flagged would_block were
// detected at or after sinceNs (enforce=warn audit evidence).
func (s *Store) CountWouldBlockSince(ctx context.Context, sinceNs uint64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM alerts WHERE would_block AND detected_at >= $1`, sinceNs).Scan(&n)
	return n, err
}

func (s *Store) SetAlertStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET status=$1 WHERE id=$2`, status, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func mergeBehaviors(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

var severityRank = map[string]int{model.SeverityInfo: 0, model.SeverityWarn: 1, model.SeverityCritical: 2}

func maxSeverity(a, b string) string {
	if severityRank[b] > severityRank[a] {
		return b
	}
	return a
}
