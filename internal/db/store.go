package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
//
// File permissions: the database file holds conversation history, working
// memory contents (which can include credentials or PII the agent has
// observed), graph facts, and lives alongside other sensitive artifacts
// (wallet.enc, plugin keys). Default umask on macOS is 022, which would
// leave a freshly-created SQLite file at 0644 — world-readable by any
// other user account or process on the host. Open() explicitly tightens
// the file mode to 0600 (and the parent directory to 0700 if it sits
// inside a roboticus-owned tree) on every call. The chmod is best-effort:
// if the file is owned by another user (e.g., it was created under sudo
// in a previous session) the chmod will fail with EPERM. We log a
// warning rather than failing the open so the daemon can boot and the
// operator can fix ownership manually — but the warning makes the
// security-relevant condition impossible to miss.
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

	// Tighten permissions on the DB file and (best-effort) the parent
	// directory. Done AFTER schema init so we know SQLite has actually
	// created the file (sql.Open is lazy — the file may not exist until
	// the first statement runs).
	tightenDatabasePermissions(dbPath)

	log.Info().Str("path", dbPath).Msg("database opened")
	return s, nil
}

// tightenDatabasePermissions sets restrictive POSIX mode on the SQLite
// database file and its sidecar files (-wal and -shm, used by WAL mode).
// Best-effort: any chmod failure (e.g., file owned by another user from a
// previous sudo invocation) is logged as a warning rather than failing
// the daemon boot. The warning is loud enough that an operator can spot
// and remediate it without the daemon being blocked from running.
//
// In-memory databases (`:memory:` and the file::memory: variants) have
// no on-disk file to chmod and are skipped silently.
func tightenDatabasePermissions(dbPath string) {
	if dbPath == "" || dbPath == ":memory:" || dbPath == "file::memory:" {
		return
	}

	// Main DB file. WAL mode also creates `<path>-wal` and `<path>-shm`
	// alongside it; both can hold uncommitted page data and warrant the
	// same protection.
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(path); err != nil {
			// Sidecar files only exist while WAL is active; absence is normal.
			continue
		}
		if err := os.Chmod(path, 0o600); err != nil {
			log.Warn().
				Err(err).
				Str("path", path).
				Msg("could not tighten database file permissions; check file ownership (chown to current user)")
		}
	}

	// Parent directory: 0700 prevents directory traversal from other
	// users. We only tighten the immediate parent; we don't walk further
	// up because the user may have intentionally placed the dataDir
	// under a wider-permission tree (e.g., a shared workspace dir).
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." && dir != "/" {
		if err := os.Chmod(dir, 0o700); err != nil {
			// Directory chmod is informational only — failures are common
			// when the parent dir is owned by a different user (e.g.,
			// /tmp). Log at debug to avoid noise.
			log.Debug().
				Err(err).
				Str("dir", dir).
				Msg("could not tighten database parent directory permissions")
		}
	}
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
