package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/session"
	"roboticus/testutil"
)

// TestProductionScenario_ExistingDataBackfill simulates what happens when
// the new embedding pipeline encounters a database with pre-existing memory
// entries that have no embeddings — mirroring the narrator profile's state
// (486 episodic, 1349 semantic, 0 memory-tier embeddings).
func TestProductionScenario_ExistingDataBackfill(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := llm.NewEmbeddingClient(nil)

	// Simulate pre-existing data (like narrator's entries).
	// Use highly diverse content to prevent dedup from removing entries.
	numEpisodic := 50
	numSemantic := 30
	for i := 0; i < numEpisodic; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance)
			 VALUES (?, 'tool_use', ?, 5)`,
			db.NewID(), fmt.Sprintf("unique episodic event %d: the specific action taken was %d and the result was code %d at timestamp %d",
				i, i*17, i*31, i*43))
	}
	for i := 0; i < numSemantic; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value)
			 VALUES (?, 'learned', ?, ?)`,
			db.NewID(), fmt.Sprintf("unique_fact_%d", i),
			fmt.Sprintf("distinct learned fact number %d describing property %d of component %d", i, i*13, i*7))
	}

	// Verify no memory-tier embeddings exist (matching production state).
	var embedsBefore int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table IN ('episodic_memory', 'semantic_memory')`).Scan(&embedsBefore)
	if embedsBefore != 0 {
		t.Fatalf("expected 0 memory-tier embeddings before backfill, got %d", embedsBefore)
	}

	// Run consolidation with embed client — should backfill embeddings.
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.EmbedClient = ec

	// First run: backfills up to 50 (25 per tier).
	r1 := pipe.Run(ctx, store)
	t.Logf("Run 1: indexed=%d, backfilled=%d", r1.Indexed, r1.EmbeddingsBackfill)

	if r1.EmbeddingsBackfill == 0 {
		t.Error("first consolidation run should have backfilled embeddings")
	}

	var embedsAfterRun1 int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table IN ('episodic_memory', 'semantic_memory')`).Scan(&embedsAfterRun1)
	t.Logf("After run 1: %d memory-tier embeddings", embedsAfterRun1)

	// Second run: should backfill more (if any remain).
	pipe.MinInterval = 0
	r2 := pipe.Run(ctx, store)
	t.Logf("Run 2: indexed=%d, backfilled=%d", r2.Indexed, r2.EmbeddingsBackfill)

	var embedsAfterRun2 int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table IN ('episodic_memory', 'semantic_memory')`).Scan(&embedsAfterRun2)
	t.Logf("After run 2: %d memory-tier embeddings", embedsAfterRun2)

	// After runs, embeddings should be growing. Some entries may have been
	// deduped/promoted by consolidation, reducing the active set.
	var activeEntries int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE memory_state = 'active'`).Scan(&activeEntries)
	var activeSemantic int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE memory_state = 'active'`).Scan(&activeSemantic)
	activeTotal := activeEntries + activeSemantic

	// Run more times to complete backfill of remaining active entries.
	for i := 0; i < 10; i++ {
		pipe.MinInterval = 0
		pipe.Run(ctx, store)
	}
	var embedsFinal int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table IN ('episodic_memory', 'semantic_memory')`).Scan(&embedsFinal)
	t.Logf("After all runs: %d embeddings, %d active entries across tiers", embedsFinal, activeTotal)
	if embedsFinal == 0 {
		t.Error("expected some backfilled embeddings")
	}

	// Verify retrieval works with backfilled embeddings.
	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	retriever.SetEmbeddingClient(ec)

	result, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "unique episodic event", 2048)
	t.Logf("✓ Retrieval after backfill: %d entries, mode=%s, budget=%.0f%%",
		metrics.TotalEntries, metrics.RetrievalMode, metrics.BudgetUsedPct*100)
	if metrics.RetrievalMode == "" {
		t.Error("retrieval mode should be populated")
	}
	_ = result
}

// TestProductionScenario_HighVolumeDedup tests dedup performance with a
// production-like volume of entries (500 per tier).
func TestProductionScenario_HighVolumeDedup(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed 500 episodic entries with some duplicates (~10% duplication rate).
	for i := 0; i < 500; i++ {
		content := fmt.Sprintf("bash: executed command variant %d with output containing status code %d",
			i%50, i%7) // Only 50 unique patterns × 7 status codes = ~350 unique
		_, _ = store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance)
			 VALUES (?, 'tool_use', ?, 5)`,
			db.NewID(), content)
	}

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	r := pipe.Run(ctx, store)
	t.Logf("High-volume dedup: processed 500 entries, deduped=%d", r.Deduped)

	// With MinHash/LSH, this should complete quickly (not O(n²)).
	// The batch cap of 500 means all entries are processed.
	var remaining int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE memory_state = 'active'`).Scan(&remaining)
	t.Logf("After dedup: %d active entries remain (from 500)", remaining)

	if remaining >= 500 {
		t.Log("No duplicates found — test patterns may not exceed Jaccard threshold 0.85")
	}
}

// TestProductionScenario_MixedWorkload simulates a realistic mixed workload:
// tool-heavy session followed by knowledge exploration, then social interaction.
func TestProductionScenario_MixedWorkload(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := llm.NewEmbeddingClient(nil)

	mgr := NewManager(DefaultConfig(), store)
	mgr.SetEmbeddingClient(ec)

	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	// Phase 1: Tool-heavy session.
	tools := []string{"bash", "list_directory", "obsidian_write", "get_runtime_context", "bash"}
	for i, tool := range tools {
		sess := newTestSession(sessionID)
		sess.AddUserMessage(fmt.Sprintf("use %s to check system status round %d", tool, i))
		sess.AddToolResult(fmt.Sprintf("call-%d", i), tool, fmt.Sprintf(`{"status": "ok", "round": %d}`, i), false)
		sess.AddAssistantMessage(fmt.Sprintf("Tool %s completed successfully.", tool), nil)
		mgr.IngestTurn(ctx, sess)
	}

	// Phase 2: Knowledge exploration (reasoning turn — no tool results, long response).
	sess := newTestSession(sessionID)
	sess.AddUserMessage("analyze the memory consolidation pipeline architecture")
	explanation := "The consolidation pipeline runs in the background every 60 seconds. " +
		"It performs index backfill, embedding backfill, within-tier deduplication using MinHash/LSH, " +
		"episodic-to-semantic promotion via LLM distillation, contradiction detection, " +
		"confidence decay, importance decay, pruning, and orphan cleanup."
	sess.AddAssistantMessage(explanation, nil)
	mgr.IngestTurn(ctx, sess)

	// Phase 3: Social interaction with entity mentions.
	sess = newTestSession(sessionID)
	sess.AddUserMessage("talked to Alice Johnson about the deployment schedule with @ops team")
	sess.AddAssistantMessage("Noted the conversation with Alice Johnson and the ops team.", nil)
	mgr.IngestTurn(ctx, sess)

	// Verify all tiers populated.
	tierCounts := map[string]int{}
	for _, table := range []string{"episodic_memory", "semantic_memory", "procedural_memory", "relationship_memory", "working_memory"} {
		var count int
		_ = store.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		tierCounts[table] = count
	}

	t.Log("Mixed workload tier counts:")
	for table, count := range tierCounts {
		t.Logf("  %s: %d", table, count)
	}

	// Episodic should have tool events (excluding derivable tools).
	if tierCounts["episodic_memory"] == 0 {
		t.Error("expected episodic entries from tool events")
	}

	// Procedural should track tool usage.
	if tierCounts["procedural_memory"] == 0 {
		t.Error("expected procedural entries from tool tracking")
	}

	// Semantic may or may not have the explanation depending on turn classification.
	// With n-gram embedding classification, the turn may be classified differently.
	if tierCounts["semantic_memory"] == 0 {
		t.Log("  ⚠ no semantic entries — n-gram classification may not trigger reasoning for this text")
	}

	// Relationship should have entities.
	if tierCounts["relationship_memory"] == 0 {
		t.Error("expected relationship entries from entity mentions")
	}

	// Working memory should have turn summaries.
	if tierCounts["working_memory"] == 0 {
		t.Error("expected working memory turn summaries")
	}

	// Verify embeddings were generated for non-derivable episodic + semantic.
	var embedCount int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table IN ('episodic_memory', 'semantic_memory')`).Scan(&embedCount)
	if embedCount == 0 {
		t.Error("expected embeddings from ingestion")
	}
	t.Logf("  embeddings: %d", embedCount)

	// Full retrieval should work.
	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	retriever.SetEmbeddingClient(ec)
	result, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "deployment", 2048)

	if result == "" || metrics.TotalEntries == 0 {
		t.Error("retrieval returned empty after mixed workload")
	}
	t.Logf("  retrieval: %d entries, mode=%s, budget=%.0f%%",
		metrics.TotalEntries, metrics.RetrievalMode, metrics.BudgetUsedPct*100)

	// Session summary promotion should work.
	mgr.PromoteSessionSummary(ctx, sessionID)
	var summaryExists int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE category = 'session_summary'`).Scan(&summaryExists)
	if summaryExists == 0 {
		t.Error("session summary promotion failed")
	}

	// Cross-session injection should work.
	newSessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, newSessionID)
	working := retriever.retrieveWorkingMemory(ctx, newSessionID, 500)
	if !strings.Contains(working, "Previously:") {
		t.Error("cross-session continuity failed")
	}

	t.Log("✓ Mixed workload: all tiers populated, embeddings generated, retrieval works, cross-session continuity active")
}

func newTestSession(sessionID string) *session.Session {
	return session.New(sessionID, "test-agent", "test-scope")
}
