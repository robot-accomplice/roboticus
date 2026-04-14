// vector_partitioned.go wraps multiple VectorIndex instances behind a single
// VectorIndex interface, routing entries to partitions by source table.
//
// This prevents semantic collapse by keeping each partition under the ~10K
// entry threshold where embedding distances remain meaningful. Queries fan
// out to all partitions and merge results by similarity.

package db

import (
	"sort"
	"sync"
)

// PartitionConfig maps source table prefixes to partition names.
// Tables not matched by any prefix go to the default "warm" partition.
var defaultPartitionMap = map[string]string{
	"working_memory":      "hot",
	"episodic_memory":     "hot",
	"semantic_memory":     "warm",
	"procedural_memory":   "warm",
	"relationship_memory": "warm",
}

// PartitionedIndex fans out AddEntry/Search across tier-based partitions.
// Each partition is an independent VectorIndex (BruteForce or HNSW).
type PartitionedIndex struct {
	mu         sync.RWMutex
	partitions map[string]VectorIndex // "hot", "warm", etc.
	routeMap   map[string]string      // source_table → partition name
}

// NewPartitionedIndex creates a partitioned index with hot and warm partitions.
// threshold controls when each partition promotes from BruteForce to HNSW.
func NewPartitionedIndex(threshold int) *PartitionedIndex {
	cfg := VectorIndexConfig{MinEntries: threshold}
	return &PartitionedIndex{
		partitions: map[string]VectorIndex{
			"hot":  NewHNSWGraph(cfg),
			"warm": NewHNSWGraph(cfg),
		},
		routeMap: defaultPartitionMap,
	}
}

// NewPartitionedIndexBrute creates a partitioned index using BruteForceIndex
// for both partitions (used when corpus is below HNSW threshold).
func NewPartitionedIndexBrute(threshold int) *PartitionedIndex {
	cfg := VectorIndexConfig{MinEntries: threshold}
	return &PartitionedIndex{
		partitions: map[string]VectorIndex{
			"hot":  NewBruteForceIndex(cfg),
			"warm": NewBruteForceIndex(cfg),
		},
		routeMap: defaultPartitionMap,
	}
}

// route returns the partition name for a given source table.
func (pi *PartitionedIndex) route(sourceTable string) string {
	if name, ok := pi.routeMap[sourceTable]; ok {
		return name
	}
	return "warm" // default partition
}

// AddEntry routes the entry to the appropriate partition by source table.
func (pi *PartitionedIndex) AddEntry(entry VectorEntry) {
	pi.mu.RLock()
	partition := pi.partitions[pi.route(entry.SourceTable)]
	pi.mu.RUnlock()

	if partition != nil {
		partition.AddEntry(entry)
	}
}

// Search fans out to all partitions, merges results, and returns top-k.
func (pi *PartitionedIndex) Search(query []float32, k int) []VectorSearchResult {
	if k <= 0 {
		return nil
	}

	pi.mu.RLock()
	parts := make([]VectorIndex, 0, len(pi.partitions))
	for _, p := range pi.partitions {
		parts = append(parts, p)
	}
	pi.mu.RUnlock()

	// Collect results from built partitions only. Unbuilt partitions (below
	// MinEntries threshold) are skipped — their entries are not yet reliable
	// enough for similarity search. This is intentional: at small corpus sizes
	// the episodic retrieval path uses direct FTS5 queries as a fallback,
	// so these entries are still reachable through the text search leg.
	var all []VectorSearchResult
	for _, p := range parts {
		if p.IsBuilt() {
			all = append(all, p.Search(query, k)...)
		}
	}

	// Sort by similarity descending and take top-k.
	sort.Slice(all, func(i, j int) bool {
		return all[i].Similarity > all[j].Similarity
	})
	if len(all) > k {
		all = all[:k]
	}
	return all
}

// IsBuilt returns true if any partition has been built.
func (pi *PartitionedIndex) IsBuilt() bool {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	for _, p := range pi.partitions {
		if p.IsBuilt() {
			return true
		}
	}
	return false
}

// EntryCount returns the total entries across all partitions.
func (pi *PartitionedIndex) EntryCount() int {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	total := 0
	for _, p := range pi.partitions {
		total += p.EntryCount()
	}
	return total
}

// BuildFromStore loads embeddings and routes them to partitions.
func (pi *PartitionedIndex) BuildFromStore(store *Store) error {
	rows, err := store.db.Query(
		`SELECT source_table, source_id, content_preview, embedding_blob
		 FROM embeddings WHERE embedding_blob IS NOT NULL`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

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
		pi.AddEntry(entry)
	}
	return nil
}

// Compile-time interface check.
var _ VectorIndex = (*PartitionedIndex)(nil)
