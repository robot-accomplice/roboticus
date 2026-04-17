package memory

import "testing"

func TestReranker_DiscardsWeakEvidence(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	candidates := []Evidence{
		{Content: "strong", Score: 0.8, SourceTier: TierSemantic},
		{Content: "medium", Score: 0.4, SourceTier: TierEpisodic},
		{Content: "weak", Score: 0.05, SourceTier: TierEpisodic},    // below threshold
		{Content: "garbage", Score: 0.01, SourceTier: TierSemantic}, // below threshold
	}

	results := rr.Filter(candidates, 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 survivors (2 below MinScore=0.1), got %d", len(results))
	}
	if results[0].Content != "strong" {
		t.Errorf("expected 'strong' first, got %q", results[0].Content)
	}
}

func TestReranker_AuthorityBoost(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	candidates := []Evidence{
		{Content: "regular", Score: 0.6, SourceTier: TierEpisodic},
		{Content: "canonical", Score: 0.5, SourceTier: TierSemantic, IsCanonical: true},
	}

	results := rr.Filter(candidates, 10)

	if len(results) < 2 {
		t.Fatal("expected 2 results")
	}
	// Canonical doc (0.5 * 1.5 = 0.75) should outrank regular (0.6).
	if results[0].Content != "canonical" {
		t.Errorf("canonical source should rank first after authority boost, got %q", results[0].Content)
	}
}

func TestReranker_RecencyPenalty(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	candidates := []Evidence{
		{Content: "recent", Score: 0.5, SourceTier: TierEpisodic, AgeDays: 1},
		{Content: "old-no-fts", Score: 0.6, SourceTier: TierEpisodic, AgeDays: 60},
		{Content: "old-with-fts", Score: 0.55, SourceTier: TierEpisodic, AgeDays: 60, FTSScore: 0.3},
	}

	results := rr.Filter(candidates, 10)

	if len(results) < 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// old-with-fts (0.55, no penalty — FTS-matched) should outrank
	// old-no-fts (0.6 * 0.8 = 0.48).
	scoreByContent := make(map[string]float64)
	for _, r := range results {
		scoreByContent[r.Content] = r.Score
	}

	if scoreByContent["old-no-fts"] >= scoreByContent["old-with-fts"] {
		t.Errorf("old entry without FTS (%.2f) should score lower than old entry with FTS (%.2f) after recency penalty",
			scoreByContent["old-no-fts"], scoreByContent["old-with-fts"])
	}
}

func TestReranker_CollapseDetection(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	// All candidates score nearly the same — collapse signal.
	candidates := []Evidence{
		{Content: "a", Score: 0.50, SourceTier: TierSemantic},
		{Content: "b", Score: 0.49, SourceTier: TierSemantic},
		{Content: "c", Score: 0.48, SourceTier: TierSemantic},
		{Content: "d", Score: 0.47, SourceTier: TierSemantic},
		{Content: "e", Score: 0.46, SourceTier: TierSemantic},
	}

	results := rr.Filter(candidates, 10)

	// Spread = 0.50 - 0.46 = 0.04 < 0.05 threshold → collapse capped to 3.
	if len(results) != 3 {
		t.Errorf("collapse detection should cap to 3 results, got %d", len(results))
	}
}

func TestReranker_NoCollapseWhenSpreadHealthy(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	candidates := []Evidence{
		{Content: "top", Score: 0.9, SourceTier: TierSemantic},
		{Content: "mid", Score: 0.5, SourceTier: TierEpisodic},
		{Content: "low", Score: 0.2, SourceTier: TierProcedural},
	}

	results := rr.Filter(candidates, 10)

	// Spread = 0.9 - 0.2 = 0.7 >> 0.05 → no collapse capping.
	if len(results) != 3 {
		t.Errorf("healthy spread should preserve all results, got %d", len(results))
	}
}

func TestReranker_MaxResults(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	var candidates []Evidence
	for i := 0; i < 20; i++ {
		candidates = append(candidates, Evidence{
			Content:    "entry",
			Score:      0.5 + float64(i)*0.02,
			SourceTier: TierSemantic,
		})
	}

	results := rr.Filter(candidates, 5)

	if len(results) > 5 {
		t.Errorf("expected at most 5 results, got %d", len(results))
	}
}

func TestReranker_EmptyInput(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	if results := rr.Filter(nil, 10); results != nil {
		t.Errorf("expected nil for empty input, got %v", results)
	}
	if results := rr.Filter([]Evidence{}, 10); results != nil {
		t.Errorf("expected nil for empty slice, got %v", results)
	}
}

func TestReranker_ZeroMaxResults(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	candidates := []Evidence{{Content: "a", Score: 0.5}}
	if results := rr.Filter(candidates, 0); results != nil {
		t.Errorf("expected nil for maxResults=0, got %v", results)
	}
}

func TestReranker_AllDiscarded(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	candidates := []Evidence{
		{Content: "garbage1", Score: 0.01},
		{Content: "garbage2", Score: 0.02},
	}

	results := rr.Filter(candidates, 10)
	if results != nil {
		t.Errorf("expected nil when all candidates below threshold, got %d results", len(results))
	}
}

func TestReranker_PreservesProvenance(t *testing.T) {
	rr := NewReranker(DefaultRerankerConfig())

	candidates := []Evidence{
		{Content: "fact", Score: 0.7, SourceTier: TierSemantic, SourceID: "sem-42", IsCanonical: true},
	}

	results := rr.Filter(candidates, 10)

	if len(results) != 1 {
		t.Fatal("expected 1 result")
	}
	if results[0].SourceTier != TierSemantic {
		t.Error("source tier should be preserved through reranking")
	}
	if results[0].SourceID != "sem-42" {
		t.Error("source ID should be preserved through reranking")
	}
	if !results[0].IsCanonical {
		t.Error("canonical flag should be preserved through reranking")
	}
}
