package memory

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// --- Original Consolidator (Jaccard dedup) tests ---

func TestConsolidator_SingleEntryPassthrough(t *testing.T) {
	c := NewConsolidator(0.5)
	entries := []ConsolidationEntry{{ID: "1", Content: "hello world", Category: "fact", Score: 1.0}}
	result := c.Consolidate(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("expected ID '1', got %q", result[0].ID)
	}
}

func TestConsolidator_TwoSimilarEntriesMerged(t *testing.T) {
	c := NewConsolidator(0.5)
	entries := []ConsolidationEntry{
		{ID: "1", Content: "the cat sat on the mat", Category: "fact", Score: 0.8},
		{ID: "2", Content: "the cat sat on the mat today", Category: "fact", Score: 0.9},
	}
	result := c.Consolidate(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged entry, got %d", len(result))
	}
	if result[0].Score != 0.9 {
		t.Errorf("expected merged score 0.9, got %f", result[0].Score)
	}
}

func TestConsolidator_DifferentCategoriesNotMerged(t *testing.T) {
	c := NewConsolidator(0.3)
	entries := []ConsolidationEntry{
		{ID: "1", Content: "the cat sat on the mat", Category: "episodic", Score: 0.8},
		{ID: "2", Content: "the cat sat on the mat", Category: "semantic", Score: 0.9},
	}
	result := c.Consolidate(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries (different categories), got %d", len(result))
	}
}

func TestConsolidator_DissimilarEntriesNotMerged(t *testing.T) {
	c := NewConsolidator(0.8)
	entries := []ConsolidationEntry{
		{ID: "1", Content: "apple banana cherry", Category: "fact", Score: 0.5},
		{ID: "2", Content: "dog cat mouse", Category: "fact", Score: 0.6},
	}
	result := c.Consolidate(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries (below similarity threshold), got %d", len(result))
	}
}

func TestConsolidator_EmptyInput(t *testing.T) {
	c := NewConsolidator(0.5)
	result := c.Consolidate(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}
}

func TestJaccardSimilarity_Boundaries(t *testing.T) {
	if s := jaccardSimilarity("hello world", "hello world"); s != 1.0 {
		t.Errorf("identical strings should have similarity 1.0, got %f", s)
	}
	if s := jaccardSimilarity("alpha beta", "gamma delta"); s != 0.0 {
		t.Errorf("disjoint strings should have similarity 0.0, got %f", s)
	}
	if s := jaccardSimilarity("", ""); s != 1.0 {
		t.Errorf("two empty strings should have similarity 1.0, got %f", s)
	}
	s := jaccardSimilarity("a b c", "b c d")
	if s <= 0 || s >= 1 {
		t.Errorf("partial overlap should be between 0 and 1, got %f", s)
	}
}

func TestConsolidator_MultipleGroupsMerged(t *testing.T) {
	c := NewConsolidator(0.5)
	entries := []ConsolidationEntry{
		{ID: "1", Content: "the cat sat on the mat", Category: "fact", Score: 0.5},
		{ID: "2", Content: "the cat sat on the mat", Category: "fact", Score: 0.9},
		{ID: "3", Content: "totally unrelated content here", Category: "fact", Score: 0.7},
	}
	result := c.Consolidate(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result))
	}
}

// --- ConsolidationPipeline tests ---

func seedEpisodic(t *testing.T, store *db.Store, id, classification, content string, importance int, createdAt string) {
	t.Helper()
	ctx := context.Background()
	_, err := store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state, created_at)
		 VALUES (?, ?, ?, ?, 'active', ?)`,
		id, classification, content, importance, createdAt)
	if err != nil {
		t.Fatalf("seed episodic: %v", err)
	}
}

func seedSemantic(t *testing.T, store *db.Store, id, category, key, value string, confidence float64, updatedAt string) {
	t.Helper()
	ctx := context.Background()
	_, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'active', ?)`,
		id, category, key, value, confidence, updatedAt)
	if err != nil {
		t.Fatalf("seed semantic: %v", err)
	}
}

func seedEmbedding(t *testing.T, store *db.Store, id, sourceTable, sourceID string) {
	t.Helper()
	ctx := context.Background()
	_, err := store.ExecContext(ctx,
		`INSERT INTO embeddings (id, source_table, source_id, content_preview)
		 VALUES (?, ?, ?, 'preview')`,
		id, sourceTable, sourceID)
	if err != nil {
		t.Fatalf("seed embedding: %v", err)
	}
}

func nowStr() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func daysAgo(n int) string {
	return time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour).Format("2006-01-02 15:04:05")
}

func TestPipeline_GatingPreventsFrequentRuns(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = time.Hour

	// First run should proceed.
	r1 := pipe.Run(ctx, store)
	// Record was created, so second run should be gated.
	r2 := pipe.Run(ctx, store)

	// Both reports should be zero (no data), but second run should have been skipped.
	// Verify by checking consolidation_log has exactly 1 row.
	var count int
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM consolidation_log`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 consolidation_log entry, got %d", count)
	}

	_ = r1
	_ = r2
}

func TestPipeline_GatingAllowsRunAfterInterval(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0 // No gating.

	pipe.Run(ctx, store)
	pipe.Run(ctx, store)

	var count int
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM consolidation_log`).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 consolidation_log entries with no gating, got %d", count)
	}
}

func TestPipeline_Phase1_IndexBackfill(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	seedEpisodic(t, store, "ep1", "fact", "the sky is blue", 5, nowStr())
	seedSemantic(t, store, "sem1", "knowledge", "color-sky", "the sky is blue", 0.8, nowStr())

	r := pipe.Run(ctx, store)
	if r.Indexed != 2 {
		t.Errorf("expected 2 indexed, got %d", r.Indexed)
	}

	// Running again should index 0 (already indexed).
	r2 := pipe.Run(ctx, store)
	if r2.Indexed != 0 {
		t.Errorf("expected 0 re-indexed, got %d", r2.Indexed)
	}
}

func TestPipeline_Phase2_WithinTierDedup(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Two very similar episodic entries within the same tier.
	seedEpisodic(t, store, "ep1", "fact", "the cat sat on the mat in the sun", 5, nowStr())
	seedEpisodic(t, store, "ep2", "fact", "the cat sat on the mat in the sun today", 7, nowStr())

	r := pipe.Run(ctx, store)
	if r.Deduped != 1 {
		t.Errorf("expected 1 deduped, got %d", r.Deduped)
	}

	// The lower-scored entry (ep1, importance=5) should be deduped.
	var state string
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep1'`).Scan(&state)
	if state != "deduped" {
		t.Errorf("expected ep1 state 'deduped', got %q", state)
	}

	// The higher-scored entry (ep2) should remain active.
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep2'`).Scan(&state)
	if state != "active" {
		t.Errorf("expected ep2 state 'active', got %q", state)
	}
}

func TestPipeline_Phase2_CrossTierNotDeduped(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Cross-tier similar entries should NOT be deduped (within-tier only).
	seedEpisodic(t, store, "ep1", "fact", "the cat sat on the mat in the sun", 5, nowStr())
	seedSemantic(t, store, "sem1", "fact", "cat-mat", "the cat sat on the mat in the sun today", 0.8, nowStr())

	r := pipe.Run(ctx, store)
	if r.Deduped != 0 {
		t.Errorf("expected 0 deduped (cross-tier should not dedup), got %d", r.Deduped)
	}

	// Both should remain active.
	var state string
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep1'`).Scan(&state)
	if state != "active" {
		t.Errorf("expected episodic state 'active', got %q", state)
	}
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM semantic_memory WHERE id = 'sem1'`).Scan(&state)
	if state != "active" {
		t.Errorf("expected semantic state 'active', got %q", state)
	}
}

func TestPipeline_Phase3_EpisodicPromotion(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// 3 episodic entries similar enough for promotion (Jaccard > 0.5) but
	// different enough to avoid within-tier dedup (Jaccard < 0.85).
	// Each pair shares 5 words and has 2 unique => Jaccard ~0.556.
	seedEpisodic(t, store, "ep1", "tool_event", "user asked about weather forecast details report", 5, nowStr())
	seedEpisodic(t, store, "ep2", "tool_event", "user asked about weather forecast temperature update", 5, nowStr())
	seedEpisodic(t, store, "ep3", "tool_event", "user asked about weather forecast wind conditions", 5, nowStr())

	r := pipe.Run(ctx, store)
	if r.Promoted != 1 {
		t.Errorf("expected 1 promotion, got %d", r.Promoted)
	}

	// Verify episodic entries are marked promoted.
	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE memory_state = 'promoted'`).Scan(&count)
	if count != 3 {
		t.Errorf("expected 3 promoted episodic entries, got %d", count)
	}

	// Verify a semantic entry was created.
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE state_reason = 'promoted from episodic'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 semantic entry from promotion, got %d", count)
	}
}

func TestPipeline_Phase3_NoPromotionUnder3(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Only 2 similar entries: should NOT trigger promotion.
	seedEpisodic(t, store, "ep1", "tool_event", "user asked about weather", 5, nowStr())
	seedEpisodic(t, store, "ep2", "tool_event", "user asked about weather today", 5, nowStr())

	r := pipe.Run(ctx, store)
	if r.Promoted != 0 {
		t.Errorf("expected 0 promotions with only 2 similar entries, got %d", r.Promoted)
	}
}

func TestPipeline_Phase4_ConfidenceDecay(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Entry last updated 10 days ago.
	seedSemantic(t, store, "sem1", "knowledge", "old-fact", "some old knowledge", 0.8, daysAgo(10))

	r := pipe.Run(ctx, store)
	if r.ConfidenceDecayed != 1 {
		t.Errorf("expected 1 confidence decayed, got %d", r.ConfidenceDecayed)
	}

	// Check new confidence: 0.8 * 0.995 = 0.796 (constant multiplier, one decay step per consolidation pass)
	var conf float64
	_ = store.QueryRowContext(ctx, `SELECT confidence FROM semantic_memory WHERE id = 'sem1'`).Scan(&conf)
	if conf >= 0.8 {
		t.Errorf("confidence should have decayed below 0.8, got %f", conf)
	}
	if conf < 0.1 {
		t.Errorf("confidence should not drop below 0.1 floor, got %f", conf)
	}
}

func TestPipeline_Phase4_ConfidenceFloor(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Entry updated 200 days ago with low confidence.
	seedSemantic(t, store, "sem1", "knowledge", "very-old", "ancient wisdom", 0.2, daysAgo(200))

	pipe.Run(ctx, store)

	var conf float64
	_ = store.QueryRowContext(ctx, `SELECT confidence FROM semantic_memory WHERE id = 'sem1'`).Scan(&conf)
	if conf < 0.1 {
		t.Errorf("confidence should not drop below floor 0.1, got %f", conf)
	}
}

func TestPipeline_Phase5_ImportanceDecay(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Entry created 21 days ago with importance 5.
	seedEpisodic(t, store, "ep1", "fact", "old event", 5, daysAgo(21))

	r := pipe.Run(ctx, store)
	if r.ImportanceDecayed != 1 {
		t.Errorf("expected 1 importance decayed, got %d", r.ImportanceDecayed)
	}

	var imp int
	_ = store.QueryRowContext(ctx, `SELECT importance FROM episodic_memory WHERE id = 'ep1'`).Scan(&imp)
	// 21 days old, 14 days past threshold, ~2 weeks = importance - 2 = 3
	if imp >= 5 {
		t.Errorf("importance should have decayed below 5, got %d", imp)
	}
	if imp < 1 {
		t.Errorf("importance should not drop below 1, got %d", imp)
	}
}

func TestPipeline_Phase5_ImportanceNoDecayUnder7Days(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	seedEpisodic(t, store, "ep1", "fact", "recent event", 5, daysAgo(3))

	r := pipe.Run(ctx, store)
	if r.ImportanceDecayed != 0 {
		t.Errorf("expected 0 decayed for recent entry, got %d", r.ImportanceDecayed)
	}
}

func TestPipeline_Phase6_PruneSemantic(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	seedSemantic(t, store, "sem1", "knowledge", "weak-fact", "barely known", 0.12, nowStr())

	r := pipe.Run(ctx, store)
	if r.Pruned < 1 {
		t.Errorf("expected at least 1 pruned, got %d", r.Pruned)
	}

	var state string
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM semantic_memory WHERE id = 'sem1'`).Scan(&state)
	if state != "pruned" {
		t.Errorf("expected state 'pruned', got %q", state)
	}
}

func TestPipeline_Phase6_PruneEpisodic(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Old, low-importance entry.
	seedEpisodic(t, store, "ep1", "fact", "ancient trivia", 1, daysAgo(45))

	r := pipe.Run(ctx, store)
	if r.Pruned < 1 {
		t.Errorf("expected at least 1 pruned, got %d", r.Pruned)
	}

	var state string
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep1'`).Scan(&state)
	if state != "pruned" {
		t.Errorf("expected state 'pruned', got %q", state)
	}
}

func TestPipeline_Phase6_NoPruneRecentLowImportance(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Low importance but recent: should NOT be pruned.
	seedEpisodic(t, store, "ep1", "fact", "recent low importance", 1, daysAgo(5))

	r := pipe.Run(ctx, store)

	var state string
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep1'`).Scan(&state)
	if state != "active" {
		t.Errorf("recent low-importance entry should remain active, got %q", state)
	}
	_ = r
}

func TestPipeline_Phase7_OrphanCleanup(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Create an embedding pointing to a non-existent episodic entry.
	seedEmbedding(t, store, "emb1", "episodic", "nonexistent-id")

	// Also create a valid episodic + embedding pair.
	seedEpisodic(t, store, "ep-real", "fact", "real entry", 5, nowStr())
	seedEmbedding(t, store, "emb2", "episodic", "ep-real")

	r := pipe.Run(ctx, store)
	if r.Orphaned != 1 {
		t.Errorf("expected 1 orphaned embedding, got %d", r.Orphaned)
	}

	// Verify the orphan was deleted.
	var count int
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings WHERE id = 'emb1'`).Scan(&count)
	if count != 0 {
		t.Errorf("orphan embedding should be deleted, found %d", count)
	}

	// Verify the valid embedding remains.
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings WHERE id = 'emb2'`).Scan(&count)
	if count != 1 {
		t.Errorf("valid embedding should remain, found %d", count)
	}
}

func TestPipeline_FullRun_Integration(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Seed a variety of data for a full integration run.
	seedEpisodic(t, store, "ep1", "fact", "the weather is sunny and warm", 5, nowStr())
	seedEpisodic(t, store, "ep2", "fact", "old forgotten trivia", 1, daysAgo(45))
	seedSemantic(t, store, "sem1", "knowledge", "weather", "current weather patterns", 0.8, nowStr())
	seedSemantic(t, store, "sem2", "knowledge", "low-conf", "barely remembered", 0.12, nowStr())
	seedEmbedding(t, store, "emb-orphan", "semantic", "does-not-exist")

	r := pipe.Run(ctx, store)

	// At minimum: indexing, pruning, and orphan cleanup should fire.
	if r.Indexed < 1 {
		t.Errorf("expected at least 1 indexed, got %d", r.Indexed)
	}
	if r.Pruned < 1 {
		t.Errorf("expected at least 1 pruned, got %d", r.Pruned)
	}
	if r.Orphaned < 1 {
		t.Errorf("expected at least 1 orphaned, got %d", r.Orphaned)
	}
}
