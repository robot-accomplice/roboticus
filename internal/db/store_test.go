package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"roboticus/internal/core"
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
		"turn_diagnostics", "turn_diagnostic_events", "baseline_runs", "model_policies",
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

func TestOpen_RepairsLegacyTreasuryStateShape(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	rawDB, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open legacy db: %v", err)
	}
	_, err = rawDB.Exec(`
CREATE TABLE schema_version (
	version INTEGER NOT NULL,
	applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO schema_version (version) VALUES (30);
CREATE TABLE treasury_state (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	usdc_balance REAL NOT NULL DEFAULT 0.0,
	native_balance REAL NOT NULL DEFAULT 0.0
);`)
	if err != nil {
		_ = rawDB.Close()
		t.Fatalf("seed legacy db: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	defer func() { _ = store.Close() }()

	requiredColumns := []string{
		"atoken_balance",
		"survival_tier",
		"updated_at",
		"last_deposit_at",
		"last_withdrawal_at",
	}
	for _, column := range requiredColumns {
		exists, err := store.columnExists("treasury_state", column)
		if err != nil {
			t.Fatalf("columnExists(treasury_state.%s): %v", column, err)
		}
		if !exists {
			t.Fatalf("treasury_state missing repaired column %q", column)
		}
	}

	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO wallet_balances (symbol, name, balance, contract, decimals, is_native, updated_at)
		 VALUES ('USDC', 'USD Coin', 11.5, '', 6, 0, datetime('now')),
		        ('ETH', 'Ethereum', 2.0, '', 18, 1, datetime('now')),
		        ('aUSDC', 'Aave USDC', 3.25, '', 6, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed wallet_balances: %v", err)
	}

	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO treasury_state (id, usdc_balance, native_balance, atoken_balance, survival_tier, updated_at)
		 VALUES (1, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
		   usdc_balance = excluded.usdc_balance,
		   native_balance = excluded.native_balance,
		   atoken_balance = excluded.atoken_balance,
		   survival_tier = excluded.survival_tier,
		   updated_at = datetime('now')`,
		11.5, 2.0, 3.25, core.SurvivalTierStable.String(),
	); err != nil {
		t.Fatalf("write repaired treasury_state: %v", err)
	}

	var usdc, native, atoken float64
	var tier string
	if err := store.QueryRowContext(context.Background(),
		`SELECT usdc_balance, native_balance, atoken_balance, survival_tier FROM treasury_state WHERE id = 1`,
	).Scan(&usdc, &native, &atoken, &tier); err != nil {
		t.Fatalf("query repaired treasury_state: %v", err)
	}
	if usdc != 11.5 || native != 2.0 || atoken != 3.25 {
		t.Fatalf("unexpected repaired treasury balances: usdc=%v native=%v atoken=%v", usdc, native, atoken)
	}
	if tier != core.SurvivalTierStable.String() {
		t.Fatalf("survival_tier = %q, want %q", tier, core.SurvivalTierStable.String())
	}
}

func TestIsSubagentName(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if _, err := store.ExecContext(ctx,
		`INSERT INTO sub_agents (id, name, model, role, enabled) VALUES ('sa-1', 'automation_scripting', 'auto', 'subagent', 1)`); err != nil {
		t.Fatalf("seed sub_agents: %v", err)
	}

	got, err := IsSubagentName(ctx, store, "automation_scripting")
	if err != nil {
		t.Fatalf("IsSubagentName(subagent): %v", err)
	}
	if !got {
		t.Fatal("expected registered subagent name to resolve true")
	}

	got, err = IsSubagentName(ctx, store, "default")
	if err != nil {
		t.Fatalf("IsSubagentName(orchestrator): %v", err)
	}
	if got {
		t.Fatal("expected non-subagent name to resolve false")
	}
}
