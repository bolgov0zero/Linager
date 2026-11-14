// Package store is the PostgreSQL access layer of the Linager server.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/bolgov0zero/linager/migrations"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps a connection pool.
type Store struct{ Pool *pgxpool.Pool }

// Open connects to PostgreSQL and applies all pending migrations (forward only).
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	s := &Store{Pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return s, nil
}

// Close releases the pool.
func (s *Store) Close() { s.Pool.Close() }

func (s *Store) migrate(ctx context.Context) error {
	db := stdlib.OpenDBFromPool(s.Pool)
	defer func() { _ = db.Close() }()
	return migrateUp(ctx, db)
}

func migrateUp(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	// Serialize concurrent starters with an advisory lock.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(727274)"); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(727274)") }()
	return goose.UpContext(ctx, db, ".")
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
