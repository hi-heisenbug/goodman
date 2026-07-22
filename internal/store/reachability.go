package store

import (
	"context"
	"database/sql"
	"time"
)

// StoredLockfile is a persisted package-lock.json for one service scope.
type StoredLockfile struct {
	Service    string
	Content    string
	UploadedAt uint64
}

// SaveLockfile persists (or replaces) the lockfile for a service scope so the
// reachability report can be recomputed as fingerprints change.
func (s *Store) SaveLockfile(ctx context.Context, service, content string, uploadedAt uint64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO lockfiles (service, content, uploaded_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (service) DO UPDATE SET content=EXCLUDED.content, uploaded_at=EXCLUDED.uploaded_at`,
		service, content, uploadedAt)
	return err
}

func (s *Store) ListLockfiles(ctx context.Context) ([]StoredLockfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT service, content, uploaded_at FROM lockfiles ORDER BY service`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredLockfile
	for rows.Next() {
		var lockfile StoredLockfile
		if err := rows.Scan(&lockfile.Service, &lockfile.Content, &lockfile.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, lockfile)
	}
	return out, rows.Err()
}

// StoredReachability is one persisted reachability snapshot plus the newest
// historical snapshot at least a week older, used for week-over-week deltas.
type StoredReachability struct {
	Service            string
	Report             string
	OSV                bool
	ComputedAt         uint64
	PreviousReport     string
	PreviousComputedAt uint64
}

func (s *Store) ListReports(ctx context.Context) ([]StoredReachability, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT service FROM reachability_reports ORDER BY service`)
	if err != nil {
		return nil, err
	}
	services, err := scanStrings(rows)
	if err != nil {
		return nil, err
	}
	out := make([]StoredReachability, 0, len(services))
	for _, service := range services {
		stored, found, err := s.GetReport(ctx, service)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		stored.Service = service
		out = append(out, stored)
	}
	return out, nil
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

const reachabilityComparisonWindow = 7 * 24 * time.Hour

func (s *Store) SaveReport(ctx context.Context, service, reportJSON string, osv bool, computedAt uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reachability_report_history (service, report, osv, computed_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (service, computed_at) DO UPDATE SET report=EXCLUDED.report, osv=EXCLUDED.osv`,
		service, reportJSON, osv, computedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reachability_reports (service, report, osv, computed_at, previous_report, previous_computed_at)
		VALUES ($1,$2,$3,$4,'',0)
		ON CONFLICT (service) DO UPDATE SET
			report=EXCLUDED.report,
			osv=EXCLUDED.osv,
			computed_at=EXCLUDED.computed_at`,
		service, reportJSON, osv, computedAt); err != nil {
		return err
	}
	if err := pruneReachabilityHistory(ctx, tx, service, computedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func pruneReachabilityHistory(ctx context.Context, tx *sql.Tx, service string, computedAt uint64) error {
	retention := uint64((30 * 24 * time.Hour).Nanoseconds())
	if computedAt <= retention {
		return nil
	}
	_, err := tx.ExecContext(ctx,
		`DELETE FROM reachability_report_history WHERE service=$1 AND computed_at < $2`,
		service, computedAt-retention)
	return err
}

func (s *Store) GetReport(ctx context.Context, service string) (StoredReachability, bool, error) {
	var out StoredReachability
	err := s.db.QueryRowContext(ctx, `
		SELECT report, osv, computed_at, previous_report, previous_computed_at
		FROM reachability_reports WHERE service=$1`, service).
		Scan(&out.Report, &out.OSV, &out.ComputedAt, &out.PreviousReport, &out.PreviousComputedAt)
	if err == sql.ErrNoRows {
		return StoredReachability{}, false, nil
	}
	if err != nil {
		return StoredReachability{}, false, err
	}
	if err := s.loadPreviousReport(ctx, service, &out); err != nil {
		return StoredReachability{}, false, err
	}
	return out, true, nil
}

func (s *Store) loadPreviousReport(ctx context.Context, service string, out *StoredReachability) error {
	windowNs := uint64(reachabilityComparisonWindow.Nanoseconds())
	if out.ComputedAt < windowNs {
		return nil
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT report, computed_at FROM reachability_report_history
		WHERE service=$1 AND computed_at <= $2
		ORDER BY computed_at DESC LIMIT 1`, service, out.ComputedAt-windowNs).
		Scan(&out.PreviousReport, &out.PreviousComputedAt)
	if err == sql.ErrNoRows {
		out.PreviousReport = ""
		out.PreviousComputedAt = 0
		return nil
	}
	return err
}
