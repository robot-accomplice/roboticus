package memory

import (
	"context"
	"testing"
)

func TestFuser_CorroboratedCanonicalFreshEvidenceRanksFirst(t *testing.T) {
	fuser := NewFuser(DefaultFusionConfig())

	candidates := []Evidence{
		{
			Content:        "Billing service depends on ledger service for invoice settlement",
			SourceTier:     TierSemantic,
			Score:          0.55,
			IsCanonical:    true,
			AuthorityScore: 0.9,
			AgeDays:        2,
			RouteWeight:    0.4,
			PositionDecay:  1.0,
		},
		{
			Content:       "Last outage showed billing service depends on ledger service",
			SourceTier:    TierEpisodic,
			Score:         0.50,
			AgeDays:       1,
			RouteWeight:   0.35,
			PositionDecay: 1.0,
		},
		{
			Content:       "Old cache note about redis failover procedure",
			SourceTier:    TierProcedural,
			Score:         0.72,
			AgeDays:       90,
			RouteWeight:   0.15,
			PositionDecay: 1.0,
		},
	}

	fused, summary := fuser.Fuse(candidates)

	if len(fused) != 3 {
		t.Fatalf("expected 3 fused candidates, got %d", len(fused))
	}
	if summary.CorroboratedCount < 2 {
		t.Fatalf("expected corroborated count >= 2, got %d", summary.CorroboratedCount)
	}
	if fused[0].SourceTier != TierSemantic {
		t.Fatalf("expected corroborated canonical semantic evidence first, got %+v", fused[0])
	}
	if fused[0].FusionCorroborationFactor <= 1.0 {
		t.Fatalf("expected corroboration boost on top result, got %.2f", fused[0].FusionCorroborationFactor)
	}
	if fused[0].FusionAuthorityFactor <= 1.0 {
		t.Fatalf("expected authority boost on top result, got %.2f", fused[0].FusionAuthorityFactor)
	}
	if fused[2].FusionFreshnessFactor >= 1.0 {
		t.Fatalf("expected stale evidence freshness penalty, got %.2f", fused[2].FusionFreshnessFactor)
	}
}

func TestAnnotateFusionSummary_EmitsTraceCounters(t *testing.T) {
	tracer := &fixtureTracer{}
	ctx := WithRetrievalTracer(context.Background(), tracer)

	annotateFusionSummary(ctx, FusionSummary{
		InputCount:         4,
		CorroboratedCount:  2,
		CanonicalCount:     1,
		FreshnessPenalized: 1,
		TopScore:           1.42,
	})

	for _, key := range []string{
		"retrieval.fusion.input",
		"retrieval.fusion.corroborated",
		"retrieval.fusion.canonical",
		"retrieval.fusion.freshness_penalized",
		"retrieval.fusion.top_score",
	} {
		if _, ok := tracer.get(key); !ok {
			t.Fatalf("expected %s annotation, got %+v", key, tracer.entries)
		}
	}
}
