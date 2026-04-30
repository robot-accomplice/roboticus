package tools

import (
	"context"
	"strings"
	"testing"

	"roboticus/testutil"
)

// Regression: search_memories tool must exist and return results from FTS + LIKE.
// Before fix: no search_memories tool existed; model had no way to find topic-specific memories.

func TestMemorySearchTool_Name(t *testing.T) {
	tool := NewMemorySearchTool(nil)
	if tool.Name() != "search_memories" {
		t.Errorf("name = %q, want search_memories", tool.Name())
	}
}

func TestMemorySearchTool_NoStore(t *testing.T) {
	tool := NewMemorySearchTool(nil)
	result, err := tool.Execute(context.Background(), `{"query": "palm"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "memory store not available" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestMemorySearchTool_EmptyQuery(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewMemorySearchTool(store)
	result, err := tool.Execute(context.Background(), `{"query": ""}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "query is required" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestMemorySearchTool_FTSFindsEpisodicMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed episodic memory — FTS trigger should auto-populate memory_fts.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES ('ep-palm-1', 'project', 'Palm USD stablecoin discussion with team', 5, 'active')`)

	tool := NewMemorySearchTool(store)
	result, err := tool.Execute(ctx, `{"query": "palm"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(result.Output, "No memories found") {
		t.Errorf("should find palm memory via FTS, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Palm USD") {
		t.Errorf("result should contain Palm USD content, got: %s", result.Output)
	}
}

func TestMemorySearchTool_LIKEFallbackFindsRelationship(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed relationship memory (not in FTS — uses LIKE fallback).
	_, _ = store.ExecContext(ctx,
		`INSERT INTO relationship_memory (id, entity_id, entity_name, trust_score, interaction_summary, interaction_count)
		 VALUES ('rel-1', 'palm-corp', 'Palm Corporation', 0.7, 'Ongoing contract negotiation', 5)`)

	tool := NewMemorySearchTool(store)
	result, err := tool.Execute(ctx, `{"query": "Palm"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(result.Output, "No memories found") {
		t.Errorf("should find Palm Corporation via LIKE, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Palm Corporation") {
		t.Errorf("result should contain entity name, got: %s", result.Output)
	}
}

func TestMemorySearchTool_FindsKnowledgeFacts(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, _ = store.ExecContext(ctx,
		`INSERT INTO knowledge_facts (id, subject, relation, object, confidence)
		 VALUES ('fact-ledger', 'Billing Service', 'depends_on', 'Ledger Service', 0.8)`)

	tool := NewMemorySearchTool(store)
	result, err := tool.Execute(ctx, `{"query": "Ledger"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(result.Output, "No memories found") {
		t.Fatalf("should find graph fact, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Billing Service depends_on Ledger Service") {
		t.Fatalf("expected graph fact in search results, got: %s", result.Output)
	}
}

func TestMemorySearchTool_NoResults(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewMemorySearchTool(store)
	result, err := tool.Execute(context.Background(), `{"query": "zzzznonexistent"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Output, "No memories found") {
		t.Errorf("should indicate no results, got: %s", result.Output)
	}
}

func TestMemorySearchTool_ConfidenceReinforce(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed episodic memory + index entry at confidence 0.5.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES ('ep-test', 'test', 'Palm project timeline review', 5, 'active')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, confidence)
		 VALUES ('idx-ep-test', 'episodic_memory', 'ep-test', 'Palm project timeline', 0.5)`)

	tool := NewMemorySearchTool(store)
	_, _ = tool.Execute(ctx, `{"query": "palm"}`, nil)

	// Check confidence was reinforced by +0.1 (not reset to 1.0).
	var conf float64
	row := store.QueryRowContext(ctx, `SELECT confidence FROM memory_index WHERE id = 'idx-ep-test'`)
	if err := row.Scan(&conf); err != nil {
		t.Fatalf("scan confidence: %v", err)
	}
	if conf < 0.59 || conf > 0.61 {
		t.Errorf("confidence should be ~0.6 (was 0.5, +0.1), got %f", conf)
	}
}

// Regression: BuildMemoryIndex must accept an optional query for query-aware selection.
// Before fix: index was static — same 20 entries regardless of what user asked about.

func TestBuildMemoryIndex_QueryAware(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed a Palm-related index entry at moderate confidence.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state)
		 VALUES ('sem-palm', 'project', 'Palm USD', 'Palm USD project details and timeline', 0.7, 'active')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-palm', 'semantic_memory', 'sem-palm', 'Palm USD project details and timeline', 'project', 0.7)`)

	// Seed a higher-confidence unrelated entry.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state)
		 VALUES ('sem-other', 'general', 'unrelated', 'Unrelated high confidence entry', 0.95, 'active')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-other', 'semantic_memory', 'sem-other', 'Unrelated high confidence entry', 'general', 0.95)`)

	// Without query: Palm might not be in top results (lower confidence).
	noQuery := BuildMemoryIndex(ctx, store, 2)
	// With query: Palm should be in results.
	withQuery := BuildMemoryIndex(ctx, store, 2, "palm")

	if !strings.Contains(withQuery, "Palm USD") {
		t.Errorf("query-aware index should contain Palm entry, got: %s", withQuery)
	}
	// The no-query path should still work.
	if noQuery == "" {
		t.Error("no-query index should not be empty")
	}
}

func TestBuildMemoryIndex_ToolNoiseFiltered(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed tool-output noise entries.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-noise1', 'episodic', 'ep-1', 'bash: Thu Apr 9 22:36:42', '', 1.0)`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-noise2', 'episodic', 'ep-2', 'get_runtime_context: Agent: default', '', 1.0)`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-noise3', 'episodic', 'ep-3', 'search_files: no matches found', '', 1.0)`)

	// Seed one real entry.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state)
		 VALUES ('sem-1', 'programming', 'go-concurrency', 'Go concurrency patterns', 0.8, 'active')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-real', 'semantic_memory', 'sem-1', 'Go concurrency patterns', 'programming', 0.8)`)

	result := BuildMemoryIndex(ctx, store, 20)
	if strings.Contains(result, "bash:") {
		t.Error("index should filter tool noise: bash output")
	}
	if strings.Contains(result, "get_runtime_context:") {
		t.Error("index should filter tool noise: runtime context")
	}
	if strings.Contains(result, "search_files: no matches") {
		t.Error("index should filter tool noise: empty search")
	}
	if !strings.Contains(result, "Go concurrency") {
		t.Error("index should include real memory entries")
	}
}

func TestBuildMemoryIndex_IncludesSearchInstruction(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state)
		 VALUES ('sem-1', 'test', 'test-entry', 'Test entry', 0.8, 'active')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-1', 'semantic_memory', 'sem-1', 'Test entry', '', 0.8)`)

	result := BuildMemoryIndex(ctx, store, 20)
	if !strings.Contains(result, "search_memories") {
		t.Error("index should mention search_memories tool")
	}
	if !strings.Contains(result, "recall_memory") {
		t.Error("index should mention recall_memory tool")
	}
}

// Regression: recall_memory confidence reinforce must use +0.1, not reset to 1.0.
// Before fix: confidence = 1.0 on every recall, causing all entries to pile up at max.

func TestMemoryRecallTool_ConfidenceIncrementalReinforce(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed episodic memory + index entry at confidence 0.5.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance)
		 VALUES ('ep-1', 'test', 'Test content', 5)`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, confidence)
		 VALUES ('idx-ep-1', 'episodic_memory', 'ep-1', 'Test', 0.5)`)

	tool := NewMemoryRecallTool(store)
	_, _ = tool.Execute(ctx, `{"memory_id": "ep-1", "source_table": "episodic_memory"}`, nil)

	var conf float64
	row := store.QueryRowContext(ctx, `SELECT confidence FROM memory_index WHERE source_table = 'episodic_memory' AND source_id = 'ep-1'`)
	if err := row.Scan(&conf); err != nil {
		t.Fatalf("scan: %v", err)
	}
	// Should be 0.6 (+0.1), NOT 1.0.
	if conf > 0.61 {
		t.Errorf("confidence should be 0.6 (incremental +0.1), got %f — regression: was resetting to 1.0", conf)
	}
	if conf < 0.59 {
		t.Errorf("confidence should be at least 0.6, got %f", conf)
	}
}
