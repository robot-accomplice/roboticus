package admin

import (
	"context"
	"testing"

	"roboticus/testutil"
)

func TestRepairOrphanReactTracesDeletesOnlyOrphans(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	if _, err := store.ExecContext(ctx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json)
		 VALUES ('pt-live', 'turn-live', 'sess-live', 'api', 1, '[]')`); err != nil {
		t.Fatalf("insert pipeline trace: %v", err)
	}
	if _, err := store.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO react_traces (id, pipeline_trace_id, react_json)
		 VALUES ('rt-live', 'pt-live', '{}'), ('rt-orphan', 'pt-missing', '{}')`); err != nil {
		t.Fatalf("insert react traces: %v", err)
	}
	if _, err := store.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	deleted, err := repairOrphanReactTraces(ctx, store)
	if err != nil {
		t.Fatalf("repairOrphanReactTraces: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	var count int
	if err := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM react_traces WHERE id = 'rt-live'`).Scan(&count); err != nil {
		t.Fatalf("count live trace: %v", err)
	}
	if count != 1 {
		t.Fatalf("live trace count = %d, want 1", count)
	}

	deleted, err = repairOrphanReactTraces(ctx, store)
	if err != nil {
		t.Fatalf("second repairOrphanReactTraces: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("second deleted = %d, want idempotent 0", deleted)
	}
}

func TestRepairFalseCapabilityDenialMemories(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	if _, err := store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES
		   ('ep-bad', 'episode_summary', 'I cannot use the Playwright tool or browse web pages.', 8, 'active'),
		   ('ep-good', 'episode_summary', 'I used the Playwright browser tool successfully.', 8, 'active')`); err != nil {
		t.Fatalf("insert episodic memory: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state)
		 VALUES
		   ('sem-bad', 'knowledge', 'I do not have capability', 'I do not have the capability to use Playwright or browse web pages.', 0.8, 'active'),
		   ('sem-good', 'knowledge', 'Playwright capability', 'Playwright browser tools are available when registered.', 0.8, 'active')`); err != nil {
		t.Fatalf("insert semantic memory: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO knowledge_facts (id, subject, relation, object, confidence)
		 VALUES
		   ('fact-bad', 'I cannot use capability', 'uses', 'Playwright browser tool', 0.75),
		   ('fact-good', 'Playwright MCP', 'uses', 'browser tools', 0.75)`); err != nil {
		t.Fatalf("insert knowledge facts: %v", err)
	}

	staled, deletedFacts, err := repairFalseCapabilityDenialMemories(ctx, store)
	if err != nil {
		t.Fatalf("repairFalseCapabilityDenialMemories: %v", err)
	}
	if staled != 2 {
		t.Fatalf("staled = %d, want 2", staled)
	}
	if deletedFacts != 1 {
		t.Fatalf("deletedFacts = %d, want 1", deletedFacts)
	}

	var state string
	if err := store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep-good'`).Scan(&state); err != nil {
		t.Fatalf("query good episodic: %v", err)
	}
	if state != "active" {
		t.Fatalf("good episodic state = %q, want active", state)
	}
	if err := store.QueryRowContext(ctx, `SELECT memory_state FROM semantic_memory WHERE id = 'sem-good'`).Scan(&state); err != nil {
		t.Fatalf("query good semantic: %v", err)
	}
	if state != "active" {
		t.Fatalf("good semantic state = %q, want active", state)
	}

	staled, deletedFacts, err = repairFalseCapabilityDenialMemories(ctx, store)
	if err != nil {
		t.Fatalf("second repairFalseCapabilityDenialMemories: %v", err)
	}
	if staled != 0 || deletedFacts != 0 {
		t.Fatalf("second repair = (%d, %d), want idempotent zeroes", staled, deletedFacts)
	}
}

func TestRepairInactiveMemoryDerivedRows(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	if _, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state)
		 VALUES
		   ('sem-stale', 'capability', 'playwright-denial', 'I cannot use Playwright.', 0.9, 'stale'),
		   ('sem-active', 'capability', 'playwright-available', 'Playwright browser tools are available.', 0.9, 'active')`); err != nil {
		t.Fatalf("insert semantic memory: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES
		   ('ep-stale', 'conversation', 'I cannot browse web pages.', 8, 'stale'),
		   ('ep-promoted', 'conversation', 'Used browser_navigate successfully.', 8, 'promoted')`); err != nil {
		t.Fatalf("insert episodic memory: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES
		   ('idx-sem-stale', 'semantic_memory', 'sem-stale', 'I cannot use Playwright.', 'capability', 0.9),
		   ('idx-sem-active', 'semantic_memory', 'sem-active', 'Playwright browser tools are available.', 'capability', 0.9),
		   ('idx-ep-stale', 'episodic_memory', 'ep-stale', 'I cannot browse web pages.', 'conversation', 0.9),
		   ('idx-ep-promoted', 'episodic_memory', 'ep-promoted', 'Used browser_navigate successfully.', 'conversation', 0.9),
		   ('idx-sem-missing', 'semantic_memory', 'sem-missing', 'Missing semantic row should not remain.', 'capability', 0.9),
		   ('idx-ep-missing', 'episodic_memory', 'ep-missing', 'Missing episodic row should not remain.', 'conversation', 0.9)`); err != nil {
		t.Fatalf("insert memory index: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO memory_fts (content, source_table, source_id, category)
		 VALUES
		   ('Missing semantic row should not remain.', 'semantic_memory', 'sem-missing', 'capability'),
		   ('Missing episodic row should not remain.', 'episodic_memory', 'ep-missing', 'conversation')`); err != nil {
		t.Fatalf("insert manual fts rows: %v", err)
	}

	indexRows, ftsRows, err := repairInactiveMemoryDerivedRows(ctx, store)
	if err != nil {
		t.Fatalf("repairInactiveMemoryDerivedRows: %v", err)
	}
	if indexRows != 4 {
		t.Fatalf("indexRows = %d, want 4", indexRows)
	}
	if ftsRows != 4 {
		t.Fatalf("ftsRows = %d, want 4", ftsRows)
	}

	var count int
	if err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_index
		  WHERE source_id IN ('sem-active', 'ep-promoted')`).Scan(&count); err != nil {
		t.Fatalf("count surviving index rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("surviving active/promoted index rows = %d, want 2", count)
	}
	if err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_fts
		  WHERE source_id IN ('sem-active', 'ep-promoted')`).Scan(&count); err != nil {
		t.Fatalf("count surviving fts rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("surviving active/promoted fts rows = %d, want 2", count)
	}

	indexRows, ftsRows, err = repairInactiveMemoryDerivedRows(ctx, store)
	if err != nil {
		t.Fatalf("second repairInactiveMemoryDerivedRows: %v", err)
	}
	if indexRows != 0 || ftsRows != 0 {
		t.Fatalf("second repair = (%d, %d), want idempotent zeroes", indexRows, ftsRows)
	}
}
