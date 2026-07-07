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

	"github.com/goodman-sec/goodman/internal/model"
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

func (s *Store) migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	suffix := "." + s.dialect + ".sql"
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		sqlText, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, string(sqlText)); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

// GetFingerprint loads one fingerprint; returns nil when absent.
func (s *Store) GetFingerprint(ctx context.Context, service, pkg, version string) (*model.Fingerprint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT behaviors, first_seen, last_seen, obs_count, is_baseline
		 FROM fingerprints WHERE service=$1 AND package=$2 AND version=$3`,
		service, pkg, version)
	fp := model.Fingerprint{Service: service, Package: pkg, Version: version}
	var behaviorsJSON []byte
	err := row.Scan(&behaviorsJSON, &fp.FirstSeen, &fp.LastSeen, &fp.ObsCount, &fp.IsBaseline)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
		return nil, err
	}
	return &fp, nil
}

// UpsertFingerprint writes the full fingerprint state.
func (s *Store) UpsertFingerprint(ctx context.Context, fp *model.Fingerprint) error {
	behaviorsJSON, err := json.Marshal(fp.Behaviors)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO fingerprints (service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (service, package, version) DO UPDATE SET
		  behaviors=EXCLUDED.behaviors, last_seen=EXCLUDED.last_seen,
		  obs_count=EXCLUDED.obs_count, is_baseline=EXCLUDED.is_baseline,
		  first_seen=EXCLUDED.first_seen`,
		fp.Service, fp.Package, fp.Version, string(behaviorsJSON),
		fp.FirstSeen, fp.LastSeen, fp.ObsCount, fp.IsBaseline)
	return err
}

// ListFingerprints returns fingerprints filtered by optional service/package.
func (s *Store) ListFingerprints(ctx context.Context, service, pkg string) ([]model.Fingerprint, error) {
	q := `SELECT service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline
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
			&fp.FirstSeen, &fp.LastSeen, &fp.ObsCount, &fp.IsBaseline); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
			return nil, err
		}
		out = append(out, fp)
	}
	return out, rows.Err()
}

// LatestBaseline returns the most recently seen baseline fingerprint for a
// (service, package), excluding the given version. nil when none exists.
func (s *Store) LatestBaseline(ctx context.Context, service, pkg, excludeVersion string) (*model.Fingerprint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT version, behaviors, first_seen, last_seen, obs_count
		 FROM fingerprints
		 WHERE service=$1 AND package=$2 AND version<>$3 AND is_baseline=TRUE
		 ORDER BY last_seen DESC LIMIT 1`,
		service, pkg, excludeVersion)
	fp := model.Fingerprint{Service: service, Package: pkg, IsBaseline: true}
	var behaviorsJSON []byte
	err := row.Scan(&fp.Version, &behaviorsJSON, &fp.FirstSeen, &fp.LastSeen, &fp.ObsCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(behaviorsJSON, &fp.Behaviors); err != nil {
		return nil, err
	}
	return &fp, nil
}

// UpsertAlert inserts a new alert or merges new behaviors into an existing
// open alert with the same deterministic id.
func (s *Store) UpsertAlert(ctx context.Context, a *model.Alert) (created bool, err error) {
	existing, err := s.GetAlert(ctx, a.ID)
	if err != nil {
		return false, err
	}
	if existing != nil {
		merged := mergeBehaviors(existing.NewBehaviors, a.NewBehaviors)
		sev := maxSeverity(existing.Severity, a.Severity)
		nbJSON, _ := json.Marshal(merged)
		_, err := s.db.ExecContext(ctx,
			`UPDATE alerts SET new_behaviors=$1, severity=$2 WHERE id=$3`,
			string(nbJSON), sev, a.ID)
		return false, err
	}
	nbJSON, _ := json.Marshal(a.NewBehaviors)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO alerts (id, service, package, old_version, new_version, severity, new_behaviors, detected_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		a.ID, a.Service, a.Package, a.OldVersion, a.NewVersion, a.Severity,
		string(nbJSON), a.DetectedAt, a.Status)
	return err == nil, err
}

func (s *Store) GetAlert(ctx context.Context, id string) (*model.Alert, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, service, package, old_version, new_version, severity, new_behaviors, detected_at, status
		 FROM alerts WHERE id=$1`, id)
	return scanAlert(row)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAlert(row rowScanner) (*model.Alert, error) {
	var a model.Alert
	var nbJSON []byte
	var oldVersion sql.NullString
	err := row.Scan(&a.ID, &a.Service, &a.Package, &oldVersion, &a.NewVersion,
		&a.Severity, &nbJSON, &a.DetectedAt, &a.Status)
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
	return &a, nil
}

func (s *Store) ListAlerts(ctx context.Context, status string) ([]model.Alert, error) {
	q := `SELECT id, service, package, old_version, new_version, severity, new_behaviors, detected_at, status
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
