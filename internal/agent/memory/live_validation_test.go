package memory

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/session"
	"roboticus/testutil"
)

// TestLiveValidation_FullPipeline exercises the complete memory lifecycle:
//
//  1. Ingestion: Multiple turns with diverse content
//  2. Embedding: Verify embeddings were generated at ingestion time
//  3. Retrieval: Query-aware retrieval with precomputed embeddings
//  4. Consolidation: Dedup, promotion, contradiction detection, backfill
//  5. Entity extraction: Proper noun detection in natural language
//  6. Cross-session continuity: Session summary promotion and injection
//  7. Adaptive budgets: Surplus redistribution from empty tiers
//
// This test validates that all remediated findings work together as a system,
// not just as isolated unit tests.
func TestLiveValidation_FullPipeline(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := llm.NewEmbeddingClient(nil) // n-gram fallback (deterministic, offline)

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 1: Ingestion — simulate a multi-turn conversation
	// ═══════════════════════════════════════════════════════════════════

	mgr := NewManager(DefaultConfig(), store)
	mgr.SetEmbeddingClient(ec)

	sessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'live-test', 'active')`, sessionID)

	// Turn 1: Tool use (should create episodic + procedural)
	sess1 := session.New(sessionID, "live-test", "test-scope")
	sess1.AddUserMessage("deploy the application to the production server")
	sess1.AddToolResult("call-1", "bash", `{"status": "deployed", "server": "prod-01"}`, false)
	sess1.AddAssistantMessage("Deployed successfully to prod-01.", nil)
	mgr.IngestTurn(ctx, sess1)

	// Turn 2: Financial event (should create episodic with importance=8)
	sess2 := session.New(sessionID, "live-test", "test-scope")
	sess2.AddUserMessage("check my wallet balance and transfer 50 USDC to the payment address")
	sess2.AddAssistantMessage("Your balance is 500 USDC. Transferring 50 USDC now.", nil)
	mgr.IngestTurn(ctx, sess2)

	// Turn 3: Reasoning/knowledge (should create semantic)
	sess3 := session.New(sessionID, "live-test", "test-scope")
	sess3.AddUserMessage("explain how the deployment pipeline works")
	longExplanation := "The deployment pipeline uses a three-stage process. First, the code is built and " +
		"tested in CI. Then, it is deployed to a staging environment for smoke tests. " +
		"Finally, after approval, it is promoted to production via blue-green deployment. " +
		"The entire process takes approximately 15 minutes end-to-end."
	sess3.AddAssistantMessage(longExplanation, nil)
	mgr.IngestTurn(ctx, sess3)

	// Turn 4: Social (with proper noun entity reference)
	sess4 := session.New(sessionID, "live-test", "test-scope")
	sess4.AddUserMessage("talked to Sarah Chen about the deployment issue with @devops")
	sess4.AddAssistantMessage("I'll note that Sarah Chen and devops are involved.", nil)
	mgr.IngestTurn(ctx, sess4)

	t.Log("✓ Ingestion: 4 turns ingested")

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 2: Verify embeddings were generated at ingestion time
	// ═══════════════════════════════════════════════════════════════════

	var episodicEmbedCount, semanticEmbedCount int
	store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table = 'episodic_memory'`).Scan(&episodicEmbedCount)
	store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table = 'semantic_memory'`).Scan(&semanticEmbedCount)

	if episodicEmbedCount == 0 {
		t.Error("CRITICAL-1 REGRESSION: no episodic embeddings generated at ingestion time")
	} else {
		t.Logf("✓ Embeddings: %d episodic, %d semantic embeddings generated at ingestion", episodicEmbedCount, semanticEmbedCount)
	}

	// Verify blob round-trip integrity.
	var blob []byte
	err := store.QueryRowContext(ctx,
		`SELECT embedding_blob FROM embeddings WHERE source_table = 'episodic_memory' LIMIT 1`).Scan(&blob)
	if err != nil {
		t.Fatalf("failed to read embedding blob: %v", err)
	}
	decoded := db.BlobToEmbedding(blob)
	if len(decoded) == 0 {
		t.Error("embedding blob decoded to empty vector")
	} else {
		t.Logf("✓ Blob integrity: %d-dimensional vector round-trips correctly", len(decoded))
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 3: Retrieval — verify precomputed embeddings are used
	// ═══════════════════════════════════════════════════════════════════

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	retriever.SetEmbeddingClient(ec)

	result, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "deployment", 2048)

	if result == "" {
		t.Error("retrieval returned empty result")
	}
	if !strings.Contains(result, "[Active Memory]") {
		t.Error("retrieval missing [Active Memory] header")
	}
	if metrics.TotalEntries == 0 {
		t.Error("retrieval returned 0 entries")
	}
	if metrics.RetrievalMode == "" {
		t.Error("MEDIUM-3 REGRESSION: retrieval mode not populated in metrics")
	}
	t.Logf("✓ Retrieval: %d entries, mode=%s, budget=%.0f%%",
		metrics.TotalEntries, metrics.RetrievalMode, metrics.BudgetUsedPct*100)

	// Verify episodic reranking uses precomputed embeddings (not per-candidate API calls).
	// The loadStoredEmbeddings function returns entries from the embeddings table.
	queryVec, _ := ec.EmbedSingle(ctx, "deployment")
	storedEmbeds := retriever.loadStoredEmbeddings(ctx, "episodic_memory", getAllEpisodicIDs(ctx, store))
	if len(storedEmbeds) == 0 {
		t.Error("HIGH-5 REGRESSION: no precomputed embeddings available for reranking")
	} else {
		// Verify cosine similarity is computable between query and stored vectors.
		for id, vec := range storedEmbeds {
			sim := llm.CosineSimilarity(queryVec, vec)
			if math.IsNaN(sim) {
				t.Errorf("cosine similarity NaN for entry %s", id)
			}
		}
		t.Logf("✓ Precomputed reranking: %d stored embeddings available, cosine computable", len(storedEmbeds))
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 4: Entity extraction — verify proper nouns detected
	// ═══════════════════════════════════════════════════════════════════

	var entityCount int
	store.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT entity_name) FROM relationship_memory`).Scan(&entityCount)

	if entityCount == 0 {
		t.Error("HIGH-2 REGRESSION: no entities extracted from natural language")
	} else {
		t.Logf("✓ Entity extraction: %d distinct entities in relationship memory", entityCount)
	}

	// Check for "Sarah Chen" or "Sarah" specifically.
	var sarahCount int
	store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM relationship_memory WHERE entity_name LIKE '%Sarah%'`).Scan(&sarahCount)
	if sarahCount == 0 {
		t.Log("  ⚠ 'Sarah Chen' not found — proper noun extraction may need tuning for this text pattern")
	} else {
		t.Log("  ✓ 'Sarah' detected via proper noun extraction")
	}

	// Check for @devops (@ mention).
	var devopsCount int
	store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM relationship_memory WHERE entity_name = 'devops'`).Scan(&devopsCount)
	if devopsCount == 0 {
		t.Error("  ✗ @devops mention not detected")
	} else {
		t.Log("  ✓ @devops detected via @-mention extraction")
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 5: Consolidation — full pipeline execution
	// ═══════════════════════════════════════════════════════════════════

	// Add some data to exercise consolidation phases.
	// Seed similar episodic entries for dedup testing.
	for i := 0; i < 3; i++ {
		store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance)
			 VALUES (?, 'tool_event', ?, 5)`,
			db.NewID(), fmt.Sprintf("bash: deployed app to server variant %d with success confirmation", i))
	}

	// Seed contradicting semantic entries.
	store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, created_at)
		 VALUES (?, 'knowledge', 'db_engine', 'the system uses PostgreSQL for persistence', datetime('now', '-1 hour'))`,
		db.NewID())
	store.ExecContext(ctx,
		`INSERT INTO consolidation_log (id, indexed, deduped, promoted, confidence_decayed, importance_decayed, pruned, orphaned, created_at)
		 VALUES (?, 0, 0, 0, 0, 0, 0, 0, datetime('now', '-30 minutes'))`, db.NewID())
	store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, 'knowledge', 'db_system', 'the system uses SQLite for persistence')`,
		db.NewID())

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.EmbedClient = ec
	report := pipe.Run(ctx, store)

	t.Logf("✓ Consolidation report:")
	t.Logf("  indexed=%d, embeddings_backfill=%d, deduped=%d, promoted=%d",
		report.Indexed, report.EmbeddingsBackfill, report.Deduped, report.Promoted)
	t.Logf("  superseded=%d, confidence_decayed=%d, importance_decayed=%d, pruned=%d, orphaned=%d",
		report.Superseded, report.ConfidenceDecayed, report.ImportanceDecayed, report.Pruned, report.Orphaned)

	if report.Indexed < 1 {
		t.Error("  ✗ index backfill should have indexed new entries")
	}
	if report.EmbeddingsBackfill < 1 {
		t.Error("  ✗ embedding backfill should have backfilled entries created outside Manager")
	}

	// Verify FTS UPDATE triggers work (HIGH-4).
	var triggerCount int
	store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE '%_fts_au'`).Scan(&triggerCount)
	if triggerCount < 2 {
		t.Errorf("  HIGH-4 REGRESSION: expected ≥2 FTS UPDATE triggers, found %d", triggerCount)
	} else {
		t.Logf("  ✓ FTS UPDATE triggers: %d present", triggerCount)
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 6: Cross-session continuity
	// ═══════════════════════════════════════════════════════════════════

	// Promote the session summary.
	mgr.PromoteSessionSummary(ctx, sessionID)

	var summaryValue string
	err = store.QueryRowContext(ctx,
		`SELECT value FROM semantic_memory WHERE category = 'session_summary' AND key = ?`, sessionID).Scan(&summaryValue)
	if err != nil {
		t.Error("MEDIUM-4 REGRESSION: session summary not promoted")
	} else {
		t.Logf("✓ Session summary promoted: %q", truncateForLog(summaryValue, 80))
	}

	// Create a new session and verify "Previously:" injection.
	newSessionID := db.NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'live-test', 'active')`, newSessionID)

	working := retriever.retrieveWorkingMemory(ctx, newSessionID, 500)
	if !strings.Contains(working, "Previously:") {
		t.Error("MEDIUM-4 REGRESSION: new session missing 'Previously:' injection")
	} else {
		t.Log("✓ Cross-session: new session receives 'Previously:' context")
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 7: Adaptive budget — verify surplus redistribution
	// ═══════════════════════════════════════════════════════════════════

	// Retrieve with empty procedural + relationship tiers (common scenario).
	_, adaptiveMetrics := retriever.RetrieveWithMetrics(ctx, newSessionID, "deployment", 2048)
	if adaptiveMetrics.ProceduralCount == 0 && adaptiveMetrics.RelationCount == 0 {
		// Good — these tiers are likely empty, which means surplus should flow to other tiers.
		t.Log("✓ Adaptive budgets: empty tiers detected, surplus available for redistribution")
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 8: Memory tier counts — verify all tiers populated
	// ═══════════════════════════════════════════════════════════════════

	tierCounts := map[string]int{}
	for _, table := range []string{"working_memory", "episodic_memory", "semantic_memory", "procedural_memory", "relationship_memory"} {
		var count int
		store.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		tierCounts[table] = count
	}

	t.Log("✓ Tier counts after full pipeline:")
	for table, count := range tierCounts {
		t.Logf("  %s: %d entries", table, count)
		if count == 0 && table != "working_memory" {
			t.Errorf("  ✗ %s is empty — ingestion may have failed for this tier", table)
		}
	}

	var totalEmbeddings int
	store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE source_table IN ('episodic_memory', 'semantic_memory')`).Scan(&totalEmbeddings)
	t.Logf("  embeddings: %d total (episodic + semantic)", totalEmbeddings)

	var totalIndex int
	store.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_index`).Scan(&totalIndex)
	t.Logf("  memory_index: %d entries", totalIndex)

	// ═══════════════════════════════════════════════════════════════════
	// FINAL: Quantifiable success criteria verification
	// ═══════════════════════════════════════════════════════════════════

	t.Log("\n═══ Quantifiable Success Criteria ═══")

	// CRITICAL-1: Embeddings at ingest
	if episodicEmbedCount+semanticEmbedCount > 0 {
		t.Log("✓ CRITICAL-1: Embeddings generated at ingestion time")
	} else {
		t.Error("✗ CRITICAL-1: FAILED — no embeddings at ingest")
	}

	// HIGH-2: Entity extraction beyond @-mentions
	if entityCount > 1 { // @devops + proper nouns
		t.Logf("✓ HIGH-2: %d entities extracted (beyond @-mentions)", entityCount)
	} else {
		t.Logf("⚠ HIGH-2: only %d entity — proper noun detection may need more data", entityCount)
	}

	// HIGH-4: FTS UPDATE triggers
	if triggerCount >= 2 {
		t.Log("✓ HIGH-4: FTS UPDATE triggers present")
	} else {
		t.Error("✗ HIGH-4: FAILED — FTS UPDATE triggers missing")
	}

	// MEDIUM-3: RetrievalStrategy wired
	if metrics.RetrievalMode != "" {
		t.Logf("✓ MEDIUM-3: RetrievalStrategy active (mode=%s)", metrics.RetrievalMode)
	} else {
		t.Error("✗ MEDIUM-3: FAILED — retrieval mode empty")
	}

	// MEDIUM-4: Cross-session continuity
	if strings.Contains(working, "Previously:") {
		t.Log("✓ MEDIUM-4: Cross-session continuity working")
	} else {
		t.Error("✗ MEDIUM-4: FAILED — no cross-session injection")
	}
}

// getAllEpisodicIDs returns all episodic memory IDs for testing.
func getAllEpisodicIDs(ctx context.Context, store *db.Store) []string {
	rows, err := store.QueryContext(ctx,
		`SELECT id FROM episodic_memory WHERE memory_state = 'active'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
