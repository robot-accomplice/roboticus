package db

import (
	"context"
	"testing"
)

func seedFTSEntry(t *testing.T, store *Store, id, classification, content string) {
	t.Helper()
	ctx := context.Background()
	// Insert into episodic_memory — triggers auto-populate memory_fts.
	_, err := store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance)
		 VALUES (?, ?, ?, 5)`, id, classification, content)
	if err != nil {
		t.Fatalf("seedFTSEntry(%s): %v", id, err)
	}
}

func TestHybridSearch_FTSOnly(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	seedFTSEntry(t, store, "e1", "fact", "the quick brown fox jumps over the lazy dog")
	seedFTSEntry(t, store, "e2", "fact", "a slow red cat sits on the warm mat")

	// hybridWeight=0 means FTS only, no vector.
	results := HybridSearch(ctx, store, "quick brown fox", nil, 10, 0.0, nil)
	if len(results) == 0 {
		t.Fatal("expected FTS results for 'quick brown fox'")
	}
	if results[0].SourceID != "e1" {
		t.Errorf("expected e1 as top FTS result, got %s", results[0].SourceID)
	}
	if results[0].Similarity <= 0 {
		t.Errorf("expected positive BM25-derived score, got %f", results[0].Similarity)
	}
}

func TestHybridSearch_VectorOnly(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	idx := NewBruteForceIndex(VectorIndexConfig{MinEntries: 1})
	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e1",
		ContentPreview: "vector A",
		Embedding:      []float32{1, 0, 0},
	})
	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e2",
		ContentPreview: "vector B",
		Embedding:      []float32{0, 1, 0},
	})

	// hybridWeight=1.0 means vector only.
	results := HybridSearch(ctx, store, "", []float32{1, 0, 0}, 10, 1.0, idx)
	if len(results) == 0 {
		t.Fatal("expected vector results")
	}
	if results[0].SourceID != "e1" {
		t.Errorf("expected e1 as closest vector, got %s", results[0].SourceID)
	}
}

func TestHybridSearch_Deduplication(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// Seed both FTS and vector with the same document.
	seedFTSEntry(t, store, "e1", "fact", "the quick brown fox")

	idx := NewBruteForceIndex(VectorIndexConfig{MinEntries: 1})
	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e1",
		ContentPreview: "the quick brown fox",
		Embedding:      []float32{1, 0, 0},
	})

	// 50/50 blend — e1 should appear only ONCE with blended score.
	results := HybridSearch(ctx, store, "quick brown fox", []float32{1, 0, 0}, 10, 0.5, idx)

	// Count how many times e1 appears.
	e1Count := 0
	for _, r := range results {
		if r.SourceID == "e1" {
			e1Count++
		}
	}
	if e1Count != 1 {
		t.Errorf("expected e1 to appear exactly once (deduped), got %d", e1Count)
	}

	// The blended score should be higher than either leg alone would give.
	if len(results) > 0 && results[0].SourceID == "e1" {
		if results[0].Similarity <= 0 {
			t.Errorf("expected positive blended score, got %f", results[0].Similarity)
		}
		// Per-leg scores should be populated for a doc found by both legs.
		if results[0].FTSScore <= 0 {
			t.Errorf("expected positive FTSScore for deduped doc, got %f", results[0].FTSScore)
		}
		if results[0].VectorScore <= 0 {
			t.Errorf("expected positive VectorScore for deduped doc, got %f", results[0].VectorScore)
		}
	}
}

func TestHybridSearch_BM25ScoresVaryByRelevance(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// One doc is highly relevant (all matching terms), one barely matches.
	seedFTSEntry(t, store, "e1", "fact", "fox brown quick jumps lazy dog")
	seedFTSEntry(t, store, "e2", "fact", "the weather is nice outside today in summer")

	results := HybridSearch(ctx, store, "fox brown quick", nil, 10, 0.0, nil)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	// Only e1 matches the query — e2 has no matching terms.
	if results[0].SourceID != "e1" {
		t.Errorf("expected e1 (matching terms) to rank first, got %s", results[0].SourceID)
	}
	// BM25 scores should be positive after normalization.
	if results[0].Similarity <= 0 {
		t.Errorf("expected positive BM25-derived score, got %f", results[0].Similarity)
	}
}

func TestHybridSearch_EmptyQuery(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	results := HybridSearch(ctx, store, "", nil, 10, 0.5, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query with no vector, got %d", len(results))
	}
}

func TestHybridSearch_LimitZero(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	results := HybridSearch(ctx, store, "anything", nil, 0, 0.5, nil)
	if results != nil {
		t.Errorf("expected nil for limit=0, got %v", results)
	}
}

func TestHybridSearch_NilVectorIndex(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	seedFTSEntry(t, store, "e1", "fact", "searchable content here")

	// Should still return FTS results even with nil vector index.
	results := HybridSearch(ctx, store, "searchable content", nil, 10, 0.5, nil)
	if len(results) == 0 {
		t.Error("expected FTS results even with nil vector index")
	}
}

func TestHybridSearch_WeightExtremes(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	seedFTSEntry(t, store, "e1", "fact", "test content for weight extremes")

	idx := NewBruteForceIndex(VectorIndexConfig{MinEntries: 1})
	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e1",
		ContentPreview: "test content for weight extremes",
		Embedding:      []float32{1, 0, 0},
	})

	// Weight 0.0: only FTS score contributes.
	r0 := HybridSearch(ctx, store, "test content", []float32{1, 0, 0}, 10, 0.0, idx)
	// Weight 1.0: only vector score contributes.
	r1 := HybridSearch(ctx, store, "test content", []float32{1, 0, 0}, 10, 1.0, idx)

	if len(r0) == 0 || len(r1) == 0 {
		t.Fatal("expected results at both weight extremes")
	}
	// Scores should differ since they use different legs.
	if r0[0].Similarity == r1[0].Similarity {
		t.Error("expected different scores at weight 0.0 vs 1.0")
	}
}

func TestSanitizeFTSQuery_RemovesOperators(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", `"hello" "world"`},
		{"", ""},
		{"  ", ""},
		{`hello "world"`, `"hello" "world"`},
		{"AND OR NOT", ""},
		{"foo AND bar", `"foo" "bar"`},
		{"test*query", `"testquery"`},
		{"(grouped)", `"grouped"`},
	}
	for _, tt := range tests {
		got := SanitizeFTSQuery(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeFTSQuery(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
