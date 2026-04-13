package memory

import (
	"context"
	"os"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// TestIntegration_RealEmbeddingProvider exercises the full memory pipeline with
// a real embedding provider. Skipped unless ROBOTICUS_TEST_EMBED_URL is set.
//
// Example: ROBOTICUS_TEST_EMBED_URL=http://localhost:11434 go test ./internal/agent/memory/... -run TestIntegration_Real -v
func TestIntegration_RealEmbeddingProvider(t *testing.T) {
	embedURL := os.Getenv("ROBOTICUS_TEST_EMBED_URL")
	if embedURL == "" {
		t.Skip("ROBOTICUS_TEST_EMBED_URL not set — skipping real embedding integration test")
	}
	embedModel := os.Getenv("ROBOTICUS_TEST_EMBED_MODEL")
	if embedModel == "" {
		embedModel = "nomic-embed-text" // default Ollama embedding model
	}

	ec := llm.NewEmbeddingClient(&llm.Provider{
		Name:           "test-embed",
		URL:            embedURL,
		Format:         llm.FormatOllama,
		EmbeddingPath:  "/api/embeddings",
		EmbeddingModel: embedModel,
		IsLocal:        true,
	})

	store := testutil.TempStore(t)
	ctx := context.Background()

	// Test 1: Embedding dimensions should be real (not 128 n-gram).
	vec, err := ec.EmbedSingle(ctx, "test embedding quality")
	if err != nil {
		t.Fatalf("EmbedSingle failed: %v", err)
	}
	if len(vec) <= 128 {
		t.Errorf("real embeddings should be >128 dimensions, got %d", len(vec))
	}
	t.Logf("✓ Embedding dimensions: %d", len(vec))

	// Test 2: Semantic similarity should be meaningful.
	vecA, _ := ec.EmbedSingle(ctx, "the server deployment failed with error")
	vecB, _ := ec.EmbedSingle(ctx, "deploy error on production server")
	vecC, _ := ec.EmbedSingle(ctx, "delicious breakfast recipe with eggs")

	simAB := llm.CosineSimilarity(vecA, vecB)
	simAC := llm.CosineSimilarity(vecA, vecC)

	t.Logf("  sim(deploy_error, deploy_error_rephrase) = %.4f", simAB)
	t.Logf("  sim(deploy_error, breakfast_recipe)       = %.4f", simAC)

	if simAB <= simAC {
		t.Error("related texts should have higher similarity than unrelated")
	}
	if simAB < 0.5 {
		t.Errorf("related texts should have sim > 0.5, got %.4f", simAB)
	}
	if simAC > 0.4 {
		t.Errorf("unrelated texts should have sim < 0.4, got %.4f", simAC)
	}
	t.Log("✓ Semantic similarity is meaningful")

	// Test 3: Full ingestion pipeline with real embeddings.
	mgr := NewManager(DefaultConfig(), store)
	mgr.SetEmbeddingClient(ec)

	sess := newTestSession(db.NewID())
	sess.AddUserMessage("deploy the app to production")
	sess.AddToolResult("call-1", "bash", `{"status": "deployed"}`, false)
	sess.AddAssistantMessage("Deployed successfully.", nil)
	mgr.IngestTurn(ctx, sess)

	var embedCount int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table = 'episodic_memory'`).Scan(&embedCount)
	if embedCount == 0 {
		t.Error("CRITICAL-1: no episodic embeddings generated with real provider")
	}

	var dims int
	_ = store.QueryRowContext(ctx,
		`SELECT dimensions FROM embeddings WHERE source_table = 'episodic_memory' LIMIT 1`).Scan(&dims)
	if dims <= 128 {
		t.Errorf("real provider should produce >128 dim embeddings, got %d", dims)
	}
	t.Logf("✓ Ingestion with real embeddings: %d entries, %d dimensions", embedCount, dims)

	// Test 4: Classification with real embeddings.
	finType, ok := classifyTurnWithEmbeddings(ctx, ec, "wire $500 to the vendor account for the invoice payment")
	if ok && finType == TurnFinancial {
		t.Log("✓ Classification: 'wire $500 to vendor' correctly classified as Financial")
	} else {
		t.Logf("⚠ Classification: 'wire $500 to vendor' = type %v (ok=%v)", finType, ok)
	}

	// Test 5: Contradiction detection with subject similarity.
	sim := subjectSimilarity("the system uses Docker containers", "the system uses Podman containers")
	t.Logf("  subject_similarity(Docker, Podman) = %.4f", sim)
	if sim < 0.5 {
		t.Error("same-subject contradiction should have high subject similarity")
	}
}
