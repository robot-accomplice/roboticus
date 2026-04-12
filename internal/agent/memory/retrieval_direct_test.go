package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"roboticus/testutil"
)

// Regression: RetrieveDirectOnly must return ONLY working + ambient, not all tiers.
// Before fix: full 5-tier memory dump was injected, causing model to treat it as
// complete memory and confabulate when topics weren't present.

func TestRetrieveDirectOnly_OnlyWorkingAndAmbient(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sessionID := testutil.SeedSession(t, store, "agent1", "api")
	testutil.SeedWorkingMemory(t, store, sessionID, []string{"current task context"})
	testutil.SeedEpisodicMemory(t, store, []string{"old episodic event"})
	testutil.SeedSemanticMemory(t, store, "knowledge", "Go", "compiled language")
	testutil.SeedProceduralMemory(t, store, "bash", 10, 2)

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.RetrieveDirectOnly(ctx, sessionID, "test query", 2000)

	// Should contain working memory.
	if !strings.Contains(result, "Working Memory") {
		t.Error("should contain Working Memory section")
	}

	// Should NOT contain episodic/semantic/procedural/relationship sections.
	if strings.Contains(result, "Relevant Memories") {
		t.Error("should NOT contain Relevant Memories — that's index-only")
	}
	if strings.Contains(result, "Knowledge") {
		t.Error("should NOT contain Knowledge — that's index-only")
	}
	if strings.Contains(result, "Tool Experience") {
		t.Error("should NOT contain Tool Experience — that's index-only")
	}
	if strings.Contains(result, "Relationships") {
		t.Error("should NOT contain Relationships — that's index-only")
	}
}

func TestRetrieveDirectOnly_EmptyStore(t *testing.T) {
	store := testutil.TempStore(t)
	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.RetrieveDirectOnly(context.Background(), "nonexistent", "query", 2000)
	if result != "" {
		t.Errorf("empty store should return empty, got %q", result)
	}
}

func TestRetrieveDirectOnly_NilStore(t *testing.T) {
	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), nil)
	result := mr.RetrieveDirectOnly(context.Background(), "sess", "query", 2000)
	if result != "" {
		t.Errorf("nil store should return empty, got %q", result)
	}
}

// Regression: episodic retrieval must use FTS5 MATCH, not just recency.
// Before fix: FTS5 join existed but MATCH clause was missing — all queries
// returned the 30 most recent memories regardless of content.

func TestRetrieveEpisodic_FTSUnionStrategy(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed an old memory about "palm" (would be missed by recency-only).
	_, _ = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state, created_at)
		 VALUES ('ep-palm-old', 'project', 'Palm USD stablecoin architecture review', 5, 'active',
		         datetime('now', '-180 days'))`)

	// Seed recent unrelated memories to fill recency slots.
	for i := 0; i < 25; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
			 VALUES (?, 'general', 'Unrelated recent activity number ' || ?, 3, 'active')`,
			fmt.Sprintf("ep-filler-%d", i), i)
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	// Full retrieval (not direct-only) should find the old palm memory via FTS.
	result := mr.Retrieve(ctx, "", "palm", 4000)

	if !strings.Contains(result, "Palm USD") {
		t.Error("FTS union should find old 'Palm USD' memory despite recency bias — regression: FTS MATCH was missing")
	}
}
