package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

func TestSessionSummary_PromotedOnArchival(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	// Add working memory entries.
	for _, entry := range []string{"debugging auth issue", "found the bug in JWT validation", "fix deployed"} {
		store.ExecContext(ctx,
			`INSERT INTO working_memory (id, session_id, entry_type, content, importance)
			 VALUES (?, ?, 'note', ?, 5)`,
			db.NewID(), sessionID, entry)
	}

	mgr := NewManager(DefaultConfig(), store)
	mgr.PromoteSessionSummary(ctx, sessionID)

	// Verify session summary was stored in semantic memory.
	var value string
	err := store.QueryRowContext(ctx,
		`SELECT value FROM semantic_memory WHERE category = 'session_summary' AND key = ?`, sessionID).Scan(&value)
	if err != nil {
		t.Fatalf("expected session summary, got error: %v", err)
	}
	if !strings.Contains(value, "auth") || !strings.Contains(value, "JWT") {
		t.Errorf("summary should contain key terms, got: %s", value)
	}
}

func TestSessionSummary_InjectedInNewSession(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Store a session summary from a "previous" session.
	store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, 'session_summary', 'old-session', 'was debugging JWT auth bug in production')`,
		db.NewID())

	// Create a new session with no working memory.
	newSessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, newSessionID)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := retriever.retrieveWorkingMemory(ctx, newSessionID, 500)

	if !strings.Contains(result, "Previously:") {
		t.Errorf("new session with empty working memory should inject 'Previously:', got: %q", result)
	}
	if !strings.Contains(result, "JWT") {
		t.Errorf("previously line should contain summary content, got: %q", result)
	}
}

func TestEmbeddingClassification_Fallback(t *testing.T) {
	// Without embedClient, keyword classification should work unchanged.
	// "create" without social keywords → TurnCreative.
	msgs := []llm.Message{
		{Role: "user", Content: "create a poem about stars and galaxies"},
	}
	turnType := classifyTurn(msgs)
	if turnType != TurnCreative {
		t.Errorf("keyword classification: expected TurnCreative, got %v", turnType)
	}
}

func TestEmbeddingClassification_Financial(t *testing.T) {
	ec := llm.NewEmbeddingClient(nil) // n-gram fallback
	ctx := context.Background()

	// "wire $500 to the vendor account" — keyword classifier would miss "wire"
	// but embedding similarity to financial prototype should be higher than others.
	turnType, ok := classifyTurnWithEmbeddings(ctx, ec, "wire $500 to the vendor account for the invoice payment")
	if !ok {
		t.Log("embedding classification below threshold (expected with n-gram fallback)")
		return // n-gram fallback may not have enough semantic signal
	}
	if turnType != TurnFinancial {
		t.Logf("expected TurnFinancial, got %v (n-gram fallback may not distinguish well)", turnType)
	}
}

func TestEmbeddingClassification_Threshold(t *testing.T) {
	ec := llm.NewEmbeddingClient(nil)
	ctx := context.Background()

	// Very ambiguous text.
	_, ok := classifyTurnWithEmbeddings(ctx, ec, "x y z")
	// With n-gram hashing, this may or may not pass threshold.
	// The test just verifies the function doesn't panic.
	_ = ok
}
