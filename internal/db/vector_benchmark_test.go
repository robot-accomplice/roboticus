package db

import (
	"fmt"
	"math/rand"
	"testing"
)

// benchmarkSearch inserts n random vectors of given dim, then benchmarks Search(k).
func benchmarkSearch(b *testing.B, idx VectorIndex, n, dim, k int) {
	b.Helper()
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rng.Float32()*2 - 1
		}
		idx.AddEntry(VectorEntry{
			SourceTable: "bench",
			SourceID:    fmt.Sprintf("v%d", i),
			Embedding:   v,
		})
	}
	query := make([]float32, dim)
	for j := range query {
		query[j] = rng.Float32()*2 - 1
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Search(query, k)
	}
}

func BenchmarkBruteForce_1K(b *testing.B) {
	benchmarkSearch(b, NewBruteForceIndex(VectorIndexConfig{MinEntries: 1}), 1000, 768, 10)
}

func BenchmarkBruteForce_10K(b *testing.B) {
	benchmarkSearch(b, NewBruteForceIndex(VectorIndexConfig{MinEntries: 1}), 10000, 768, 10)
}

func BenchmarkHNSW_1K(b *testing.B) {
	benchmarkSearch(b, NewHNSWGraph(VectorIndexConfig{MinEntries: 1}), 1000, 768, 10)
}

func BenchmarkHNSW_10K(b *testing.B) {
	benchmarkSearch(b, NewHNSWGraph(VectorIndexConfig{MinEntries: 1}), 10000, 768, 10)
}

func BenchmarkPartitioned_1K(b *testing.B) {
	idx := NewPartitionedIndex(1)
	rng := rand.New(rand.NewSource(42))
	tables := []string{"episodic_memory", "semantic_memory"}
	for i := 0; i < 1000; i++ {
		v := make([]float32, 768)
		for j := range v {
			v[j] = rng.Float32()*2 - 1
		}
		idx.AddEntry(VectorEntry{
			SourceTable: tables[i%2],
			SourceID:    fmt.Sprintf("v%d", i),
			Embedding:   v,
		})
	}
	query := make([]float32, 768)
	for j := range query {
		query[j] = rng.Float32()*2 - 1
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Search(query, 10)
	}
}

// BenchmarkRecall measures HNSW recall@10 vs brute-force at different scales.
func BenchmarkRecall_1K(b *testing.B) {
	benchmarkRecall(b, 1000, 768, 10)
}

func benchmarkRecall(b *testing.B, n, dim, k int) {
	rng := rand.New(rand.NewSource(12345))

	hnsw := NewHNSWGraph(VectorIndexConfig{MinEntries: 1})
	brute := NewBruteForceIndex(VectorIndexConfig{MinEntries: 1})

	for i := 0; i < n; i++ {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rng.Float32()*2 - 1
		}
		entry := VectorEntry{SourceTable: "bench", SourceID: fmt.Sprintf("v%d", i), Embedding: v}
		hnsw.AddEntry(entry)
		brute.AddEntry(entry)
	}

	queries := make([][]float32, 20)
	for q := range queries {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rng.Float32()*2 - 1
		}
		queries[q] = v
	}

	b.ResetTimer()
	totalRecall := 0.0
	for i := 0; i < b.N; i++ {
		query := queries[i%len(queries)]
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
	avgRecall := totalRecall / float64(b.N)
	b.ReportMetric(avgRecall*100, "recall%")
}
