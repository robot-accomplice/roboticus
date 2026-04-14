// Hybrid FTS5 + vector search.
// Rust parity: embeddings.rs hybrid_search() — combines FTS5 MATCH with cosine
// similarity, weighted by hybrid_weight (0=FTS only, 1=vector only).

package db

import (
	"context"
	"sort"
	"strings"
)

// mergedCandidate holds per-leg scores before final blending.
type mergedCandidate struct {
	sourceTable    string
	sourceID       string
	contentPreview string
	ftsScore       float64 // 0 if not found by FTS
	vecScore       float64 // 0 if not found by vector search
}

// HybridSearch combines FTS5 text search with vector cosine similarity.
// hybridWeight controls the blend: 0.0 = FTS only, 1.0 = vector only, 0.5 = balanced.
// Results are deduplicated by (source_table, source_id) — a document found by both
// legs gets a blended score rather than appearing twice.
// Rust parity: embeddings.rs hybrid_search().
func HybridSearch(
	ctx context.Context,
	store *Store,
	queryText string,
	queryEmbedding []float32,
	limit int,
	hybridWeight float64,
	vectorIndex VectorIndex,
) []VectorSearchResult {
	if limit <= 0 {
		return nil
	}

	// Merge map keyed by "source_table|source_id".
	candidates := make(map[string]*mergedCandidate)
	key := func(table, id string) string { return table + "|" + id }

	// FTS5 leg: BM25-scored text search via memory_fts.
	ftsQuery := SanitizeFTSQuery(queryText)
	if ftsQuery != "" && hybridWeight < 1.0 {
		rows, err := store.QueryContext(ctx,
			`SELECT content, source_table, source_id, bm25(memory_fts) AS score
			 FROM memory_fts
			 WHERE memory_fts MATCH ?1
			 ORDER BY score
			 LIMIT ?2`,
			ftsQuery, limit*2)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var content, sourceTable, sourceID string
				var bm25Score float64
				if rows.Scan(&content, &sourceTable, &sourceID, &bm25Score) != nil {
					continue
				}
				// BM25 returns negative values (more negative = better match).
				// Normalize to (0, 1]: score = 1 / (1 - bm25).
				// bm25=-10 → 0.09, bm25=-1 → 0.5, bm25=0 → 1.0.
				// Guard: bm25 >= 1.0 would cause div-by-zero (unreachable in
				// SQLite's BM25 impl, but defensive).
				var ftsScore float64
				if bm25Score >= 1.0 {
					ftsScore = 1.0
				} else {
					ftsScore = 1.0 / (1.0 - bm25Score)
				}

				preview := content
				if len(preview) > 200 {
					preview = preview[:200]
				}
				k := key(sourceTable, sourceID)
				if c, ok := candidates[k]; ok {
					c.ftsScore = ftsScore
				} else {
					candidates[k] = &mergedCandidate{
						sourceTable:    sourceTable,
						sourceID:       sourceID,
						contentPreview: preview,
						ftsScore:       ftsScore,
					}
				}
			}
		}
	}

	// Vector leg: cosine similarity via vector index.
	if len(queryEmbedding) > 0 && hybridWeight > 0.0 && vectorIndex != nil && vectorIndex.IsBuilt() {
		vecResults := vectorIndex.Search(queryEmbedding, limit*2)
		for _, r := range vecResults {
			k := key(r.SourceTable, r.SourceID)
			if c, ok := candidates[k]; ok {
				c.vecScore = r.Similarity
				// Prefer the vector leg's preview if FTS truncated it.
				if r.ContentPreview != "" && len(r.ContentPreview) > len(c.contentPreview) {
					c.contentPreview = r.ContentPreview
				}
			} else {
				candidates[k] = &mergedCandidate{
					sourceTable:    r.SourceTable,
					sourceID:       r.SourceID,
					contentPreview: r.ContentPreview,
					vecScore:       r.Similarity,
				}
			}
		}
	}

	// Blend scores and build result slice with per-leg transparency.
	results := make([]VectorSearchResult, 0, len(candidates))
	for _, c := range candidates {
		blended := c.ftsScore*(1.0-hybridWeight) + c.vecScore*hybridWeight
		results = append(results, VectorSearchResult{
			SourceTable:    c.sourceTable,
			SourceID:       c.sourceID,
			ContentPreview: c.contentPreview,
			Similarity:     blended,
			FTSScore:       c.ftsScore,
			VectorScore:    c.vecScore,
		})
	}

	// Sort by blended similarity descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// SanitizeFTSQuery escapes FTS5 special characters to prevent syntax errors.
func SanitizeFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	// Remove FTS5 operators that could cause parse errors.
	replacer := strings.NewReplacer(
		"*", "", "\"", "", "(", "", ")", "",
		"AND", "", "OR", "", "NOT", "", "NEAR", "",
	)
	cleaned := replacer.Replace(query)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	// Quote each word for exact match.
	words := strings.Fields(cleaned)
	return "\"" + strings.Join(words, "\" \"") + "\""
}
