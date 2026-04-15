package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
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

	// New structured format uses [Working State] for direct-injected active state.
	if !strings.Contains(result, "Working State") {
		t.Error("result should contain Working State section")
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

	// Episodic evidence now appears in [Retrieved Evidence] section.
	if !strings.Contains(result, "Retrieved Evidence") && !strings.Contains(result, "concurrency") {
		t.Error("result should contain retrieved episodic evidence")
	}
}

func TestRetrieve_SemanticMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedSemanticMemory(t, store, "programming", "Go channels", "Used for goroutine communication")

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, "", "channels", 2000)

	// Semantic evidence now appears in [Retrieved Evidence] section.
	if !strings.Contains(result, "goroutine") && !strings.Contains(result, "channels") {
		t.Error("result should contain seeded semantic memory content")
	}
}

func TestRetrieve_ProceduralMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedProceduralMemory(t, store, "read_file", 45, 5)
	testutil.SeedProceduralMemory(t, store, "bash", 20, 10)

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, "", "how to read files", 2000)

	// Procedural content appears in evidence or as formatted text.
	if !strings.Contains(result, "read_file") {
		t.Error("result should contain tool name from procedural memory")
	}
}

func TestRetrieveProceduralMemory_FallsBackWhenQueryFilterMisses(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedProceduralMemory(t, store, "read_file", 45, 5)
	testutil.SeedProceduralMemory(t, store, "bash", 20, 10)

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.retrieveProceduralMemory(ctx, "how to read files", RetrievalKeyword, 200)

	if !strings.Contains(result, "read_file") {
		t.Fatalf("expected fallback procedural retrieval to include read_file, got %q", result)
	}
}

func TestRetrieveRelationshipMemory_FallsBackWhenQueryFilterMisses(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, err := store.ExecContext(ctx,
		`INSERT INTO relationship_memory
		 (id, entity_id, entity_name, trust_score, interaction_summary, interaction_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		db.NewID(), "svc-auth", "Auth Service", 0.9, "owns authentication flows", 12)
	if err != nil {
		t.Fatalf("seed relationship memory: %v", err)
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.retrieveRelationshipMemory(ctx, "who handles login incidents?", RetrievalKeyword, 200)

	if !strings.Contains(result, "Auth Service") {
		t.Fatalf("expected fallback relationship retrieval to include Auth Service, got %q", result)
	}
}

func TestRetrieveRelationshipEvidence_PreservesSourceAndAge(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, err := store.ExecContext(ctx,
		`INSERT INTO relationship_memory
		 (id, entity_id, entity_name, trust_score, interaction_summary, interaction_count, last_interaction, updated_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now', '-2 days'), datetime('now', '-40 days'), datetime('now', '-40 days'))`,
		db.NewID(), "svc-billing", "Billing Service", 0.8, "depends on ledger service for invoice settlement", 7)
	if err != nil {
		t.Fatalf("seed relationship memory: %v", err)
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveRelationshipEvidence(ctx, "ledger", RetrievalKeyword, 200)

	if len(results) == 0 {
		t.Fatal("expected relationship evidence")
	}
	if results[0].SourceID == "" || results[0].SourceTable != "relationship_memory" {
		t.Fatalf("expected relationship provenance, got %+v", results[0])
	}
	if results[0].SourceLabel == "" {
		t.Fatal("expected relationship source label")
	}
	if results[0].AgeDays < 39 {
		t.Fatalf("expected stale relationship age to be preserved, got %.2f", results[0].AgeDays)
	}
	if !strings.Contains(results[0].Content, "depends on ledger service") {
		t.Fatalf("expected relationship summary in content, got %q", results[0].Content)
	}
}

func TestRetrieveSemanticEvidence_PreservesAuthorityMetadata(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedSemanticMemory(t, store, "policy", "refund_window", "Refunds are available for 30 days")

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveSemanticEvidence(ctx, "refund", nil, RetrievalKeyword, 200)

	if len(results) == 0 {
		t.Fatal("expected semantic evidence")
	}
	if results[0].SourceID == "" {
		t.Fatal("expected source ID to be preserved")
	}
	if results[0].SourceLabel == "" {
		t.Fatal("expected source label to be populated")
	}
	if !results[0].IsCanonical {
		t.Fatal("policy category should be treated as canonical")
	}
	if results[0].AuthorityScore <= 0 {
		t.Fatal("expected positive authority score")
	}
}

func TestRetrieve_BudgetEnforcement(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sessionID := testutil.SeedSession(t, store, "agent1", "api")
	for i := 0; i < 50; i++ {
		testutil.SeedWorkingMemory(t, store, sessionID, []string{
			"This is a moderately long working memory entry that takes up space in the context window. " +
				"It contains enough text to test budget enforcement across multiple entries.",
		})
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := mr.Retrieve(ctx, sessionID, "", 50) // 50 tokens ≈ 200 chars total
	if len(result) > 2000 {
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
