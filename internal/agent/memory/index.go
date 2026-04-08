package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// MemoryTier classifies memory entries by their lifecycle stage.
type MemoryTier int

const (
	TierWorking      MemoryTier = iota // Active session context
	TierEpisodic                       // Event-based memories
	TierSemantic                       // Distilled knowledge
	TierProcedural                     // Tool/skill statistics
	TierRelationship                   // Entity relationship graph
)

// String returns the tier name.
func (t MemoryTier) String() string {
	switch t {
	case TierWorking:
		return "working"
	case TierEpisodic:
		return "episodic"
	case TierSemantic:
		return "semantic"
	case TierProcedural:
		return "procedural"
	case TierRelationship:
		return "relationship"
	default:
		return "unknown"
	}
}

// MemoryTierFromString parses a tier name string to MemoryTier.
func MemoryTierFromString(s string) MemoryTier {
	switch strings.ToLower(s) {
	case "working":
		return TierWorking
	case "episodic":
		return TierEpisodic
	case "semantic":
		return TierSemantic
	case "procedural":
		return TierProcedural
	case "relationship":
		return TierRelationship
	default:
		return TierEpisodic
	}
}

// MemoryIndexEntry is a lightweight recall entry in the memory index.
type MemoryIndexEntry struct {
	ID      string     `json:"id"`
	Summary string     `json:"summary"`
	Tier    MemoryTier `json:"tier"`
	Score   float64    `json:"score"`
}

// MemoryIndex provides fast lightweight lookups across memory tiers
// without loading full entry content. Backed by the memory_index table.
type MemoryIndex struct {
	mu      sync.RWMutex
	entries []MemoryIndexEntry
	store   *db.Store
}

// NewMemoryIndex creates a MemoryIndex backed by the given store.
func NewMemoryIndex(store *db.Store) *MemoryIndex {
	return &MemoryIndex{store: store}
}

// Load populates the in-memory index from the database.
func (mi *MemoryIndex) Load(ctx context.Context) error {
	rows, err := mi.store.QueryContext(ctx,
		`SELECT id, source_table, summary, confidence
		 FROM memory_index
		 ORDER BY confidence DESC
		 LIMIT 1000`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	mi.mu.Lock()
	defer mi.mu.Unlock()

	mi.entries = nil
	for rows.Next() {
		var id, sourceTable, summary string
		var confidence float64
		if err := rows.Scan(&id, &sourceTable, &summary, &confidence); err != nil {
			log.Warn().Err(err).Msg("memory_index: scan failed")
			continue
		}
		mi.entries = append(mi.entries, MemoryIndexEntry{
			ID:      id,
			Summary: summary,
			Tier:    MemoryTierFromString(sourceTable),
			Score:   confidence,
		})
	}
	return nil
}

// Lookup returns lightweight recall entries matching the query, limited by count.
// Uses simple keyword matching against summaries for fast recall.
func (mi *MemoryIndex) Lookup(query string, limit int) []MemoryIndexEntry {
	mi.mu.RLock()
	defer mi.mu.RUnlock()

	if limit <= 0 || len(mi.entries) == 0 {
		return nil
	}

	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	var results []MemoryIndexEntry
	for _, entry := range mi.entries {
		if len(results) >= limit {
			break
		}
		summaryLower := strings.ToLower(entry.Summary)

		// Match if any query word appears in the summary.
		matched := false
		for _, word := range queryWords {
			if strings.Contains(summaryLower, word) {
				matched = true
				break
			}
		}
		if matched {
			results = append(results, entry)
		}
	}
	return results
}

// EntryCount returns the number of loaded index entries.
func (mi *MemoryIndex) EntryCount() int {
	mi.mu.RLock()
	defer mi.mu.RUnlock()
	return len(mi.entries)
}
