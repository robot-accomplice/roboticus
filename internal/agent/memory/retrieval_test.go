package memory

import (
	"context"
	"strings"
	"testing"

	"goboticus/testutil"
)

func TestRetrieve_NilStore(t *testing.T) {
	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), nil)
	result := mr.Retrieve(context.Background(), "sess1", "query", 1000)
	if result != "" {
		t.Errorf("nil store should return empty, got %q", result)
	}
}

func TestRetrieve_EmptyStore(t *testing.T) {
	store := testutil.TempStore(t)
	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(context.Background(), "sess1", "query", 1000)
	// Empty store should return empty or just section headers.
	if strings.Contains(result, "error") {
		t.Errorf("empty store should not error, got %q", result)
	}
}

func TestRetrieve_WorkingMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sessionID := testutil.SeedSession(t, store, "agent1", "api")
	testutil.SeedWorkingMemory(t, store, sessionID, []string{
		"User is debugging a Go program",
		"User prefers concise responses",
	})

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, sessionID, "help me debug", 2000)

	if !strings.Contains(result, "Working Memory") {
		t.Error("result should contain Working Memory section")
	}
	if !strings.Contains(result, "debugging") {
		t.Error("result should contain seeded working memory content")
	}
}

func TestRetrieve_EpisodicMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedEpisodicMemory(t, store, []string{
		"User asked about Go concurrency patterns",
		"Agent used the read_file tool successfully",
	})

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, "", "concurrency", 2000)

	if !strings.Contains(result, "Relevant Memories") {
		t.Error("result should contain Relevant Memories section")
	}
}

func TestRetrieve_SemanticMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedSemanticMemory(t, store, "programming", "Go channels", "Used for goroutine communication")

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, "", "channels", 2000)

	if !strings.Contains(result, "Knowledge") {
		t.Error("result should contain Knowledge section")
	}
	if !strings.Contains(result, "goroutine") {
		t.Error("result should contain seeded semantic memory")
	}
}

func TestRetrieve_ProceduralMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedProceduralMemory(t, store, "read_file", 45, 5)
	testutil.SeedProceduralMemory(t, store, "bash", 20, 10)

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, "", "", 2000)

	if !strings.Contains(result, "Tool Experience") {
		t.Error("result should contain Tool Experience section")
	}
	if !strings.Contains(result, "read_file") {
		t.Error("result should contain tool name")
	}
	if !strings.Contains(result, "90%") {
		t.Error("result should contain success rate (45/50 = 90%)")
	}
}

func TestRetrieve_BudgetEnforcement(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sessionID := testutil.SeedSession(t, store, "agent1", "api")
	// Seed many entries.
	for i := 0; i < 50; i++ {
		testutil.SeedWorkingMemory(t, store, sessionID, []string{
			"This is a moderately long working memory entry that takes up space in the context window. " +
				"It contains enough text to test budget enforcement across multiple entries.",
		})
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	// Very small budget — should truncate.
	result := mr.Retrieve(ctx, sessionID, "", 50) // 50 tokens ≈ 200 chars total
	if len(result) > 1000 {
		t.Errorf("result too long (%d chars) for 50 token budget", len(result))
	}
}

func TestRetrieve_ActiveMemoryWrapper(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sessionID := testutil.SeedSession(t, store, "agent1", "api")
	testutil.SeedWorkingMemory(t, store, sessionID, []string{"test entry"})

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, sessionID, "", 1000)

	if !strings.HasPrefix(result, "[Active Memory]") {
		t.Errorf("result should start with [Active Memory], got prefix: %q", result[:min(30, len(result))])
	}
}
