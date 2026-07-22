package store

import (
	"context"
	"time"
)

func (s *Store) GetEnforceState(ctx context.Context) (enabled bool, rev int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT enabled, rev FROM enforce_state WHERE id=1`).Scan(&enabled, &rev)
	return enabled, rev, err
}

func (s *Store) SetEnforceEnabled(ctx context.Context, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE enforce_state SET enabled=$1, updated_at=$2 WHERE id=1`,
		enabled, time.Now().UnixNano())
	return err
}

func (s *Store) SetEnforceRev(ctx context.Context, rev int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE enforce_state SET rev=$1 WHERE id=1`, rev)
	return err
}
