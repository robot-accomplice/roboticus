package memory

import (
	"context"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// Architecture fitness functions — these tests enforce structural invariants
// that prevent regression of the memory framework improvements.

func TestArchitecture_ManagerHasEmbedClient(t *testing.T) {
	// Ensures the Manager struct retains its embedding capability.
	mgr := NewManager(DefaultConfig(), nil)
	mgr.SetEmbeddingClient(nil) // Must compile — proves the field exists.
	mgr.SetVectorIndex(nil)     // Must compile — proves the field exists.
}

func TestArchitecture_RetrieverHasCompleter(t *testing.T) {
	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), nil)
	retriever.SetCompleter(nil) // Must compile — proves the field exists.
}

func TestArchitecture_ConsolidationHasEmbedClient(t *testing.T) {
	// Ensures the ConsolidationPipeline retains embedding backfill capability.
	pipe := NewConsolidationPipeline()
	pipe.EmbedClient = nil // Must compile — proves the field exists.
}

func TestArchitecture_ConsolidationHasDistiller(t *testing.T) {
	// Ensures the ConsolidationPipeline retains LLM distillation capability.
	pipe := NewConsolidationPipeline()
	pipe.Distiller = nil      // Must compile — proves the field exists.
	pipe.MaxDistillPerRun = 5 // Must compile — proves the field exists.
}

func TestArchitecture_RetrievalMetricsHasMode(t *testing.T) {
	// Ensures retrieval metrics report the active retrieval mode.
	var m RetrievalMetrics
	_ = m.RetrievalMode // Must compile — proves the field exists.
}

func TestArchitecture_RetrievalMetricsHasCollapseSignals(t *testing.T) {
	// Ensures collapse detection fields exist on RetrievalMetrics.
	var m RetrievalMetrics
	_ = m.ScoreSpread
	_ = m.AvgFTSScore
	_ = m.AvgVectorScore
	_ = m.CorpusSize
	_ = m.HybridWeight
}

func TestArchitecture_ConsolidationReportHasSuperseded(t *testing.T) {
	// Ensures contradiction detection is tracked in reports.
	var r ConsolidationReport
	_ = r.Superseded         // Must compile — proves the field exists.
	_ = r.EmbeddingsBackfill // Must compile — proves the field exists.
}

func TestArchitecture_FTSHasUpdateTriggers(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	var count int
	err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE '%_fts_au'`).Scan(&count)
	if err != nil {
		t.Fatalf("query triggers: %v", err)
	}
	if count < 2 {
		t.Errorf("expected at least 2 FTS UPDATE triggers (*_fts_au), found %d", count)
	}
}

func TestArchitecture_EmbeddingsTableExists(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Verify the embeddings table exists and has the expected columns.
	var count int
	err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('embeddings') WHERE name IN ('source_table', 'source_id', 'embedding_blob', 'dimensions')`).Scan(&count)
	if err != nil {
		t.Fatalf("query embeddings schema: %v", err)
	}
	if count < 4 {
		t.Errorf("embeddings table missing expected columns, found %d/4", count)
	}
}

func TestArchitecture_MemoryIndexTableExists(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	var count int
	err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('memory_index') WHERE name IN ('source_table', 'source_id', 'summary', 'confidence')`).Scan(&count)
	if err != nil {
		t.Fatalf("query memory_index schema: %v", err)
	}
	if count < 4 {
		t.Errorf("memory_index table missing expected columns, found %d/4", count)
	}
}

func TestArchitecture_AdaptiveBudgetRedistributes(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed only episodic data — other tiers empty.
	for i := 0; i < 10; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content) VALUES (?, 'test', ?)`,
			db.NewID(), "test entry for adaptive budget architecture verification")
	}
	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	_, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "test", 2048)

	// With empty procedural and relationship tiers, the adaptive budget should
	// still produce a reasonable utilization.
	if metrics.EpisodicCount == 0 {
		t.Error("expected episodic entries in result")
	}
	// The mode should be set (not empty).
	if metrics.RetrievalMode == "" {
		t.Error("retrieval mode should be populated")
	}
}
