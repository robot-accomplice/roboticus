package db

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"testing"
)

// randomVec generates a random unit-ish vector of the given dimension.
func randomVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()*2 - 1 // [-1, 1]
	}
	return v
}

func TestHNSWGraph_Basic(t *testing.T) {
	idx := NewHNSWGraph(VectorIndexConfig{MinEntries: 2})

	if idx.IsBuilt() {
		t.Error("should not be built initially")
	}
	if idx.EntryCount() != 0 {
		t.Errorf("entry count = %d, want 0", idx.EntryCount())
	}

	idx.AddEntry(VectorEntry{
		SourceTable: "test", SourceID: "a",
		Embedding: []float32{1, 0, 0},
	})
	if idx.IsBuilt() {
		t.Error("should not be built with 1 entry (min=2)")
	}

	idx.AddEntry(VectorEntry{
		SourceTable: "test", SourceID: "b",
		Embedding: []float32{0, 1, 0},
	})
	if !idx.IsBuilt() {
		t.Error("should be built with 2 entries (min=2)")
	}
	if idx.EntryCount() != 2 {
		t.Errorf("entry count = %d, want 2", idx.EntryCount())
	}
}

func TestHNSWGraph_ExactMatch(t *testing.T) {
	idx := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})

	idx.AddEntry(VectorEntry{
		SourceTable: "test", SourceID: "a",
		Embedding: []float32{1, 0, 0},
	})
	idx.AddEntry(VectorEntry{
		SourceTable: "test", SourceID: "b",
		Embedding: []float32{0, 1, 0},
	})
	idx.AddEntry(VectorEntry{
		SourceTable: "test", SourceID: "c",
		Embedding: []float32{0.9, 0.1, 0},
	})

	results := idx.Search([]float32{1, 0, 0}, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].SourceID != "a" {
		t.Errorf("expected 'a' as closest, got %s", results[0].SourceID)
	}
	if results[0].Similarity < 0.99 {
		t.Errorf("expected similarity ~1.0, got %f", results[0].Similarity)
	}
}

func TestHNSWGraph_SearchEmpty(t *testing.T) {
	idx := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})
	results := idx.Search([]float32{1, 0, 0}, 5)
	if results != nil {
		t.Errorf("expected nil for empty index, got %v", results)
	}
}

func TestHNSWGraph_SearchKZero(t *testing.T) {
	idx := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})
	idx.AddEntry(VectorEntry{Embedding: []float32{1, 0}})
	results := idx.Search([]float32{1, 0}, 0)
	if results != nil {
		t.Errorf("expected nil for k=0, got %v", results)
	}
}

func TestHNSWGraph_RecallVsBruteForce(t *testing.T) {
	// Insert 1000 random 64-dim vectors (smaller dims for test speed),
	// then verify HNSW recall >= 90% vs brute-force ground truth at k=10.
	const (
		n    = 1000
		dim  = 64
		k    = 10
		runs = 20
	)

	rng := rand.New(rand.NewSource(12345))

	hnsw := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})
	brute := NewBruteForceIndex(VectorIndexConfig{MinEntries: 1})

	for i := 0; i < n; i++ {
		v := randomVec(rng, dim)
		entry := VectorEntry{
			SourceTable: "test",
			SourceID:    fmt.Sprintf("v%d", i),
			Embedding:   v,
		}
		hnsw.AddEntry(entry)
		brute.AddEntry(entry)
	}

	totalRecall := 0.0
	for q := 0; q < runs; q++ {
		query := randomVec(rng, dim)
		hnswResults := hnsw.Search(query, k)
		bruteResults := brute.Search(query, k)

		// Build ground truth set.
		truth := make(map[string]bool, k)
		for _, r := range bruteResults {
			truth[r.SourceID] = true
		}

		// Count HNSW hits in ground truth.
		hits := 0
		for _, r := range hnswResults {
			if truth[r.SourceID] {
				hits++
			}
		}
		totalRecall += float64(hits) / float64(k)
	}

	avgRecall := totalRecall / float64(runs)
	t.Logf("HNSW recall@%d over %d queries: %.1f%%", k, runs, avgRecall*100)
	if avgRecall < 0.90 {
		t.Errorf("HNSW recall %.1f%% is below 90%% threshold", avgRecall*100)
	}
}

func TestHNSWGraph_RecallHighDim(t *testing.T) {
	// Test with more realistic 768-dim vectors but fewer entries for speed.
	const (
		n    = 200
		dim  = 768
		k    = 5
		runs = 10
	)

	rng := rand.New(rand.NewSource(99999))

	hnsw := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})
	brute := NewBruteForceIndex(VectorIndexConfig{MinEntries: 1})

	for i := 0; i < n; i++ {
		v := randomVec(rng, dim)
		entry := VectorEntry{
			SourceTable: "test",
			SourceID:    fmt.Sprintf("v%d", i),
			Embedding:   v,
		}
		hnsw.AddEntry(entry)
		brute.AddEntry(entry)
	}

	totalRecall := 0.0
	for q := 0; q < runs; q++ {
		query := randomVec(rng, dim)
		hnswResults := hnsw.Search(query, k)
		bruteResults := brute.Search(query, k)

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
	t.Logf("HNSW recall@%d (768-dim, %d entries): %.1f%%", k, n, avgRecall*100)
	if avgRecall < 0.85 {
		t.Errorf("HNSW 768-dim recall %.1f%% is below 85%% threshold", avgRecall*100)
	}
}

func TestHNSWGraph_ConcurrentReadWrite(t *testing.T) {
	idx := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})
	rng := rand.New(rand.NewSource(42))

	// Pre-populate with some entries.
	for i := 0; i < 50; i++ {
		idx.AddEntry(VectorEntry{
			SourceTable: "test",
			SourceID:    fmt.Sprintf("init-%d", i),
			Embedding:   randomVec(rng, 32),
		})
	}

	var wg sync.WaitGroup
	// Concurrent writers.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localRng := rand.New(rand.NewSource(int64(id * 1000)))
			for i := 0; i < 25; i++ {
				idx.AddEntry(VectorEntry{
					SourceTable: "test",
					SourceID:    fmt.Sprintf("w%d-%d", id, i),
					Embedding:   randomVec(localRng, 32),
				})
			}
		}(w)
	}
	// Concurrent readers.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localRng := rand.New(rand.NewSource(int64(id * 2000)))
			for i := 0; i < 25; i++ {
				q := randomVec(localRng, 32)
				results := idx.Search(q, 5)
				_ = results
			}
		}(r)
	}
	wg.Wait()

	// Should have 50 + 4*25 = 150 entries.
	if idx.EntryCount() != 150 {
		t.Errorf("expected 150 entries after concurrent ops, got %d", idx.EntryCount())
	}
}

func TestHNSWGraph_SimilarityOrder(t *testing.T) {
	idx := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})

	// Insert vectors at known angles.
	idx.AddEntry(VectorEntry{SourceTable: "t", SourceID: "exact", Embedding: []float32{1, 0, 0}})
	idx.AddEntry(VectorEntry{SourceTable: "t", SourceID: "close", Embedding: []float32{0.95, 0.05, 0}})
	idx.AddEntry(VectorEntry{SourceTable: "t", SourceID: "medium", Embedding: []float32{0.5, 0.5, 0}})
	idx.AddEntry(VectorEntry{SourceTable: "t", SourceID: "far", Embedding: []float32{0, 1, 0}})

	results := idx.Search([]float32{1, 0, 0}, 4)
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// Verify descending similarity order.
	for i := 1; i < len(results); i++ {
		if results[i].Similarity > results[i-1].Similarity+1e-9 {
			t.Errorf("results not in descending order: [%d]=%f > [%d]=%f",
				i, results[i].Similarity, i-1, results[i-1].Similarity)
		}
	}

	if results[0].SourceID != "exact" {
		t.Errorf("expected 'exact' as top result, got %s", results[0].SourceID)
	}
}

func TestHNSWGraph_SingleEntry(t *testing.T) {
	idx := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})
	idx.AddEntry(VectorEntry{SourceTable: "t", SourceID: "only", Embedding: []float32{1, 0}})

	results := idx.Search([]float32{0, 1}, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for single-entry index, got %d", len(results))
	}
	if results[0].SourceID != "only" {
		t.Errorf("expected 'only', got %s", results[0].SourceID)
	}
}

func TestHNSWGraph_CosineSimilarityF32_ZeroVector(t *testing.T) {
	sim := CosineSimilarityF32([]float32{0, 0, 0}, []float32{1, 0, 0})
	if sim != 0 {
		t.Errorf("expected 0 for zero vector, got %f", sim)
	}
}

func TestHNSWGraph_CosineSimilarityF32_Identical(t *testing.T) {
	sim := CosineSimilarityF32([]float32{1, 2, 3}, []float32{1, 2, 3})
	if math.Abs(sim-1.0) > 1e-6 {
		t.Errorf("expected ~1.0 for identical vectors, got %f", sim)
	}
}
