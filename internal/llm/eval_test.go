package llm

import "testing"

func TestDefaultEvalCorpus_CoversAllTiers(t *testing.T) {
	corpus := DefaultEvalCorpus()
	seen := make(map[ModelTier]bool)
	for _, tc := range corpus {
		seen[tc.ExpectedTier] = true
	}

	for _, tier := range []ModelTier{TierSmall, TierMedium, TierLarge, TierFrontier} {
		if !seen[tier] {
			t.Errorf("DefaultEvalCorpus is missing cases for tier %d", tier)
		}
	}
}

func TestRunEval_ProducesValidResults(t *testing.T) {
	router := NewRouter([]RouteTarget{
		{Model: "small", Tier: TierSmall},
		{Model: "medium", Tier: TierMedium},
		{Model: "large", Tier: TierLarge},
		{Model: "frontier", Tier: TierFrontier},
	}, RouterConfig{})

	corpus := DefaultEvalCorpus()
	result := RunEval(router, corpus)

	if result.Total != len(corpus) {
		t.Errorf("Total = %d, want %d", result.Total, len(corpus))
	}
	if result.Correct < 0 || result.Correct > result.Total {
		t.Errorf("Correct = %d, out of valid range [0, %d]", result.Correct, result.Total)
	}
	if result.Accuracy < 0 || result.Accuracy > 1 {
		t.Errorf("Accuracy = %f, want value in [0, 1]", result.Accuracy)
	}
	if result.Correct+len(result.Errors) != result.Total {
		t.Errorf("Correct(%d) + Errors(%d) != Total(%d)", result.Correct, len(result.Errors), result.Total)
	}

	// Verify ByTier totals sum to Total.
	tierSum := 0
	for _, tr := range result.ByTier {
		tierSum += tr.Total
	}
	if tierSum != result.Total {
		t.Errorf("sum of ByTier totals = %d, want %d", tierSum, result.Total)
	}
}

func TestRunEval_PerfectRouter(t *testing.T) {
	// Build a corpus where the complexity heuristic will produce the exact
	// expected tier, so we expect 100% accuracy. We use the default corpus
	// which is designed to match the heuristics.
	router := NewRouter([]RouteTarget{
		{Model: "small", Tier: TierSmall},
		{Model: "medium", Tier: TierMedium},
		{Model: "large", Tier: TierLarge},
		{Model: "frontier", Tier: TierFrontier},
	}, RouterConfig{})

	corpus := DefaultEvalCorpus()
	result := RunEval(router, corpus)

	if result.Accuracy != 1.0 {
		t.Errorf("expected 100%% accuracy on default corpus, got %.2f%%", result.Accuracy*100)
		// Find the original EvalCase for each error to reconstruct the full request.
		casesByPrompt := make(map[string]EvalCase, len(corpus))
		for _, tc := range corpus {
			casesByPrompt[tc.Prompt] = tc
		}
		for _, e := range result.Errors {
			// Show the complexity score for debugging.
			tc := casesByPrompt[e.Prompt]
			req := buildEvalRequest(tc)
			complexity := estimateComplexity(req)
			promptSnippet := e.Prompt
			if len(promptSnippet) > 80 {
				promptSnippet = promptSnippet[:80] + "..."
			}
			t.Logf("  mismatch: prompt=%q expected=%d got=%d complexity=%.3f",
				promptSnippet, e.Expected, e.Got, complexity)
		}
	}
}

func TestRunEval_EmptyCorpus(t *testing.T) {
	router := NewRouter([]RouteTarget{
		{Model: "small", Tier: TierSmall},
	}, RouterConfig{})

	result := RunEval(router, nil)
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0 for empty corpus", result.Total)
	}
	if result.Accuracy != 0 {
		t.Errorf("Accuracy = %f, want 0 for empty corpus", result.Accuracy)
	}
}
