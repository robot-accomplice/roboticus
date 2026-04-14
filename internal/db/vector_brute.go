package db

import (
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
)

// VectorIndexConfig controls vector index construction.
type VectorIndexConfig struct {
	MinEntries int // minimum embeddings before marking index as built (default 100)
}

// VectorEntry is a single indexed embedding with metadata.
// Embeddings are stored as float32 — the native format from providers and SQLite blobs.
type VectorEntry struct {
	SourceTable    string    `json:"source_table"`
	SourceID       string    `json:"source_id"`
	ContentPreview string    `json:"content_preview"`
	Embedding      []float32 `json:"embedding"`
}

// VectorSearchResult is a search hit with similarity score.
type VectorSearchResult struct {
	SourceTable    string  `json:"source_table"`
	SourceID       string  `json:"source_id"`
	ContentPreview string  `json:"content_preview"`
	Similarity     float64 `json:"similarity"`
}

// BruteForceIndex is an in-memory exact nearest neighbor index.
// Uses brute-force cosine similarity (O(n) per query) over a flat slice.
// Satisfies VectorIndex. Use HNSWGraph for sub-linear search at scale.
type BruteForceIndex struct {
	mu         sync.RWMutex
	entries    []VectorEntry
	built      bool
	minEntries int
}

// NewBruteForceIndex creates a brute-force vector index.
func NewBruteForceIndex(cfg VectorIndexConfig) *BruteForceIndex {
	if cfg.MinEntries <= 0 {
		cfg.MinEntries = 100
	}
	return &BruteForceIndex{
		minEntries: cfg.MinEntries,
	}
}

// BuildFromStore loads all embeddings from the database and builds the index.
// Rust parity: reads embedding_blob (4-byte LE IEEE 754 BLOB), not JSON text.
func (h *BruteForceIndex) BuildFromStore(store *Store) error {
	rows, err := store.db.Query(
		`SELECT source_table, source_id, content_preview, embedding_blob
		 FROM embeddings WHERE embedding_blob IS NOT NULL`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = nil
	for rows.Next() {
		var entry VectorEntry
		var blob []byte
		if err := rows.Scan(&entry.SourceTable, &entry.SourceID, &entry.ContentPreview, &blob); err != nil {
			continue
		}
		entry.Embedding = BlobToEmbedding(blob)
		if len(entry.Embedding) == 0 {
			continue
		}
		h.entries = append(h.entries, entry)
	}

	if len(h.entries) >= h.minEntries {
		h.built = true
		log.Info().Int("entries", len(h.entries)).Msg("vector index built (brute-force)")
	} else {
		log.Info().Int("entries", len(h.entries)).Int("min", h.minEntries).Msg("vector index: not enough entries")
	}

	return nil
}

// Search returns the top-k nearest neighbors via exhaustive cosine similarity.
func (h *BruteForceIndex) Search(query []float32, k int) []VectorSearchResult {
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
		sim := CosineSimilarityF32(query, entry.Embedding)
		results = append(results, scored{i, sim})
	}

	// Sort by similarity descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	if k > len(results) {
		k = len(results)
	}

	out := make([]VectorSearchResult, k)
	for i := 0; i < k; i++ {
		e := h.entries[results[i].idx]
		out[i] = VectorSearchResult{
			SourceTable:    e.SourceTable,
			SourceID:       e.SourceID,
			ContentPreview: e.ContentPreview,
			Similarity:     results[i].similarity,
		}
	}
	return out
}

// IsBuilt returns whether the index has been built.
func (h *BruteForceIndex) IsBuilt() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.built
}

// EntryCount returns the number of indexed entries.
func (h *BruteForceIndex) EntryCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}

// AddEntry adds a single entry to the index (for incremental updates).
func (h *BruteForceIndex) AddEntry(entry VectorEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, entry)
	if !h.built && len(h.entries) >= h.minEntries {
		h.built = true
	}
}
