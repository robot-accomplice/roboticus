// Hybrid FTS5 + vector search.
// Rust parity: embeddings.rs hybrid_search() — combines FTS5 MATCH with cosine
// similarity, weighted by hybrid_weight (0=FTS only, 1=vector only).

package db

import (
	"context"
	"math"
	"sort"
	"strings"
)

// HybridSearch combines FTS5 text search with vector cosine similarity.
// hybrid_weight controls the blend: 0.0 = FTS only, 1.0 = vector only, 0.5 = balanced.
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

	var results []VectorSearchResult

	// FTS5 leg: text search via memory_fts MATCH.
	ftsQuery := SanitizeFTSQuery(queryText)
	if ftsQuery != "" && hybridWeight < 1.0 {
		rows, err := store.QueryContext(ctx,
			`SELECT content, source_table, source_id
			 FROM memory_fts
			 WHERE memory_fts MATCH ?1
			 LIMIT ?2`,
			ftsQuery, limit*2)
		if err == nil {
			defer func() { _ = rows.Close() }()
			i := 0
			for rows.Next() {
				var content, sourceTable, sourceID string
				if rows.Scan(&content, &sourceTable, &sourceID) != nil {
					continue
				}
				// Rust: rank-based FTS score: 1.0 - (i * 0.05), min 0.1.
				ftsScore := math.Max(1.0-float64(i)*0.05, 0.1)
				preview := content
				if len(preview) > 200 {
					preview = preview[:200]
				}
				results = append(results, VectorSearchResult{
					SourceTable:    sourceTable,
					SourceID:       sourceID,
					ContentPreview: preview,
					Similarity:     ftsScore * (1.0 - hybridWeight),
				})
				i++
			}
		}
	}

	// Vector leg: cosine similarity via vector index.
	if len(queryEmbedding) > 0 && hybridWeight > 0.0 && vectorIndex != nil && vectorIndex.IsBuilt() {
		vecResults := vectorIndex.Search(queryEmbedding, limit*2)
		for _, r := range vecResults {
			results = append(results, VectorSearchResult{
				SourceTable:    r.SourceTable,
				SourceID:       r.SourceID,
				ContentPreview: r.ContentPreview,
				Similarity:     r.Similarity * hybridWeight,
			})
		}
	}

	// Sort by weighted similarity descending.
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
