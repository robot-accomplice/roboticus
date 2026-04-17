package memory

import (
	"context"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/session"
	"roboticus/testutil"
)

// ngramEmbedClient returns an embedding client using the local n-gram fallback
// (no network calls, deterministic, suitable for testing).
func ngramEmbedClient() *llm.EmbeddingClient {
	return llm.NewEmbeddingClient(nil)
}

func TestIngestTurn_GeneratesEpisodicEmbedding(t *testing.T) {
	store := testutil.TempStore(t)
	mgr := NewManager(DefaultConfig(), store)
	mgr.SetEmbeddingClient(ngramEmbedClient())

	sess := session.New(db.NewID(), "test-agent", "test-scope")
	sess.AddUserMessage("deploy the app")
	sess.AddToolResult("call-1", "bash", `{"status": "deployed"}`, false)
	sess.AddAssistantMessage("Deployment complete.", nil)

	mgr.IngestTurn(context.Background(), sess)

	// Verify episodic embedding was created.
	var count int
	err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM embeddings WHERE source_table = 'episodic_memory'`).Scan(&count)
	if err != nil {
		t.Fatalf("query embeddings: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least 1 episodic embedding, got %d", count)
	}
}

func TestIngestTurn_GeneratesSemanticEmbedding(t *testing.T) {
	store := testutil.TempStore(t)
	mgr := NewManager(DefaultConfig(), store)
	mgr.SetEmbeddingClient(ngramEmbedClient())

	sess := session.New(db.NewID(), "test-agent", "test-scope")
	// Reasoning turn with long content triggers semantic storage.
	sess.AddUserMessage("explain the architecture")
	longContent := "The system uses a five-tier memory architecture derived from cognitive science. " +
		"Working memory holds session-scoped context. Episodic memory stores event logs. " +
		"Semantic memory contains distilled facts. Procedural memory tracks tool statistics. " +
		"Relationship memory maintains entity trust scores."
	sess.AddAssistantMessage(longContent, nil)

	mgr.IngestTurn(context.Background(), sess)

	var count int
	err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM embeddings WHERE source_table = 'semantic_memory'`).Scan(&count)
	if err != nil {
		t.Fatalf("query embeddings: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least 1 semantic embedding, got %d", count)
	}
}

func TestIngestTurn_NoEmbeddingWithoutClient(t *testing.T) {
	store := testutil.TempStore(t)
	mgr := NewManager(DefaultConfig(), store)
	// Deliberately NOT setting embedClient.

	sess := session.New(db.NewID(), "test-agent", "test-scope")
	sess.AddUserMessage("deploy the app")
	sess.AddToolResult("call-1", "bash", `{"status": "ok"}`, false)
	sess.AddAssistantMessage("Done.", nil)

	mgr.IngestTurn(context.Background(), sess)

	var count int
	err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM embeddings WHERE source_table IN ('episodic_memory', 'semantic_memory')`).Scan(&count)
	if err != nil {
		t.Fatalf("query embeddings: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 embeddings without client, got %d", count)
	}
}

func TestEmbedAndStore_BlobRoundTrip(t *testing.T) {
	store := testutil.TempStore(t)
	mgr := NewManager(DefaultConfig(), store)
	mgr.SetEmbeddingClient(ngramEmbedClient())
	ctx := context.Background()

	// Store a known text.
	content := "test embedding content for round trip verification"
	mgr.embedAndStore(ctx, "episodic_memory", "test-id-1", content)

	// Read back the blob and verify it decodes correctly.
	var blob []byte
	err := store.QueryRowContext(ctx,
		`SELECT embedding_blob FROM embeddings WHERE source_id = 'test-id-1'`).Scan(&blob)
	if err != nil {
		t.Fatalf("read back embedding: %v", err)
	}

	decoded := db.BlobToEmbedding(blob)
	if len(decoded) == 0 {
		t.Fatal("decoded embedding is empty")
	}

	// Generate the expected embedding directly and compare.
	expected, err := ngramEmbedClient().EmbedSingle(ctx, content)
	if err != nil {
		t.Fatalf("embed expected: %v", err)
	}

	if len(decoded) != len(expected) {
		t.Fatalf("dimension mismatch: got %d, expected %d", len(decoded), len(expected))
	}

	for i := range decoded {
		delta := decoded[i] - expected[i]
		if delta > 1e-6 || delta < -1e-6 {
			t.Errorf("dimension %d: got %f, expected %f (delta %e)", i, decoded[i], expected[i], delta)
		}
	}
}

func TestEmbedAndStore_IncrementalVectorIndex(t *testing.T) {
	store := testutil.TempStore(t)
	mgr := NewManager(DefaultConfig(), store)
	mgr.SetEmbeddingClient(ngramEmbedClient())

	idx := db.NewBruteForceIndex(db.VectorIndexConfig{MinEntries: 1})
	mgr.SetVectorIndex(idx)
	ctx := context.Background()

	mgr.embedAndStore(ctx, "episodic_memory", "test-vec-1", "test content for vector indexing")

	if idx.EntryCount() != 1 {
		t.Errorf("expected 1 vector index entry, got %d", idx.EntryCount())
	}
}

func TestRetrieveEpisodic_UsesPrecomputedEmbeddings(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := ngramEmbedClient()

	// Seed an episodic entry with its embedding.
	entryID := db.NewID()
	content := "the deployment to production server failed with permission denied"
	_, err := store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content) VALUES (?, 'tool_event', ?)`,
		entryID, content)
	if err != nil {
		t.Fatalf("seed episodic: %v", err)
	}

	// Generate and store the embedding.
	vec, err := ec.EmbedSingle(ctx, content)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	blob := db.EmbeddingToBlob(vec)
	_, err = store.ExecContext(ctx,
		`INSERT INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
		 VALUES (?, 'episodic_memory', ?, ?, ?, ?)`,
		db.NewID(), entryID, content[:50], blob, len(vec))
	if err != nil {
		t.Fatalf("seed embedding: %v", err)
	}

	// Create retriever with embedding client.
	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	retriever.SetEmbeddingClient(ec)

	// Retrieve with a semantically related query.
	queryVec, _ := ec.EmbedSingle(ctx, "production deployment error")
	result := retriever.retrieveEpisodic(ctx, "production deployment error", queryVec, 500, 0)

	if result == "" {
		t.Error("expected non-empty episodic retrieval result")
	}
	// The result should contain our seeded content.
	if !contains(result, "deployment") {
		t.Errorf("result should contain 'deployment', got: %s", result)
	}
}

func TestConsolidation_BackfillsEmbeddings(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed episodic entries WITHOUT embeddings (simulating pre-pipeline entries).
	for i := 0; i < 5; i++ {
		_, err := store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content) VALUES (?, 'test', ?)`,
			db.NewID(), "test episodic entry for backfill verification")
		if err != nil {
			t.Fatalf("seed episodic %d: %v", i, err)
		}
	}

	// Verify no embeddings exist yet.
	var countBefore int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table = 'episodic_memory'`).Scan(&countBefore)
	if countBefore != 0 {
		t.Fatalf("expected 0 embeddings before backfill, got %d", countBefore)
	}

	// Run consolidation with embed client.
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.EmbedClient = ngramEmbedClient()
	report := pipe.Run(ctx, store)

	if report.EmbeddingsBackfill < 1 {
		t.Errorf("expected at least 1 backfilled embedding, got %d", report.EmbeddingsBackfill)
	}

	// Verify embeddings now exist.
	var countAfter int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table = 'episodic_memory'`).Scan(&countAfter)
	if countAfter == 0 {
		t.Error("expected embeddings after backfill, got 0")
	}
	if countAfter != 5 {
		t.Errorf("expected 5 backfilled embeddings, got %d", countAfter)
	}
}

func TestLoadStoredEmbeddings_BulkLoad(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := ngramEmbedClient()

	// Seed 3 entries with embeddings.
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = db.NewID()
		content := "test content number " + string(rune('A'+i))
		vec, _ := ec.EmbedSingle(ctx, content)
		blob := db.EmbeddingToBlob(vec)
		_, _ = store.ExecContext(ctx,
			`INSERT INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
			 VALUES (?, 'episodic_memory', ?, ?, ?, ?)`,
			db.NewID(), ids[i], content, blob, len(vec))
	}

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := retriever.loadStoredEmbeddings(ctx, "episodic_memory", ids)

	if len(result) != 3 {
		t.Errorf("expected 3 loaded embeddings, got %d", len(result))
	}
	for _, id := range ids {
		if _, ok := result[id]; !ok {
			t.Errorf("missing embedding for id %s", id)
		}
	}
}

func TestLoadStoredEmbeddings_MissingSomeIDs(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := ngramEmbedClient()

	// Seed only 1 of 3 IDs.
	realID := db.NewID()
	vec, _ := ec.EmbedSingle(ctx, "real content")
	blob := db.EmbeddingToBlob(vec)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
		 VALUES (?, 'episodic_memory', ?, 'real', ?, ?)`,
		db.NewID(), realID, blob, len(vec))

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	result := retriever.loadStoredEmbeddings(ctx, "episodic_memory", []string{realID, "fake-1", "fake-2"})

	if len(result) != 1 {
		t.Errorf("expected 1 loaded embedding (2 missing), got %d", len(result))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
