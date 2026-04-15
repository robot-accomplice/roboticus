package db

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/rs/zerolog/log"

	"roboticus/internal/core"

	_ "modernc.org/sqlite"
)

// Store wraps a *sql.DB for SQLite access. Unlike the Rust version which uses
// Arc<Mutex<Connection>>, Go's *sql.DB is already safe for concurrent use
// and manages a connection pool internally.
type Store struct {
	db                *sql.DB
	onSessionArchived []func(ctx context.Context, sessionID string)
}

// OnSessionArchived registers a callback that fires after a session is archived.
// Used to trigger post-archival operations like memory summary promotion.
func (s *Store) OnSessionArchived(fn func(ctx context.Context, sessionID string)) {
	s.onSessionArchived = append(s.onSessionArchived, fn)
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
		_ = db.Close()
		return nil, core.WrapError(core.ErrDatabase, "failed to ping database", err)
	}

	s := &Store{db: db}

	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := s.runMigrations(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := s.ensureOptionalColumns(); err != nil {
		_ = db.Close()
		return nil, err
	}

	log.Info().Str("path", dbPath).Msg("database opened")
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Stats returns database connection pool statistics.
func (s *Store) Stats() sql.DBStats {
	return s.db.Stats()
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

// Ping verifies the database connection is alive.
func (s *Store) Ping() error {
	return s.db.Ping()
}

// TruncateAllData deletes all rows from every data table, preserving schema_version.
func (s *Store) TruncateAllData() error {
	tables := []string{
		"session_messages", "turns", "tool_calls", "policy_decisions",
		"working_memory", "episodic_memory", "semantic_memory",
		"procedural_memory", "relationship_memory", "knowledge_facts",
		"tasks", "cron_runs", "cron_jobs",
		"transactions", "service_requests", "revenue_opportunities", "revenue_feedback",
		"inference_costs", "semantic_cache",
		"identity", "os_personality_history", "metric_snapshots",
		"discovered_agents", "paired_devices", "skills", "delivery_queue",
		"approval_requests", "plugins", "embeddings", "sub_agents",
		"context_checkpoints", "hippocampus", "turn_feedback",
		"context_snapshots", "model_selection_events", "shadow_routing_predictions",
		"abuse_events", "learned_skills", "memory_index", "consolidation_log",
		"hygiene_log", "pipeline_traces", "react_traces",
		"heartbeat_task_results", "delegation_outcomes",
		"agent_tasks", "task_steps", "task_events", "agent_delegation_outcomes",
		"sessions",
	}

	return s.InTx(context.Background(), func(tx *sql.Tx) error {
		// Rebuild FTS index.
		if _, err := tx.Exec("DELETE FROM memory_fts"); err != nil {
			// FTS table may not exist in all configurations; ignore.
			_ = err
		}

		for _, t := range tables {
			if _, err := tx.Exec("DELETE FROM " + t); err != nil {
				return core.WrapError(core.ErrDatabase, fmt.Sprintf("failed to truncate %s", t), err)
			}
		}
		return nil
	})
}

// InTx executes fn within a transaction. If fn returns an error, the
// transaction is rolled back; otherwise it is committed.
func (s *Store) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.WrapError(core.ErrDatabase, "failed to begin transaction", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return core.WrapError(core.ErrDatabase, "failed to commit transaction", err)
	}
	return nil
}
