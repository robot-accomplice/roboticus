package memory

import (
	"context"
	"sort"
	"strings"
)

// FusionConfig controls the explicit evidence-fusion stage that runs between
// tier retrieval and reranking.
type FusionConfig struct {
	RouteWeightBonus       float64
	AuthorityWeight        float64
	FreshnessPenalty       float64
	FreshnessThresholdDays float64
	MinFreshnessFactor     float64
	CorroborationWeight    float64
	MinSharedTerms         int
}

// DefaultFusionConfig returns the default fusion behavior for the live path.
func DefaultFusionConfig() FusionConfig {
	return FusionConfig{
		RouteWeightBonus:       0.35,
		AuthorityWeight:        0.35,
		FreshnessPenalty:       0.25,
		FreshnessThresholdDays: 30.0,
		MinFreshnessFactor:     0.65,
		CorroborationWeight:    0.15,
		MinSharedTerms:         2,
	}
}

// FusionSummary is the operator/trace-facing aggregate for a fusion pass.
type FusionSummary struct {
	InputCount         int
	CorroboratedCount  int
	CanonicalCount     int
	FreshnessPenalized int
	TopScore           float64
}

// Fuser applies a single owned fusion stage to raw retrieval candidates.
type Fuser struct {
	config FusionConfig
}

// NewFuser creates a fuser with the provided config.
func NewFuser(cfg FusionConfig) *Fuser {
	return &Fuser{config: cfg}
}

// Fuse computes the first unified retrieval-quality score for each candidate.
// The returned slice is stably sorted by fused score descending.
func (f *Fuser) Fuse(candidates []Evidence) ([]Evidence, FusionSummary) {
	summary := FusionSummary{InputCount: len(candidates)}
	if len(candidates) == 0 {
		return nil, summary
	}

	fused := make([]Evidence, len(candidates))
	copy(fused, candidates)

	shared := buildSharedTermMatrix(fused, f.config.MinSharedTerms)
	for i := range fused {
		ev := &fused[i]
		ev.FusionBaseScore = fusionBaseScore(*ev)
		ev.FusionRouteFactor = 1.0 + (ev.RouteWeight * f.config.RouteWeightBonus)
		if ev.PositionDecay <= 0 {
			ev.PositionDecay = 1.0
		}
		ev.FusionAuthorityFactor = fusionAuthorityFactor(*ev, f.config)
		ev.FusionFreshnessFactor = fusionFreshnessFactor(*ev, f.config)
		ev.CorroborationCount = shared[i]
		ev.FusionCorroborationFactor = 1.0 + (float64(ev.CorroborationCount) * f.config.CorroborationWeight)
		ev.FusionScore = ev.FusionBaseScore *
			ev.FusionRouteFactor *
			ev.PositionDecay *
			ev.FusionAuthorityFactor *
			ev.FusionFreshnessFactor *
			ev.FusionCorroborationFactor
		ev.Score = ev.FusionScore

		if ev.CorroborationCount > 0 {
			summary.CorroboratedCount++
		}
		if ev.IsCanonical {
			summary.CanonicalCount++
		}
		if ev.FusionFreshnessFactor < 1.0 {
			summary.FreshnessPenalized++
		}
		if ev.FusionScore > summary.TopScore {
			summary.TopScore = ev.FusionScore
		}
	}

	sort.SliceStable(fused, func(i, j int) bool {
		return fused[i].FusionScore > fused[j].FusionScore
	})
	return fused, summary
}

func annotateFusionSummary(ctx context.Context, summary FusionSummary) {
	tracer := retrievalTracerFromContext(ctx)
	if tracer == nil {
		return
	}
	tracer.Annotate("retrieval.fusion.input", summary.InputCount)
	tracer.Annotate("retrieval.fusion.corroborated", summary.CorroboratedCount)
	tracer.Annotate("retrieval.fusion.canonical", summary.CanonicalCount)
	tracer.Annotate("retrieval.fusion.freshness_penalized", summary.FreshnessPenalized)
	tracer.Annotate("retrieval.fusion.top_score", summary.TopScore)
}

func fusionBaseScore(ev Evidence) float64 {
	switch {
	case ev.Score > 0:
		return ev.Score
	case ev.FTSScore > 0 && ev.VecScore > 0:
		return (ev.FTSScore + ev.VecScore) / 2
	case ev.FTSScore > 0:
		return ev.FTSScore
	case ev.VecScore > 0:
		return ev.VecScore
	default:
		if ev.RouteWeight > 0 {
			return ev.RouteWeight
		}
		return 0.05
	}
}

func fusionAuthorityFactor(ev Evidence, cfg FusionConfig) float64 {
	factor := 1.0 + (ev.AuthorityScore * cfg.AuthorityWeight)
	if ev.IsCanonical {
		factor += cfg.AuthorityWeight
	}
	return factor
}

func fusionFreshnessFactor(ev Evidence, cfg FusionConfig) float64 {
	if ev.AgeDays <= 0 || ev.AgeDays <= cfg.FreshnessThresholdDays {
		return 1.0
	}
	if ev.FTSScore > 0 {
		return 1.0
	}
	over := (ev.AgeDays - cfg.FreshnessThresholdDays) / cfg.FreshnessThresholdDays
	if over > 1 {
		over = 1
	}
	factor := 1.0 - (over * cfg.FreshnessPenalty)
	if factor < cfg.MinFreshnessFactor {
		return cfg.MinFreshnessFactor
	}
	return factor
}

func buildSharedTermMatrix(candidates []Evidence, minSharedTerms int) []int {
	if len(candidates) == 0 {
		return nil
	}
	tokenSets := make([]map[string]struct{}, len(candidates))
	for i, ev := range candidates {
		tokenSets[i] = fusionTerms(ev.Content)
	}
	shared := make([]int, len(candidates))
	for i := range candidates {
		for j := range candidates {
			if i == j {
				continue
			}
			if candidates[i].SourceTier == candidates[j].SourceTier {
				continue
			}
			if sharedTermCount(tokenSets[i], tokenSets[j]) >= minSharedTerms {
				shared[i]++
			}
		}
	}
	return shared
}

func fusionTerms(content string) map[string]struct{} {
	raw := strings.Fields(strings.ToLower(content))
	out := make(map[string]struct{}, len(raw))
	for _, tok := range raw {
		tok = strings.Trim(tok, ".,;:!?()[]{}<>\"'`")
		if len(tok) < 4 {
			continue
		}
		switch tok {
		case "that", "this", "with", "from", "into", "there", "their", "about", "because", "which":
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

func sharedTermCount(a, b map[string]struct{}) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	count := 0
	for k := range a {
		if _, ok := b[k]; ok {
			count++
		}
	}
	return count
}
