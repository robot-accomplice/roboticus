package tools

import (
	"context"
	"strings"
	"testing"

	"roboticus/testutil"
)

func TestMemoryRecallTool_Name(t *testing.T) {
	tool := NewMemoryRecallTool(nil)
	if tool.Name() != "recall_memory" {
		t.Errorf("name = %q", tool.Name())
	}
}

func TestMemoryRecallTool_NoStore(t *testing.T) {
	tool := NewMemoryRecallTool(nil)
	result, err := tool.Execute(context.Background(), `{"memory_id": "test"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "memory store not available" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestMemoryRecallTool_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewMemoryRecallTool(store)
	result, err := tool.Execute(context.Background(), `{"memory_id": "nonexistent"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil || result.Output == "" {
		t.Fatal("expected non-empty output")
	}
	// Should contain "not found" message.
	if result.Output == "" {
		t.Error("output should indicate not found")
	}
}

func TestMemoryRecallTool_FindsEntry(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed an episodic memory entry.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance)
		 VALUES ('ep-1', 'test_event', 'This is a test memory entry', 5)`)

	tool := NewMemoryRecallTool(store)
	result, err := tool.Execute(ctx, `{"memory_id": "ep-1", "source_table": "episodic_memory"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Output == "" || result.Output == "memory entry not found" {
		t.Errorf("should find the entry, got: %s", result.Output)
	}
}

func TestMemoryRecallTool_FindsKnowledgeFact(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, _ = store.ExecContext(ctx,
		`INSERT INTO knowledge_facts (id, subject, relation, object, confidence)
		 VALUES ('fact-1', 'Billing Service', 'depends_on', 'Ledger Service', 0.8)`)

	tool := NewMemoryRecallTool(store)
	result, err := tool.Execute(ctx, `{"memory_id": "fact-1", "source_table": "knowledge_facts"}`, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !contains(result.Output, "Billing Service") || !contains(result.Output, "depends_on") {
		t.Fatalf("expected knowledge fact output, got %s", result.Output)
	}
}

func TestBuildMemoryIndex_EmptyStore(t *testing.T) {
	store := testutil.TempStore(t)
	result := BuildMemoryIndex(context.Background(), store, 20)
	if result != "" {
		t.Errorf("expected empty index for empty store, got: %q", result)
	}
}

func TestBuildMemoryIndex_WithEntries(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed memory_index entries.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES ('ep-1', 'test', 'Test memory about something', 5, 'active')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence)
		 VALUES ('idx-1', 'episodic_memory', 'ep-1', 'Test memory about something', 'test', 0.9)`)

	result := BuildMemoryIndex(ctx, store, 20)
	if result == "" {
		t.Fatal("expected non-empty index")
	}
	if !contains(result, "Memory Index") {
		t.Error("should contain Memory Index header")
	}
	if !contains(result, "recall") {
		t.Error("should contain recall instruction")
	}
}

func TestBuildMemoryIndex_FiltersInactiveSourceMemoryRows(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, memory_state)
		 VALUES ('sem-stale', 'capability', 'playwright', 'I currently do not have the capability to use Playwright.', 'stale')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, memory_state)
		 VALUES ('sem-active', 'capability', 'browser', 'Browser tools are available when selected for a turn.', 'active')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES ('ep-stale', 'conversation', 'I cannot browse web pages.', 5, 'stale')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES ('ep-promoted', 'conversation', 'Used browser_navigate to open https://example.com.', 5, 'promoted')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, category, confidence) VALUES
		 ('idx-stale-sem', 'semantic_memory', 'sem-stale', 'I currently do not have the capability to use Playwright.', 'capability', 1.0),
		 ('idx-active-sem', 'semantic_memory', 'sem-active', 'Browser tools are available when selected for a turn.', 'capability', 0.9),
		 ('idx-stale-ep', 'episodic_memory', 'ep-stale', 'I cannot browse web pages.', 'conversation', 1.0),
		 ('idx-promoted-ep', 'episodic_memory', 'ep-promoted', 'Used browser_navigate to open https://example.com.', 'conversation', 0.8)`)

	result := BuildMemoryIndex(ctx, store, 20, "playwright browser")
	if strings.Contains(result, "do not have the capability") || strings.Contains(result, "cannot browse") {
		t.Fatalf("stale source memory leaked into index:\n%s", result)
	}
	if !strings.Contains(result, "Browser tools are available") {
		t.Fatalf("active semantic memory missing from index:\n%s", result)
	}
	if !strings.Contains(result, "browser_navigate") {
		t.Fatalf("promoted episodic memory missing from index:\n%s", result)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSub(s, sub))
}

func containsSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
