package store

import (
	"context"
)

// Advisory lock keys for singleton background work on Postgres. SQLite
// single-replica deployments ignore these — WithLeader always runs fn inline.
const (
	LockRetention    int64 = 1
	LockReachability int64 = 2
	LockDigest       int64 = 3
)

// Dialect returns "postgres" or "sqlite".
func (s *Store) Dialect() string { return s.dialect }

// WithLeader runs fn when this replica holds the Postgres advisory lock for
// lockKey. On SQLite (single writer) fn always runs. When the lock is held by
// another session, returns nil without calling fn — callers should retry on
// the next tick.
func (s *Store) WithLeader(ctx context.Context, lockKey int64, fn func(context.Context) error) error {
	if s.dialect != "postgres" {
		return fn(ctx)
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&acquired); err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockKey)
	}()
	return fn(ctx)
}
