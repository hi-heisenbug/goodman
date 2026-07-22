// Package store persists fingerprints, alerts, reachability reports, and
// enforcement state. Production uses PostgreSQL; local runs use SQLite. One
// SQL codepath serves both because both drivers accept $N placeholders.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db      *sql.DB
	dialect string // "postgres" | "sqlite"
}

func Open(dsn string) (*Store, error) {
	driver, dialect, connStr := databaseConfig(dsn)
	if dialect == "sqlite" {
		connStr = sqliteConnectionString(connStr)
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
		_ = db.Close()
		return nil, fmt.Errorf("connect %s: %w", dialect, err)
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func databaseConfig(dsn string) (driver, dialect, connStr string) {
	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		return "pgx", "postgres", dsn
	case strings.HasPrefix(dsn, "sqlite://"):
		return "sqlite", "sqlite", strings.TrimPrefix(dsn, "sqlite://")
	default:
		return "sqlite", "sqlite", dsn
	}
}

func sqliteConnectionString(connStr string) string {
	separator := "?"
	if strings.Contains(connStr, "?") {
		separator = "&"
	}
	return connStr + separator + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

func (s *Store) Close() error { return s.db.Close() }

// Ping verifies the backing database is reachable (readiness probes).
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// migrate applies pending migration files in name order. Applied files are
// recorded in schema_migrations so non-idempotent statements run exactly once.
func (s *Store) migrate(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck
	unlock, err := s.lockMigrations(ctx, conn)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := conn.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	suffix := "." + s.dialect + ".sql"
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		if err := applyMigration(ctx, conn, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) lockMigrations(ctx context.Context, conn *sql.Conn) (func(), error) {
	if s.dialect != "postgres" {
		return func() {}, nil
	}
	const migrationLock int64 = 0x474f4f444d414e
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLock); err != nil {
		return nil, err
	}
	return func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLock)
	}, nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, name string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var applied int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE name=$1`, name).Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	sqlText, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(sqlText)); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (name, applied_at) VALUES ($1,$2) ON CONFLICT (name) DO NOTHING`,
		name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}
