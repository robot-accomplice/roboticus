// vector_index.go defines the VectorIndex interface and shared vector utilities.
//
// All vector search implementations (BruteForceIndex, HNSWGraph, PartitionedIndex)
// satisfy VectorIndex. Callers should depend on the interface, not concrete types.

package db

import "math"

// VectorIndex abstracts over vector search implementations.
// BruteForceIndex provides O(n) exact search; HNSWGraph provides O(log n)
// approximate search. PartitionedIndex fans out across tier-specific indices.
type VectorIndex interface {
	// Search returns the top-k nearest neighbors to the query embedding,
	// sorted by descending similarity.
	Search(query []float64, k int) []VectorSearchResult

	// AddEntry inserts a single entry for incremental index updates.
	AddEntry(entry VectorEntry)

	// IsBuilt returns true when the index has enough entries to be useful.
	IsBuilt() bool

	// EntryCount returns the number of indexed entries.
	EntryCount() int
}

// CosineSimilarityF64 computes cosine similarity between two float64 vectors.
// Returns 0 for empty, mismatched-length, or zero-norm vectors.
func CosineSimilarityF64(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// Compile-time interface checks.
var _ VectorIndex = (*BruteForceIndex)(nil)
