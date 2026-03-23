package db

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/rs/zerolog/log"

	"goboticus/internal/core"

	_ "modernc.org/sqlite"
)

// Store wraps a *sql.DB for SQLite access. Unlike the Rust version which uses
// Arc<Mutex<Connection>>, Go's *sql.DB is already safe for concurrent use
// and manages a connection pool internally.
type Store struct {
	db *sql.DB
}

// Open creates a new Store, configures SQLite pragmas (WAL, foreign keys),
// initializes the schema, and runs any pending migrations.
func Open(dbPath string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode%%3Dwal&_pragma=foreign_keys%%3Don&_pragma=auto_vacuum%%3Dincremental&_pragma=busy_timeout%%3D5000", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, core.WrapError(core.ErrDatabase, "failed to open database", err)
	}

	// SQLite performs best with a single writer. WAL mode allows concurrent
	// readers alongside the single writer.
	db.SetMaxOpenConns(4)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, core.WrapError(core.ErrDatabase, "failed to ping database", err)
	}

	s := &Store{db: db}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	if err := s.runMigrations(); err != nil {
		db.Close()
		return nil, err
	}

	log.Info().Str("path", dbPath).Msg("database opened")
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for use in queries.
func (s *Store) DB() *sql.DB {
	return s.db
}

// ExecContext executes a query without returning rows.
func (s *Store) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

// QueryContext executes a query that returns rows.
func (s *Store) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// QueryRowContext executes a query that returns at most one row.
func (s *Store) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

// InTx executes fn within a transaction. If fn returns an error, the
// transaction is rolled back; otherwise it is committed.
func (s *Store) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.WrapError(core.ErrDatabase, "failed to begin transaction", err)
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return core.WrapError(core.ErrDatabase, "failed to commit transaction", err)
	}
	return nil
}
