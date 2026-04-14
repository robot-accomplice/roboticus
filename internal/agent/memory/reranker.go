// reranker.go implements the evidence filter (Layer 11 of the agentic architecture).
//
// The reranker's job is to DISCARD weak evidence, not just reorder it.
// Most weak agents fail because they keep too much — the reranker prevents
// retrieval noise from reaching the reasoning engine.
//
// v1.0.5: score-based filtering with authority/recency adjustments.
// v1.1.0+: LLM-based cross-encoder reranking for semantic precision.

package memory

import (
	"sort"

	"github.com/rs/zerolog/log"
)

// Evidence represents a single retrieval result with provenance metadata.
type Evidence struct {
	Content    string
	SourceTier MemoryTier
	SourceID   string
	Score      float64 // blended retrieval score from HybridSearch
	FTSScore   float64 // per-leg FTS score (0 if not found by FTS)
	VecScore   float64 // per-leg vector score (0 if not found by vector)
	AgeDays    float64 // how old the entry is
	IsCanonical bool   // true for authoritative/policy documents
}

// RerankerConfig controls evidence filtering behavior.
type RerankerConfig struct {
	MinScore         float64 // discard below this threshold (default 0.1)
	AuthorityBoost   float64 // multiplier for canonical sources (default 1.5)
	RecencyPenalty   float64 // multiplier for entries >30 days old (default 0.8)
	RecencyThreshold float64 // days after which recency penalty applies (default 30)
	CollapseSpread   float64 // if top1-topK spread < this, collapse detected (default 0.05)
	MaxOnCollapse    int     // max results to return when collapse detected (default 3)
}

// DefaultRerankerConfig returns sensible defaults.
func DefaultRerankerConfig() RerankerConfig {
	return RerankerConfig{
		MinScore:         0.1,
		AuthorityBoost:   1.5,
		RecencyPenalty:   0.8,
		RecencyThreshold: 30.0,
		CollapseSpread:   0.05,
		MaxOnCollapse:    3,
	}
}

// Reranker filters and scores evidence from retrieval.
type Reranker struct {
	config RerankerConfig
}

// NewReranker creates a reranker with the given config.
func NewReranker(cfg RerankerConfig) *Reranker {
	return &Reranker{config: cfg}
}

// Filter takes raw retrieval candidates, applies scoring adjustments,
// discards weak evidence, and returns the survivors ranked by adjusted score.
//
// The returned slice is always shorter than or equal to the input — the
// reranker's primary job is elimination, not reordering.
func (rr *Reranker) Filter(candidates []Evidence, maxResults int) []Evidence {
	if len(candidates) == 0 || maxResults <= 0 {
		return nil
	}

	// Phase 1: Adjust scores based on authority and recency.
	adjusted := make([]Evidence, 0, len(candidates))
	for _, c := range candidates {
		score := c.Score

		// Authority boost: canonical/policy documents are more trustworthy.
		if c.IsCanonical {
			score *= rr.config.AuthorityBoost
		}

		// Recency penalty: old entries without FTS match are less relevant.
		// FTS-matched old entries are explicitly query-relevant, so they're exempt.
		if c.AgeDays > rr.config.RecencyThreshold && c.FTSScore == 0 {
			score *= rr.config.RecencyPenalty
		}

		c.Score = score
		adjusted = append(adjusted, c)
	}

	// Phase 2: Sort by adjusted score descending.
	sort.Slice(adjusted, func(i, j int) bool {
		return adjusted[i].Score > adjusted[j].Score
	})

	// Phase 3: Discard below minimum threshold.
	var survivors []Evidence
	for _, c := range adjusted {
		if c.Score < rr.config.MinScore {
			continue
		}
		survivors = append(survivors, c)
	}

	discarded := len(candidates) - len(survivors)
	log.Debug().
		Int("input", len(candidates)).
		Int("survived", len(survivors)).
		Int("discarded", discarded).
		Msg("reranker: evidence filtered")

	if len(survivors) == 0 {
		return nil
	}

	// Phase 4: Collapse detection.
	// If the spread between top and bottom scores is tiny, all results
	// are scoring alike — semantic collapse is happening. Return fewer
	// results to avoid feeding the model undifferentiated noise.
	if len(survivors) >= 2 {
		spread := survivors[0].Score - survivors[len(survivors)-1].Score
		if spread < rr.config.CollapseSpread {
			limit := rr.config.MaxOnCollapse
			if limit > len(survivors) {
				limit = len(survivors)
			}
			log.Warn().
				Float64("spread", spread).
				Int("capped_to", limit).
				Msg("reranker: collapse detected — score spread near zero, capping results")
			survivors = survivors[:limit]
		}
	}

	// Phase 5: Trim to max results.
	if len(survivors) > maxResults {
		survivors = survivors[:maxResults]
	}

	return survivors
}
