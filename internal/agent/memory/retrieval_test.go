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
	result := mr.retrieveProceduralMemory(ctx, "how to read files", nil, RetrievalKeyword, 200)

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
	result := mr.retrieveRelationshipMemory(ctx, "who handles login incidents?", nil, RetrievalKeyword, 200)

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
	results := mr.retrieveRelationshipEvidence(ctx, "ledger", nil, RetrievalKeyword, 200)

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

func TestRetrieveKnowledgeFactEvidence(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, err := store.ExecContext(ctx,
		`INSERT INTO knowledge_facts
		 (id, subject, relation, object, confidence, updated_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now', '-12 days'))`,
		db.NewID(), "Billing Service", "depends_on", "Ledger Service", 0.82)
	if err != nil {
		t.Fatalf("seed knowledge_facts: %v", err)
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveKnowledgeFactEvidence(ctx, "ledger", RetrievalKeyword, 200)

	if len(results) == 0 {
		t.Fatal("expected graph evidence")
	}
	if results[0].SourceTable != "knowledge_facts" {
		t.Fatalf("expected knowledge_facts source, got %+v", results[0])
	}
	if !strings.Contains(results[0].Content, "Billing Service depends_on Ledger Service") {
		t.Fatalf("unexpected graph content: %q", results[0].Content)
	}
	if results[0].AgeDays < 11 {
		t.Fatalf("expected preserved fact age, got %.2f", results[0].AgeDays)
	}
}

func TestRetrieveKnowledgeFactEvidence_GraphTraversalExpandsDependencies(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	seedFacts := []struct {
		subject    string
		relation   string
		object     string
		confidence float64
	}{
		{"Billing Service", "depends_on", "Ledger Service", 0.9},
		{"Ledger Service", "uses", "Postgres", 0.8},
		{"Billing Service", "owned_by", "Revenue Platform", 0.75},
	}
	for _, fact := range seedFacts {
		_, err := store.ExecContext(ctx,
			`INSERT INTO knowledge_facts
			 (id, subject, relation, object, confidence, updated_at)
			 VALUES (?, ?, ?, ?, ?, datetime('now', '-1 day'))`,
			db.NewID(), fact.subject, fact.relation, fact.object, fact.confidence)
		if err != nil {
			t.Fatalf("seed knowledge_facts: %v", err)
		}
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveKnowledgeFactEvidence(ctx, "what depends on billing service?", RetrievalGraph, 400)

	if len(results) < 2 {
		t.Fatalf("expected graph traversal results, got %+v", results)
	}

	joined := results[0].Content + "\n" + results[1].Content + "\n" + results[2].Content
	if !strings.Contains(joined, "Dependency chain from Billing Service") {
		t.Fatalf("expected graph expansion evidence, got %q", joined)
	}
	if !strings.Contains(joined, "Billing Service --depends_on--> Ledger Service") {
		t.Fatalf("expected seed dependency chain, got %q", joined)
	}
	if !strings.Contains(joined, "Ledger Service --uses--> Postgres") {
		t.Fatalf("expected connected dependency expansion, got %q", joined)
	}
}

func TestRetrieveKnowledgeFactEvidence_GraphPathBetweenEntities(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	seedFacts := []struct {
		subject    string
		relation   string
		object     string
		confidence float64
	}{
		{"Billing Service", "depends_on", "Ledger Service", 0.9},
		{"Ledger Service", "uses", "Postgres", 0.8},
		{"Customer Portal", "uses", "Redis", 0.7},
	}
	for _, fact := range seedFacts {
		_, err := store.ExecContext(ctx,
			`INSERT INTO knowledge_facts
			 (id, subject, relation, object, confidence, updated_at)
			 VALUES (?, ?, ?, ?, ?, datetime('now', '-1 day'))`,
			db.NewID(), fact.subject, fact.relation, fact.object, fact.confidence)
		if err != nil {
			t.Fatalf("seed knowledge_facts: %v", err)
		}
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveKnowledgeFactEvidence(ctx, "what path connects Billing Service and Postgres?", RetrievalGraph, 400)

	if len(results) == 0 {
		t.Fatal("expected graph path evidence")
	}
	if !strings.Contains(results[0].Content, "Path between Billing Service and Postgres") {
		t.Fatalf("expected path evidence first, got %q", results[0].Content)
	}
	if !strings.Contains(results[0].Content, "--depends_on--> Ledger Service --uses--> Postgres") {
		t.Fatalf("expected full graph path, got %q", results[0].Content)
	}
}

func TestRetrieveKnowledgeFactEvidence_GraphImpactTraversesReverseDependencies(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	seedFacts := []struct {
		subject    string
		relation   string
		object     string
		confidence float64
	}{
		{"Billing Service", "depends_on", "Ledger Service", 0.9},
		{"Invoice Worker", "depends_on", "Billing Service", 0.85},
		{"Ledger Service", "uses", "Postgres", 0.8},
	}
	for _, fact := range seedFacts {
		_, err := store.ExecContext(ctx,
			`INSERT INTO knowledge_facts
			 (id, subject, relation, object, confidence, updated_at)
			 VALUES (?, ?, ?, ?, ?, datetime('now', '-1 day'))`,
			db.NewID(), fact.subject, fact.relation, fact.object, fact.confidence)
		if err != nil {
			t.Fatalf("seed knowledge_facts: %v", err)
		}
	}

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveKnowledgeFactEvidence(ctx, "what is the blast radius if Ledger Service fails?", RetrievalGraph, 400)

	if len(results) == 0 {
		t.Fatal("expected impact chain evidence")
	}
	if !strings.Contains(results[0].Content, "Impact chain from Ledger Service") {
		t.Fatalf("expected impact chain evidence, got %q", results[0].Content)
	}
	joined := results[0].Content
	if len(results) > 1 {
		joined += "\n" + results[1].Content
	}
	if !strings.Contains(joined, "Ledger Service --depends_on--> Billing Service") {
		t.Fatalf("expected reverse dependency to impacted service, got %q", joined)
	}
}

func TestRetrieveSemanticEvidence_PreservesAuthorityMetadata(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// After Milestone 3 follow-on: canonical status is an explicit persisted
	// flag, not an inference from category/key. Tests that expect an
	// authoritative ranking must seed a canonically-asserted row and supply
	// the source_label directly.
	testutil.SeedCanonicalSemanticMemory(t, store, "policy", "refund_window",
		"Refunds are available for 30 days", "policy/refund-v1")

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveSemanticEvidence(ctx, "refund", nil, RetrievalKeyword, 200)

	if len(results) == 0 {
		t.Fatal("expected semantic evidence")
	}
	if results[0].SourceID == "" {
		t.Fatal("expected source ID to be preserved")
	}
	if results[0].SourceLabel != "policy/refund-v1" {
		t.Fatalf("expected persisted source_label to be used verbatim, got %q", results[0].SourceLabel)
	}
	if !results[0].IsCanonical {
		t.Fatal("caller-asserted canonical row should surface as canonical")
	}
	if results[0].AuthorityScore <= 0 {
		t.Fatal("expected positive authority score")
	}
}

func TestRetrieveSemanticEvidence_NonCanonicalStaysNonCanonical(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Pre-migration behaviour inferred canonical from the word "policy" in
	// the category. That inference is gone — a row seeded without the
	// explicit canonical flag must NOT come back marked canonical even if
	// its category says "policy".
	testutil.SeedSemanticMemory(t, store, "policy", "refund_window", "Refunds are available for 30 days")

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveSemanticEvidence(ctx, "refund", nil, RetrievalKeyword, 200)
	if len(results) == 0 {
		t.Fatal("expected semantic evidence")
	}
	if results[0].IsCanonical {
		t.Fatal("row without explicit canonical flag must not surface as canonical")
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
