package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// retrieveEpisodic fetches from episodic_memory using a union strategy:
// 1. HybridSearch (BM25 + vector, deduplicated) for query-relevant results
// 2. Recency results (recent, any content)
// Re-ranked with temporal decay + adaptive hybrid weight.
func (mr *Retriever) retrieveEpisodic(ctx context.Context, query string, queryEmbed []float32, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	type candidate struct {
		id      string
		content string
		ageDays float64
		ftsHit  bool // true if this came from FTS/hybrid match
	}
	seen := make(map[string]struct{})
	var candidates []candidate

	// Leg 1: HybridSearch — BM25 + vector, deduplicated, query-relevant.
	if query != "" {
		corpusSize := mr.estimateCorpusSize(ctx)
		hybridWeight := AdaptiveHybridWeight(corpusSize)

		hybridResults := db.HybridSearch(ctx, mr.store, query, queryEmbed, 20, hybridWeight, mr.vectorIndex)
		if len(hybridResults) > 0 {
			// Look up age for each result.
			for _, hr := range hybridResults {
				if hr.SourceTable != "episodic_memory" {
					continue
				}
				var ageDays float64
				var content string
				err := mr.store.QueryRowContext(ctx,
					`SELECT content, julianday('now') - julianday(created_at)
					 FROM episodic_memory WHERE id = ? AND memory_state = 'active'`,
					hr.SourceID).Scan(&content, &ageDays)
				if err != nil {
					continue
				}
				seen[hr.SourceID] = struct{}{}
				candidates = append(candidates, candidate{
					id:      hr.SourceID,
					content: content,
					ageDays: ageDays,
					ftsHit:  true,
				})
			}
		}

		// Fallback: if HybridSearch returned no episodic results (e.g., no FTS
		// matches and no vector index), try direct FTS5 join as before.
		if len(candidates) == 0 {
			ftsQuery := db.SanitizeFTSQuery(query)
			if ftsQuery != "" {
				rows, err := mr.store.QueryContext(ctx,
					`SELECT em.id, em.content, julianday('now') - julianday(em.created_at) as age_days
					 FROM memory_fts fts
					 JOIN episodic_memory em ON em.id = fts.source_id
					 WHERE fts.source_table = 'episodic_memory'
					   AND memory_fts MATCH ?
					   AND em.memory_state = 'active'
					 LIMIT 20`, ftsQuery)
				if err != nil {
					log.Debug().Err(err).Str("fts_query", ftsQuery).Msg("episodic FTS fallback failed")
				} else {
					for rows.Next() {
						var c candidate
						if rows.Scan(&c.id, &c.content, &c.ageDays) != nil {
							continue
						}
						c.ftsHit = true
						seen[c.id] = struct{}{}
						candidates = append(candidates, c)
					}
					_ = rows.Close()
				}
			}
		}
	}

	// Leg 2: Recency — recent memories regardless of query match.
	{
		rows, err := mr.store.QueryContext(ctx,
			`SELECT id, content, julianday('now') - julianday(created_at) as age_days
			 FROM episodic_memory WHERE memory_state = 'active'
			 ORDER BY created_at DESC LIMIT 20`)
		if err == nil {
			for rows.Next() {
				var c candidate
				if rows.Scan(&c.id, &c.content, &c.ageDays) != nil {
					continue
				}
				if _, dup := seen[c.id]; dup {
					continue
				}
				seen[c.id] = struct{}{}
				candidates = append(candidates, c)
			}
			_ = rows.Close()
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Pre-load stored embeddings for re-ranking (single bulk query).
	storedEmbeds := make(map[string][]float32)
	if queryEmbed != nil && mr.store != nil {
		ids := make([]string, len(candidates))
		for i, c := range candidates {
			ids[i] = c.id
		}
		storedEmbeds = mr.loadStoredEmbeddings(ctx, "episodic_memory", ids)
	}

	// Score and rank: blend temporal decay, FTS relevance, and embedding similarity.
	corpusSize := mr.estimateCorpusSize(ctx)
	hybridWeight := AdaptiveHybridWeight(corpusSize)

	type scored struct {
		content string
		score   float64
	}
	entries := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		score := scoreEpisodicCandidate(c.ageDays, c.ftsHit, mr.config)

		// Blend with precomputed embedding similarity (no API calls).
		if queryEmbed != nil {
			if textEmbed, ok := storedEmbeds[c.id]; ok {
				sim := llm.CosineSimilarity(queryEmbed, textEmbed)
				score = (1-hybridWeight)*score + hybridWeight*sim
			}
		}

		entries = append(entries, scored{content: c.content, score: score})
	}

	// Sort by score descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	var b strings.Builder
	used := 0
	for _, e := range entries {
		if used+len(e.content) > maxChars {
			break
		}
		fmt.Fprintf(&b, "- (sim=%.2f) %s\n", e.score, e.content)
		used += len(e.content)
	}
	return b.String()
}

// scoreEpisodicCandidate computes a base relevance score for an episodic memory.
// FTS hits get a relevance boost that resists temporal decay, ensuring old but
// query-matched memories can outrank recent but irrelevant ones.
func scoreEpisodicCandidate(ageDays float64, ftsHit bool, cfg RetrievalConfig) float64 {
	decay := math.Pow(0.5, ageDays/cfg.EpisodicHalfLife)
	if decay < cfg.DecayFloor {
		decay = cfg.DecayFloor
	}
	if ftsHit {
		decay += 0.4
		if decay > 1.0 {
			decay = 1.0
		}
	}
	return decay
}

// loadStoredEmbeddings bulk-loads precomputed embeddings from the embeddings table
// for the given source IDs. Returns a map of sourceID → embedding vector.
func (mr *Retriever) loadStoredEmbeddings(ctx context.Context, sourceTable string, ids []string) map[string][]float32 {
	result := make(map[string][]float32, len(ids))
	if len(ids) == 0 || mr.store == nil {
		return result
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, sourceTable)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	q := fmt.Sprintf(
		`SELECT source_id, embedding_blob FROM embeddings
		 WHERE source_table = ? AND source_id IN (%s)
		 AND embedding_blob IS NOT NULL`,
		strings.Join(placeholders, ","))

	rows, err := mr.store.QueryContext(ctx, q, args...)
	if err != nil {
		log.Debug().Err(err).Msg("loadStoredEmbeddings: query failed")
		return result
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sourceID string
		var blob []byte
		if rows.Scan(&sourceID, &blob) != nil {
			continue
		}
		vec := db.BlobToEmbedding(blob)
		if len(vec) > 0 {
			result[sourceID] = vec
		}
	}
	return result
}
