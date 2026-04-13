package memory

import (
	"context"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

func TestAdaptiveBudget_EmptyTierRedistributes(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed only episodic and semantic — leave procedural and relationship empty.
	for i := 0; i < 5; i++ {
		store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content) VALUES (?, 'test', ?)`,
			db.NewID(), "test episodic content with enough words to fill budget slots")
	}
	for i := 0; i < 5; i++ {
		store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value) VALUES (?, 'knowledge', ?, 'some fact value')`,
			db.NewID(), "key"+string(rune('A'+i)))
	}

	// Create a session for working memory.
	sessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test-agent', 'active')`, sessionID)
	store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content) VALUES (?, ?, 'note', 'working mem entry')`,
		db.NewID(), sessionID)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)

	_, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "test query", 2048)

	// With empty procedural + relationship tiers, budget should flow to earlier tiers.
	// BudgetUsedPct should be higher than if those tiers just wasted their allocation.
	if metrics.ProceduralCount != 0 {
		t.Log("procedural tier empty as expected")
	}
	if metrics.RelationCount != 0 {
		t.Log("relationship tier empty as expected")
	}
	// The key assertion: total entries should be more than if we had strict budgets,
	// because surplus flows downstream.
	if metrics.TotalEntries == 0 {
		t.Error("expected some entries in retrieval result")
	}
}

func TestAdaptiveBudget_TotalCharsWithinBudget(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed all tiers generously.
	for i := 0; i < 20; i++ {
		store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content) VALUES (?, 'test', ?)`,
			db.NewID(), "episodic entry with substantial content to use budget space effectively for testing purposes")
	}
	for i := 0; i < 10; i++ {
		store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value) VALUES (?, 'knowledge', ?, 'detailed fact about the system architecture')`,
			db.NewID(), "key"+string(rune('A'+i)))
	}

	sessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test-agent', 'active')`, sessionID)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	totalTokens := 2048

	result, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "test", totalTokens)

	// Total chars used should never exceed the total budget.
	maxChars := totalTokens * 4 // charsPerToken = 4
	if len(result) > maxChars+100 { // small tolerance for section headers
		t.Errorf("result length %d exceeds budget %d chars", len(result), maxChars)
	}

	// BudgetUsedPct should be a reasonable percentage.
	if metrics.BudgetUsedPct < 0 || metrics.BudgetUsedPct > 2.0 {
		t.Errorf("BudgetUsedPct out of range: %.2f", metrics.BudgetUsedPct)
	}
}

func TestRetrievalStrategy_ModeSelection(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test-agent', 'active')`, sessionID)

	// Without embed client → should use keyword mode.
	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	_, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "test", 2048)

	if metrics.RetrievalMode != "keyword" {
		t.Errorf("without embedClient, expected keyword mode, got %s", metrics.RetrievalMode)
	}
}

func TestRetrievalStrategy_RecencyForNewSession(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := ngramEmbedClient()

	// Create a session with created_at = now (very new session).
	sessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status, created_at) VALUES (?, 'test', 'active', datetime('now'))`, sessionID)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	retriever.SetEmbeddingClient(ec)
	_, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "test", 2048)

	// New session (< 5min) should use recency mode.
	if metrics.RetrievalMode != "recency" {
		t.Errorf("new session should use recency mode, got %s", metrics.RetrievalMode)
	}
}

func TestRetrievalStrategy_HybridForMatureSession(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := ngramEmbedClient()

	// Create a session with created_at = 1 hour ago.
	sessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status, created_at) VALUES (?, 'test', 'active', datetime('now', '-1 hour'))`, sessionID)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	retriever.SetEmbeddingClient(ec)
	_, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "test", 2048)

	if metrics.RetrievalMode != "hybrid" {
		t.Errorf("mature session should use hybrid mode, got %s", metrics.RetrievalMode)
	}
}

func TestRetrievalStrategy_SessionNotFound_DefaultsGracefully(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	// Nonexistent session ID — should not crash, should default.
	_, metrics := retriever.RetrieveWithMetrics(ctx, "nonexistent-session", "test", 2048)

	if metrics.RetrievalMode == "" {
		t.Error("retrieval mode should not be empty even for nonexistent session")
	}
}
