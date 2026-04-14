// vector_ann.go implements a Hierarchical Navigable Small World (HNSW) graph
// for sub-linear approximate nearest neighbor search.
//
// The algorithm builds a multi-layer proximity graph where upper layers are
// sparse (enabling fast traversal) and layer 0 is dense (enabling precise
// neighbor selection). Search starts at the top and greedily descends.
//
// References:
//   - Malkov & Yashunin, "Efficient and robust approximate nearest neighbor
//     using Hierarchical Navigable Small World graphs" (2018)
//   - ColBERTv2 (Santhanam et al., 2022) for the compression insight
//
// Satisfies VectorIndex interface. Thread-safe via sync.RWMutex.

package db

import (
	"math"
	"math/rand"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
)

// hnswNode represents a single element in the HNSW graph.
type hnswNode struct {
	entry      VectorEntry
	neighbors  [][]int // neighbors[layer] = list of node indices
	maxLayer   int
}

// HNSWGraph is a multi-layer approximate nearest neighbor index.
type HNSWGraph struct {
	mu             sync.RWMutex
	nodes          []hnswNode
	entryPoint     int // index of the entry point node (-1 if empty)
	maxLevel       int // current maximum layer in the graph
	m              int // max connections per node per layer
	mMax0          int // max connections at layer 0 (typically 2*M)
	efConstruction int // beam width during construction
	efSearch       int // beam width during search
	mL             float64 // level generation factor: 1/ln(M)
	rng            *rand.Rand
	built          bool
	minEntries     int
}

// NewHNSWGraph creates an HNSW index with the given configuration.
// Uses VectorIndexConfig.MinEntries for the built threshold.
// HNSW-specific parameters use sensible defaults:
//   - M=16, EfConstruction=200, EfSearch=50
func NewHNSWGraph(cfg VectorIndexConfig) *HNSWGraph {
	m := 16
	if cfg.MinEntries <= 0 {
		cfg.MinEntries = 100
	}
	return &HNSWGraph{
		entryPoint:     -1,
		maxLevel:       -1,
		m:              m,
		mMax0:          2 * m,
		efConstruction: 200,
		efSearch:       50,
		mL:             1.0 / math.Log(float64(m)),
		rng:            rand.New(rand.NewSource(42)),
		minEntries:     cfg.MinEntries,
	}
}

// randomLevel generates a random layer assignment for a new node.
// Caller MUST hold h.mu.Lock() — h.rng is not goroutine-safe.
func (h *HNSWGraph) randomLevel() int {
	r := h.rng.Float64()
	return int(-math.Log(r) * h.mL)
}

// AddEntry inserts a single vector into the HNSW graph.
func (h *HNSWGraph) AddEntry(entry VectorEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.insertUnlocked(entry)
}

// insertUnlocked performs the actual HNSW insertion.
// Caller MUST hold h.mu.Lock().
func (h *HNSWGraph) insertUnlocked(entry VectorEntry) {
	nodeIdx := len(h.nodes)
	level := h.randomLevel()

	node := hnswNode{
		entry:    entry,
		maxLayer: level,
	}
	node.neighbors = make([][]int, level+1)
	for i := range node.neighbors {
		node.neighbors[i] = nil
	}
	h.nodes = append(h.nodes, node)

	if h.entryPoint == -1 {
		h.entryPoint = nodeIdx
		h.maxLevel = level
		if !h.built && len(h.nodes) >= h.minEntries {
			h.built = true
		}
		return
	}

	// Phase 1: Greedily traverse from top to the node's max layer + 1.
	current := h.entryPoint
	for lc := h.maxLevel; lc > level; lc-- {
		current = h.greedyClosest(entry.Embedding, current, lc)
	}

	// Phase 2: Insert at each layer from min(level, maxLevel) down to 0.
	for lc := min(level, h.maxLevel); lc >= 0; lc-- {
		candidates := h.searchLayer(entry.Embedding, current, h.efConstruction, lc)

		maxConn := h.m
		if lc == 0 {
			maxConn = h.mMax0
		}
		neighbors := h.selectNeighborsSimple(candidates, maxConn)

		// Connect the new node to its neighbors.
		h.nodes[nodeIdx].neighbors[lc] = neighbors

		// Add reverse connections and prune if needed.
		for _, nIdx := range neighbors {
			h.nodes[nIdx].neighbors[lc] = append(h.nodes[nIdx].neighbors[lc], nodeIdx)
			if len(h.nodes[nIdx].neighbors[lc]) > maxConn {
				h.nodes[nIdx].neighbors[lc] = h.pruneNeighbors(nIdx, lc, maxConn)
			}
		}

		if len(candidates) > 0 {
			current = candidates[0].idx
		}
	}

	// Update entry point if the new node has a higher level.
	if level > h.maxLevel {
		h.maxLevel = level
		h.entryPoint = nodeIdx
	}

	if !h.built && len(h.nodes) >= h.minEntries {
		h.built = true
	}
}

// candidateItem is a (index, distance) pair used during search.
type candidateItem struct {
	idx  int
	dist float64 // distance = 1 - similarity (lower is closer)
}

// greedyClosest finds the single closest node to the query at a given layer.
func (h *HNSWGraph) greedyClosest(query []float32, start int, layer int) int {
	current := start
	currentDist := 1.0 - CosineSimilarityF32(query, h.nodes[current].entry.Embedding)

	for {
		changed := false
		if layer < len(h.nodes[current].neighbors) {
			for _, nIdx := range h.nodes[current].neighbors[layer] {
				d := 1.0 - CosineSimilarityF32(query, h.nodes[nIdx].entry.Embedding)
				if d < currentDist {
					currentDist = d
					current = nIdx
					changed = true
				}
			}
		}
		if !changed {
			return current
		}
	}
}

// searchLayer performs a beam search at a given layer, returning the ef closest nodes.
// TODO(perf): Replace sorted slice with container/heap for O(log c) vs O(c log c)
// per expansion step. Not blocking at efSearch=50 but needed before tuning ef>100.
func (h *HNSWGraph) searchLayer(query []float32, entryIdx int, ef int, layer int) []candidateItem {
	visited := make(map[int]bool)
	visited[entryIdx] = true

	d := 1.0 - CosineSimilarityF32(query, h.nodes[entryIdx].entry.Embedding)
	candidates := []candidateItem{{entryIdx, d}}
	results := []candidateItem{{entryIdx, d}}

	for len(candidates) > 0 {
		// Pop the closest candidate.
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].dist < candidates[j].dist
		})
		closest := candidates[0]
		candidates = candidates[1:]

		// Furthest in results.
		furthestDist := results[len(results)-1].dist
		if closest.dist > furthestDist && len(results) >= ef {
			break
		}

		// Expand neighbors.
		if layer < len(h.nodes[closest.idx].neighbors) {
			for _, nIdx := range h.nodes[closest.idx].neighbors[layer] {
				if visited[nIdx] {
					continue
				}
				visited[nIdx] = true
				nd := 1.0 - CosineSimilarityF32(query, h.nodes[nIdx].entry.Embedding)

				if nd < furthestDist || len(results) < ef {
					candidates = append(candidates, candidateItem{nIdx, nd})
					results = append(results, candidateItem{nIdx, nd})

					// Keep results sorted and trimmed to ef.
					sort.Slice(results, func(i, j int) bool {
						return results[i].dist < results[j].dist
					})
					if len(results) > ef {
						results = results[:ef]
					}
				}
			}
		}
	}

	return results
}

// selectNeighborsSimple picks the closest maxConn candidates (simple heuristic).
func (h *HNSWGraph) selectNeighborsSimple(candidates []candidateItem, maxConn int) []int {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})
	n := maxConn
	if n > len(candidates) {
		n = len(candidates)
	}
	result := make([]int, n)
	for i := 0; i < n; i++ {
		result[i] = candidates[i].idx
	}
	return result
}

// pruneNeighbors reduces a node's neighbor list to maxConn by keeping the closest.
func (h *HNSWGraph) pruneNeighbors(nodeIdx int, layer int, maxConn int) []int {
	neighbors := h.nodes[nodeIdx].neighbors[layer]
	embedding := h.nodes[nodeIdx].entry.Embedding

	type nd struct {
		idx  int
		dist float64
	}
	dists := make([]nd, len(neighbors))
	for i, nIdx := range neighbors {
		dists[i] = nd{nIdx, 1.0 - CosineSimilarityF32(embedding, h.nodes[nIdx].entry.Embedding)}
	}
	sort.Slice(dists, func(i, j int) bool {
		return dists[i].dist < dists[j].dist
	})
	if len(dists) > maxConn {
		dists = dists[:maxConn]
	}
	result := make([]int, len(dists))
	for i, d := range dists {
		result[i] = d.idx
	}
	return result
}

// Search returns the top-k nearest neighbors to the query embedding.
func (h *HNSWGraph) Search(query []float32, k int) []VectorSearchResult {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entryPoint == -1 || k <= 0 {
		return nil
	}

	// Phase 1: Greedy descent from top layer to layer 1.
	current := h.entryPoint
	for lc := h.maxLevel; lc > 0; lc-- {
		current = h.greedyClosest(query, current, lc)
	}

	// Phase 2: Beam search at layer 0.
	ef := h.efSearch
	if ef < k {
		ef = k
	}
	results := h.searchLayer(query, current, ef, 0)

	// Take top-k.
	sort.Slice(results, func(i, j int) bool {
		return results[i].dist < results[j].dist
	})
	if len(results) > k {
		results = results[:k]
	}

	out := make([]VectorSearchResult, len(results))
	for i, r := range results {
		e := h.nodes[r.idx].entry
		out[i] = VectorSearchResult{
			SourceTable:    e.SourceTable,
			SourceID:       e.SourceID,
			ContentPreview: e.ContentPreview,
			Similarity:     1.0 - r.dist,
		}
	}
	return out
}

// BuildFromStore loads all embeddings from the database and batch-inserts them.
// Holds the write lock once for the entire batch — avoids N lock/unlock cycles.
func (h *HNSWGraph) BuildFromStore(store *Store) error {
	rows, err := store.db.Query(
		`SELECT source_table, source_id, content_preview, embedding_blob
		 FROM embeddings WHERE embedding_blob IS NOT NULL`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	// Collect entries first (rows iteration holds the DB cursor).
	var entries []VectorEntry
	for rows.Next() {
		var entry VectorEntry
		var blob []byte
		if err := rows.Scan(&entry.SourceTable, &entry.SourceID, &entry.ContentPreview, &blob); err != nil {
			log.Debug().Err(err).Msg("BuildFromStore: skipping unreadable embedding row")
			continue
		}
		entry.Embedding = BlobToEmbedding(blob)
		if len(entry.Embedding) == 0 {
			continue
		}
		entries = append(entries, entry)
	}

	// Single lock acquisition for the entire batch.
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, entry := range entries {
		h.insertUnlocked(entry)
	}
	return nil
}

// IsBuilt returns whether the index has enough entries to be useful.
func (h *HNSWGraph) IsBuilt() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.built
}

// EntryCount returns the number of indexed entries.
func (h *HNSWGraph) EntryCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes)
}

// Compile-time interface check.
var _ VectorIndex = (*HNSWGraph)(nil)
