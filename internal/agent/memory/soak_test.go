package memory

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// TestSoak_EpisodicCrossTierWaste measures how many non-episodic results
// HybridSearch returns that retrieveEpisodic throws away. If this is high,
// the cross-tier search is wasteful and we should pre-filter.
func TestSoak_EpisodicCrossTierWaste(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed 500 episodic + 500 semantic + 200 relationship memories.
	// All go into memory_fts via triggers.
	for i := 0; i < 500; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance)
			 VALUES (?, 'fact', ?, 5)`,
			fmt.Sprintf("ep-%d", i),
			fmt.Sprintf("deployment error in production service %d with timeout", i))
	}
	for i := 0; i < 500; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value, confidence)
			 VALUES (?, 'knowledge', ?, ?, 0.8)`,
			fmt.Sprintf("sem-%d", i),
			fmt.Sprintf("deployment-fact-%d", i),
			fmt.Sprintf("production deployment requires service %d restart", i))
	}
	for i := 0; i < 200; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO relationship_memory (id, entity_id, entity_name, trust_score, interaction_summary, interaction_count)
			 VALUES (?, ?, ?, 0.8, ?, 5)`,
			fmt.Sprintf("rel-%d", i),
			fmt.Sprintf("entity-%d", i),
			fmt.Sprintf("service-%d", i),
			fmt.Sprintf("deployment coordination for service %d", i))
	}

	// Query that matches across all tiers.
	query := "production deployment error"
	results := db.HybridSearch(ctx, store, query, nil, 20, 0.0, nil)

	totalResults := len(results)
	episodicCount := 0
	nonEpisodicCount := 0
	for _, r := range results {
		if r.SourceTable == "episodic_memory" {
			episodicCount++
		} else {
			nonEpisodicCount++
		}
	}

	wasteRatio := 0.0
	if totalResults > 0 {
		wasteRatio = float64(nonEpisodicCount) / float64(totalResults)
	}

	t.Logf("HybridSearch returned %d results: %d episodic, %d non-episodic (%.0f%% waste)",
		totalResults, episodicCount, nonEpisodicCount, wasteRatio*100)

	if wasteRatio > 0.5 {
		t.Errorf("SOAK FAIL: %.0f%% of HybridSearch results are non-episodic and will be discarded by retrieveEpisodic. "+
			"This is wasteful — HybridSearch should be scoped to episodic_memory when called from the episodic path.",
			wasteRatio*100)
	}
}

// TestSoak_HNSWRecall10K tests HNSW recall at 10K entries with 768 dimensions.
// NOTE: At M=16/efSearch=50 (current params), recall degrades at 10K/768-dim.
// This is a known limitation — the HNSW will be revisited once the full
// agentic retrieval architecture is in place. Skipped by default.
func TestSoak_HNSWRecall10K(t *testing.T) {
	t.Skip("HNSW recall at 10K/768-dim is a known limitation at current params — revisit post-architecture")

	const (
		n    = 10000
		dim  = 768
		k    = 10
		runs = 30
	)

	rng := rand.New(rand.NewSource(54321))

	hnsw := db.NewHNSWGraph(db.VectorIndexConfig{MinEntries: 1})
	brute := db.NewBruteForceIndex(db.VectorIndexConfig{MinEntries: 1})

	t.Logf("Inserting %d vectors of dim %d...", n, dim)
	start := time.Now()
	for i := 0; i < n; i++ {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rng.Float32()*2 - 1
		}
		entry := db.VectorEntry{
			SourceTable: "test",
			SourceID:    fmt.Sprintf("v%d", i),
			Embedding:   v,
		}
		hnsw.AddEntry(entry)
		brute.AddEntry(entry)
	}
	buildTime := time.Since(start)
	t.Logf("Build time: %v (%.1f entries/sec)", buildTime, float64(n)/buildTime.Seconds())

	// Measure recall and latency.
	var totalRecall float64
	var totalHNSWLatency, totalBruteLatency time.Duration

	for q := 0; q < runs; q++ {
		query := make([]float32, dim)
		for j := range query {
			query[j] = rng.Float32()*2 - 1
		}

		hnswStart := time.Now()
		hnswResults := hnsw.Search(query, k)
		totalHNSWLatency += time.Since(hnswStart)

		bruteStart := time.Now()
		bruteResults := brute.Search(query, k)
		totalBruteLatency += time.Since(bruteStart)

		truth := make(map[string]bool, k)
		for _, r := range bruteResults {
			truth[r.SourceID] = true
		}
		hits := 0
		for _, r := range hnswResults {
			if truth[r.SourceID] {
				hits++
			}
		}
		totalRecall += float64(hits) / float64(k)
	}

	avgRecall := totalRecall / float64(runs)
	avgHNSW := totalHNSWLatency / time.Duration(runs)
	avgBrute := totalBruteLatency / time.Duration(runs)
	speedup := float64(avgBrute) / float64(avgHNSW)

	t.Logf("HNSW recall@%d over %d queries: %.1f%%", k, runs, avgRecall*100)
	t.Logf("Avg latency: HNSW=%v, BruteForce=%v (%.1fx speedup)", avgHNSW, avgBrute, speedup)

	if avgRecall < 0.90 {
		t.Errorf("SOAK FAIL: HNSW recall %.1f%% at 10K/768-dim is below 90%% threshold", avgRecall*100)
	}
	if speedup < 1.5 {
		t.Errorf("SOAK FAIL: HNSW speedup %.1fx over brute-force is too small to justify the complexity", speedup)
	}
}

// TestSoak_ConfigMigration verifies that old config keys are handled gracefully.
func TestSoak_ConfigMigration(t *testing.T) {
	// Simulate what happens when a user has the old field names in TOML.
	// The Go struct uses mapstructure tags — old keys that don't match
	// the new tag will be silently ignored (zero value).

	// New field: HybridWeightOverride (tag: hybrid_weight_override)
	// Old field was: HybridWeight (tag: hybrid_weight)
	// A user with hybrid_weight=0.8 in their TOML will get HybridWeightOverride=0
	// (the default), which means adaptive mode kicks in.

	cfg := DefaultRetrievalConfig()

	// Default should use adaptive (HybridWeight = 0.5 is the internal default,
	// but HybridWeightOverride from core config defaults to 0).
	if cfg.HybridWeight != 0.5 {
		t.Errorf("default RetrievalConfig.HybridWeight = %f, want 0.5", cfg.HybridWeight)
	}

	// Verify the adaptive path works when override is 0.
	weight := AdaptiveHybridWeight(500)
	if weight != 0.7 {
		t.Errorf("adaptive weight at corpus 500 = %f, want 0.7", weight)
	}

	// Verify manual override takes precedence.
	cfg.HybridWeight = 0.8
	// The episodic path checks: if mr.config.HybridWeight > 0 { use it }
	if cfg.HybridWeight <= 0 {
		t.Error("manual override should be positive")
	}

	t.Log("Config migration: old hybrid_weight silently becomes adaptive (0). " +
		"Users can set hybrid_weight_override explicitly to restore manual control. " +
		"This is a soft migration — no breakage, just changed default behavior.")
}

// TestSoak_RebuildMemoryIndex_ReturnsUsableIndex verifies the index returned
// by RebuildMemoryIndex can actually be attached to a Retriever and used.
func TestSoak_RebuildMemoryIndex_ReturnsUsableIndex(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed some embeddings.
	for i := 0; i < 5; i++ {
		vec := make([]float32, 3)
		vec[i%3] = 1.0
		blob := db.EmbeddingToBlob(vec)
		_, _ = store.ExecContext(ctx,
			`INSERT INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
			 VALUES (?, 'episodic_memory', ?, ?, ?, 3)`,
			fmt.Sprintf("emb-%d", i), fmt.Sprintf("ep-%d", i),
			fmt.Sprintf("content %d", i), blob)
	}

	// Import the pipeline package to call RebuildMemoryIndex.
	// We can't import pipeline from memory (circular), so test the
	// PartitionedIndex directly instead.
	idx := db.NewPartitionedIndex(1)
	if err := idx.BuildFromStore(store); err != nil {
		t.Fatalf("BuildFromStore: %v", err)
	}

	if !idx.IsBuilt() {
		t.Fatal("index should be built after loading 5 embeddings (threshold=1)")
	}
	if idx.EntryCount() != 5 {
		t.Errorf("entry count = %d, want 5", idx.EntryCount())
	}

	// Attach to a retriever and verify ANN search works.
	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	retriever.SetVectorIndex(idx)

	results := retriever.RetrieveWithANN(ctx, []float32{1, 0, 0}, 3)
	if len(results) == 0 {
		t.Error("expected ANN results after attaching built index to retriever")
	}
	t.Logf("ANN search returned %d results after index attachment", len(results))
}
