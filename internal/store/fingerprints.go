package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func normalizeOrigin(origin string) string {
	if origin == "" {
		return model.OriginLocal
	}
	return origin
}

func (s *Store) GetFingerprint(ctx context.Context, service, pkg, version string) (*model.Fingerprint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT behaviors, first_seen, last_seen, obs_count, is_baseline, origin
		 FROM fingerprints WHERE service=$1 AND package=$2 AND version=$3`,
		service, pkg, version)
	fingerprint := model.Fingerprint{Service: service, Package: pkg, Version: version}
	var behaviorsJSON []byte
	err := row.Scan(&behaviorsJSON, &fingerprint.FirstSeen, &fingerprint.LastSeen,
		&fingerprint.ObsCount, &fingerprint.IsBaseline, &fingerprint.Origin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(behaviorsJSON, &fingerprint.Behaviors); err != nil {
		return nil, err
	}
	fingerprint.Origin = normalizeOrigin(fingerprint.Origin)
	return &fingerprint, nil
}

type FingerprintKey struct {
	Service string
	Package string
	Version string
}

func (s *Store) GetFingerprints(ctx context.Context, keys []FingerprintKey) ([]model.Fingerprint, error) {
	const keysPerQuery = 250
	var out []model.Fingerprint
	for start := 0; start < len(keys); start += keysPerQuery {
		end := min(start+keysPerQuery, len(keys))
		batch, err := s.getFingerprints(ctx, keys[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (s *Store) getFingerprints(ctx context.Context, keys []FingerprintKey) ([]model.Fingerprint, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	query := `SELECT service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin
	          FROM fingerprints WHERE `
	args := make([]any, 0, len(keys)*3)
	for i, key := range keys {
		if i > 0 {
			query += " OR "
		}
		args = append(args, key.Service, key.Package, key.Version)
		query += fmt.Sprintf("(service=$%d AND package=$%d AND version=$%d)", len(args)-2, len(args)-1, len(args))
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFingerprints(rows)
}

func (s *Store) MergeFingerprint(ctx context.Context, service, pkg, version string, merge func(*model.Fingerprint)) (*model.Fingerprint, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	if s.dialect == "postgres" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, fingerprintLockKey(service, pkg, version)); err != nil {
			return nil, err
		}
	}
	fingerprint, err := readFingerprintForUpdate(ctx, tx, s.dialect, service, pkg, version)
	if err != nil {
		return nil, err
	}
	merge(fingerprint)
	if err := writeFingerprint(ctx, tx, fingerprint); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	fingerprint.Origin = normalizeOrigin(fingerprint.Origin)
	return fingerprint, nil
}

func readFingerprintForUpdate(ctx context.Context, tx *sql.Tx, dialect, service, pkg, version string) (*model.Fingerprint, error) {
	query := `SELECT behaviors, first_seen, last_seen, obs_count, is_baseline, origin
	          FROM fingerprints WHERE service=$1 AND package=$2 AND version=$3`
	if dialect == "postgres" {
		query += ` FOR UPDATE`
	}
	fingerprint := &model.Fingerprint{
		Service: service, Package: pkg, Version: version,
		Behaviors: map[string]model.BehaviorStat{},
	}
	var behaviorsJSON []byte
	err := tx.QueryRowContext(ctx, query, service, pkg, version).Scan(
		&behaviorsJSON, &fingerprint.FirstSeen, &fingerprint.LastSeen,
		&fingerprint.ObsCount, &fingerprint.IsBaseline, &fingerprint.Origin)
	if err == sql.ErrNoRows {
		return fingerprint, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(behaviorsJSON, &fingerprint.Behaviors); err != nil {
		return nil, err
	}
	if fingerprint.Behaviors == nil {
		fingerprint.Behaviors = map[string]model.BehaviorStat{}
	}
	fingerprint.Origin = normalizeOrigin(fingerprint.Origin)
	return fingerprint, nil
}

func writeFingerprint(ctx context.Context, tx *sql.Tx, fingerprint *model.Fingerprint) error {
	behaviorsJSON, err := json.Marshal(fingerprint.Behaviors)
	if err != nil {
		return err
	}
	fingerprint.Origin = normalizeOrigin(fingerprint.Origin)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO fingerprints (service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (service, package, version) DO UPDATE SET
		  behaviors=EXCLUDED.behaviors, last_seen=EXCLUDED.last_seen,
		  obs_count=EXCLUDED.obs_count, is_baseline=EXCLUDED.is_baseline,
		  first_seen=EXCLUDED.first_seen`,
		fingerprint.Service, fingerprint.Package, fingerprint.Version, string(behaviorsJSON),
		fingerprint.FirstSeen, fingerprint.LastSeen, fingerprint.ObsCount,
		fingerprint.IsBaseline, fingerprint.Origin)
	return err
}

func fingerprintLockKey(service, pkg, version string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(service))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(pkg))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(version))
	return int64(hash.Sum64())
}

func (s *Store) UpsertFingerprint(ctx context.Context, fingerprint *model.Fingerprint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := writeFingerprint(ctx, tx, fingerprint); err != nil {
		return err
	}
	return tx.Commit()
}

type ImportOutcome string

const (
	ImportImported           ImportOutcome = "imported"
	ImportSkippedLocal       ImportOutcome = "skipped_local"
	ImportReplaced           ImportOutcome = "replaced"
	ImportIgnoredNonBaseline ImportOutcome = "ignored_non_baseline"
)

func (s *Store) ImportFingerprint(ctx context.Context, fingerprint *model.Fingerprint) (ImportOutcome, error) {
	if !fingerprint.IsBaseline {
		return ImportIgnoredNonBaseline, nil
	}
	existing, err := s.GetFingerprint(ctx, fingerprint.Service, fingerprint.Package, fingerprint.Version)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.Origin == model.OriginLocal {
		return ImportSkippedLocal, nil
	}
	behaviorsJSON, err := json.Marshal(fingerprint.Behaviors)
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
		fingerprint.Service, fingerprint.Package, fingerprint.Version, string(behaviorsJSON),
		fingerprint.FirstSeen, fingerprint.LastSeen, fingerprint.ObsCount,
		model.OriginImported, model.OriginImported)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return ImportReplaced, nil
	}
	return ImportImported, nil
}

func (s *Store) ListBaselines(ctx context.Context) ([]model.Fingerprint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin
		FROM fingerprints WHERE is_baseline=TRUE
		ORDER BY service, package, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFingerprints(rows)
}

func (s *Store) ListFingerprints(ctx context.Context, service, pkg string) ([]model.Fingerprint, error) {
	query := `SELECT service, package, version, behaviors, first_seen, last_seen, obs_count, is_baseline, origin
	          FROM fingerprints WHERE 1=1`
	var args []any
	if service != "" {
		args = append(args, service)
		query += fmt.Sprintf(" AND service=$%d", len(args))
	}
	if pkg != "" {
		args = append(args, pkg)
		query += fmt.Sprintf(" AND package=$%d", len(args))
	}
	query += " ORDER BY service, package, version"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFingerprints(rows)
}

func scanFingerprints(rows *sql.Rows) ([]model.Fingerprint, error) {
	var out []model.Fingerprint
	for rows.Next() {
		var fingerprint model.Fingerprint
		var behaviorsJSON []byte
		if err := rows.Scan(&fingerprint.Service, &fingerprint.Package, &fingerprint.Version, &behaviorsJSON,
			&fingerprint.FirstSeen, &fingerprint.LastSeen, &fingerprint.ObsCount,
			&fingerprint.IsBaseline, &fingerprint.Origin); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(behaviorsJSON, &fingerprint.Behaviors); err != nil {
			return nil, err
		}
		fingerprint.Origin = normalizeOrigin(fingerprint.Origin)
		out = append(out, fingerprint)
	}
	return out, rows.Err()
}

func (s *Store) LatestBaseline(ctx context.Context, service, pkg, excludeVersion string) (*model.Fingerprint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT version, behaviors, first_seen, last_seen, obs_count, origin
		 FROM fingerprints
		 WHERE service=$1 AND package=$2 AND version<>$3 AND is_baseline=TRUE
		 ORDER BY last_seen DESC LIMIT 1`,
		service, pkg, excludeVersion)
	fingerprint := model.Fingerprint{Service: service, Package: pkg, IsBaseline: true}
	var behaviorsJSON []byte
	err := row.Scan(&fingerprint.Version, &behaviorsJSON, &fingerprint.FirstSeen,
		&fingerprint.LastSeen, &fingerprint.ObsCount, &fingerprint.Origin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(behaviorsJSON, &fingerprint.Behaviors); err != nil {
		return nil, err
	}
	fingerprint.Origin = normalizeOrigin(fingerprint.Origin)
	return &fingerprint, nil
}
