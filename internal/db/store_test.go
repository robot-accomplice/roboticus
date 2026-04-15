package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestOpenAndClose(t *testing.T) {
	store := testStore(t)

	// Verify we can ping the database.
	if err := store.Ping(); err != nil {
		t.Errorf("Ping() error: %v", err)
	}
}

func TestSchemaVersion(t *testing.T) {
	store := testStore(t)

	var version int
	err := store.QueryRowContext(context.Background(),
		"SELECT MAX(version) FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatalf("failed to query schema_version: %v", err)
	}

	if version < embeddedSchemaVersion {
		t.Errorf("schema version = %d, want >= %d", version, embeddedSchemaVersion)
	}
}

func TestTablesExist(t *testing.T) {
	store := testStore(t)

	expectedTables := []string{
		"sessions", "session_messages", "turns", "tool_calls",
		"policy_decisions", "working_memory", "episodic_memory",
		"semantic_memory", "procedural_memory", "relationship_memory",
		"knowledge_facts",
		"cron_jobs", "cron_runs", "transactions", "inference_costs",
		"semantic_cache", "delivery_queue", "approval_requests",
		"plugins", "embeddings", "sub_agents", "skills",
		"abuse_events", "learned_skills", "hygiene_log",
	}

	for _, table := range expectedTables {
		var name string
		err := store.QueryRowContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q should exist: %v", table, err)
		}
	}
}

func TestInTx(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Verify a successful transaction commits.
	err := store.InTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO identity (key, value) VALUES ('test_key', 'test_value')")
		return err
	})
	if err != nil {
		t.Fatalf("InTx() error: %v", err)
	}

	// Verify the row was committed.
	var val string
	err = store.QueryRowContext(ctx, "SELECT value FROM identity WHERE key = 'test_key'").Scan(&val)
	if err != nil {
		t.Fatalf("row should exist after commit: %v", err)
	}
	if val != "test_value" {
		t.Errorf("value = %q, want %q", val, "test_value")
	}
}
