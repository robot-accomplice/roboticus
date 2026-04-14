package memory

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// seedSyntheticCorpus inserts n episodic memories with FTS entries and embeddings.
func seedSyntheticCorpus(t *testing.T, store *db.Store, n int) {
	t.Helper()
	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))

	words := []string{
		"alpha", "bravo", "charlie", "delta", "echo",
		"foxtrot", "golf", "hotel", "india", "juliet",
		"kilo", "lima", "mike", "november", "oscar",
		"papa", "quebec", "romeo", "sierra", "tango",
	}

	for i := 0; i < n; i++ {
		id := fmt.Sprintf("ep-%d", i)
		// Generate semi-random content with a few overlapping keywords.
		content := fmt.Sprintf("%s %s %s interaction number %d",
			words[rng.Intn(len(words))],
			words[rng.Intn(len(words))],
			words[rng.Intn(len(words))],
			i)

		_, err := store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance)
			 VALUES (?, 'fact', ?, 5)`, id, content)
		if err != nil {
			t.Fatalf("seedSyntheticCorpus entry %d: %v", i, err)
		}

		// Insert a memory_index entry for corpus size estimation.
		_, _ = store.ExecContext(ctx,
			`INSERT INTO memory_index (id, source_table, source_id, summary, confidence)
			 VALUES (?, 'episodic_memory', ?, ?, 0.8)`,
			fmt.Sprintf("mi-%d", i), id, content)
	}
}

func TestCollapseRegression_100(t *testing.T) {
	collapseRegressionAtScale(t, 100)
}

func TestCollapseRegression_1K(t *testing.T) {
	collapseRegressionAtScale(t, 1000)
}

func collapseRegressionAtScale(t *testing.T, n int) {
	store := testutil.TempStore(t)
	seedSyntheticCorpus(t, store, n)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)

	// Create a session for the retriever.
	ctx := context.Background()
	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	_, metrics := retriever.RetrieveWithMetrics(ctx, sessionID, "alpha bravo charlie", 4096)

	// CorpusSize should reflect the seeded data.
	if metrics.CorpusSize < n/2 {
		t.Errorf("corpus size %d seems too low for %d seeded entries", metrics.CorpusSize, n)
	}

	// HybridWeight should be adaptive based on corpus size.
	expectedWeight := AdaptiveHybridWeight(metrics.CorpusSize)
	if metrics.HybridWeight != expectedWeight {
		t.Errorf("hybrid weight = %f, want %f for corpus size %d",
			metrics.HybridWeight, expectedWeight, metrics.CorpusSize)
	}

	// RetrievalMode should be set.
	if metrics.RetrievalMode == "" {
		t.Error("retrieval mode should be populated")
	}

	t.Logf("scale=%d corpus=%d weight=%.2f spread=%.4f mode=%s episodic=%d",
		n, metrics.CorpusSize, metrics.HybridWeight, metrics.ScoreSpread,
		metrics.RetrievalMode, metrics.EpisodicCount)
}

func TestCollapseRegression_AdaptiveWeightMatchesCorpus(t *testing.T) {
	// Verify the adaptive weight function produces the expected value
	// at each scale boundary.
	tests := []struct {
		corpus int
		want   float64
	}{
		{500, 0.7},
		{2000, 0.5},
		{7000, 0.4},
		{20000, 0.3},
	}
	for _, tt := range tests {
		got := AdaptiveHybridWeight(tt.corpus)
		if got != tt.want {
			t.Errorf("AdaptiveHybridWeight(%d) = %f, want %f", tt.corpus, got, tt.want)
		}
	}
}
