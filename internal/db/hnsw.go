package db

import (
	"encoding/json"
	"math"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
)

// HNSWConfig controls the ANN index.
type HNSWConfig struct {
	MinEntries int // minimum embeddings before building index (default 100)
}

// HNSWEntry is a single indexed embedding with metadata.
type HNSWEntry struct {
	SourceTable    string    `json:"source_table"`
	SourceID       string    `json:"source_id"`
	ContentPreview string    `json:"content_preview"`
	Embedding      []float64 `json:"embedding"`
}

// HNSWSearchResult is a search hit with similarity score.
type HNSWSearchResult struct {
	SourceTable    string  `json:"source_table"`
	SourceID       string  `json:"source_id"`
	ContentPreview string  `json:"content_preview"`
	Similarity     float64 `json:"similarity"`
}

// HNSWIndex is an in-memory approximate nearest neighbor index.
// Uses brute-force cosine similarity (exact NN) for correctness, with the
// same API as an HNSW index. Can be upgraded to a real HNSW implementation
// when the embedding count justifies the complexity.
type HNSWIndex struct {
	mu         sync.RWMutex
	entries    []HNSWEntry
	built      bool
	minEntries int
}

// NewHNSWIndex creates an ANN index.
func NewHNSWIndex(cfg HNSWConfig) *HNSWIndex {
	if cfg.MinEntries <= 0 {
		cfg.MinEntries = 100
	}
	return &HNSWIndex{
		minEntries: cfg.MinEntries,
	}
}

// BuildFromStore loads all embeddings from the database and builds the index.
func (h *HNSWIndex) BuildFromStore(store *Store) error {
	rows, err := store.db.Query(
		`SELECT source_table, source_id, content_preview, embedding_json
		 FROM embeddings WHERE embedding_json IS NOT NULL`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = nil
	for rows.Next() {
		var entry HNSWEntry
		var embJSON string
		if err := rows.Scan(&entry.SourceTable, &entry.SourceID, &entry.ContentPreview, &embJSON); err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(embJSON), &entry.Embedding); err != nil {
			continue
		}
		h.entries = append(h.entries, entry)
	}

	if len(h.entries) >= h.minEntries {
		h.built = true
		log.Info().Int("entries", len(h.entries)).Msg("HNSW index built")
	} else {
		log.Info().Int("entries", len(h.entries)).Int("min", h.minEntries).Msg("HNSW index: not enough entries")
	}

	return nil
}

// Search returns the top-k nearest neighbors to the query embedding.
func (h *HNSWIndex) Search(query []float64, k int) []HNSWSearchResult {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.entries) == 0 || k <= 0 {
		return nil
	}

	type scored struct {
		idx        int
		similarity float64
	}

	var results []scored
	for i, entry := range h.entries {
		sim := cosineSimilarity(query, entry.Embedding)
		results = append(results, scored{i, sim})
	}

	// Sort by similarity descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	if k > len(results) {
		k = len(results)
	}

	out := make([]HNSWSearchResult, k)
	for i := 0; i < k; i++ {
		e := h.entries[results[i].idx]
		out[i] = HNSWSearchResult{
			SourceTable:    e.SourceTable,
			SourceID:       e.SourceID,
			ContentPreview: e.ContentPreview,
			Similarity:     results[i].similarity,
		}
	}
	return out
}

// IsBuilt returns whether the index has been built.
func (h *HNSWIndex) IsBuilt() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.built
}

// EntryCount returns the number of indexed entries.
func (h *HNSWIndex) EntryCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}

// AddEntry adds a single entry to the index (for incremental updates).
func (h *HNSWIndex) AddEntry(entry HNSWEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, entry)
	if !h.built && len(h.entries) >= h.minEntries {
		h.built = true
	}
}

func cosineSimilarity(a, b []float64) float64 {
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
