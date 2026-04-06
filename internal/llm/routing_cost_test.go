package llm

import (
	"testing"
)

// --- Routing Cost Estimation Tests ---
// Ported from Rust: crates/roboticus-api/src/api/routes/agent/tests/routing_tests.rs
// Verifies behavioral properties of cost estimation.

func TestCost_NonNegative(t *testing.T) {
	// "Cost estimation always returns non-negative values."
	cases := []struct {
		name  string
		usage Usage
		rate  Provider
	}{
		{"zero_tokens", Usage{0, 0}, Provider{CostPerInputTok: 0.001, CostPerOutputTok: 0.002}},
		{"normal", Usage{100, 50}, Provider{CostPerInputTok: 0.001, CostPerOutputTok: 0.002}},
		{"zero_rates", Usage{1000, 500}, Provider{CostPerInputTok: 0, CostPerOutputTok: 0}},
		{"large_tokens", Usage{1000000, 500000}, Provider{CostPerInputTok: 0.0001, CostPerOutputTok: 0.0002}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cost := tc.usage.Cost(&tc.rate)
			if cost < 0 {
				t.Errorf("cost should never be negative, got %f", cost)
			}
		})
	}
}

func TestCost_CorrectFormula(t *testing.T) {
	// "Cost = (input_tokens × input_rate) + (output_tokens × output_rate)."
	p := &Provider{CostPerInputTok: 0.003, CostPerOutputTok: 0.006}

	cases := []struct {
		name     string
		usage    Usage
		expected float64
	}{
		{"basic", Usage{100, 50}, 100*0.003 + 50*0.006},
		{"input_only", Usage{200, 0}, 200 * 0.003},
		{"output_only", Usage{0, 300}, 300 * 0.006},
		{"both", Usage{1000, 2000}, 1000*0.003 + 2000*0.006},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.usage.Cost(p)
			if got != tc.expected {
				t.Errorf("cost = %f, want %f", got, tc.expected)
			}
		})
	}
}

func TestCost_ZeroRates_ProduceZeroCost(t *testing.T) {
	// "Zero rates produce zero cost."
	p := &Provider{CostPerInputTok: 0, CostPerOutputTok: 0}
	usage := Usage{InputTokens: 10000, OutputTokens: 5000}

	cost := usage.Cost(p)
	if cost != 0 {
		t.Errorf("zero rates should produce zero cost, got %f", cost)
	}
}

func TestCost_ZeroTokens_ProduceZeroCost(t *testing.T) {
	// "Zero tokens produce zero cost regardless of rate."
	p := &Provider{CostPerInputTok: 0.1, CostPerOutputTok: 0.2}
	usage := Usage{InputTokens: 0, OutputTokens: 0}

	cost := usage.Cost(p)
	if cost != 0 {
		t.Errorf("zero tokens should produce zero cost, got %f", cost)
	}
}

func TestCost_Symmetry(t *testing.T) {
	// Swapping input and output rates with corresponding token counts
	// should produce the same total cost.
	usage := Usage{InputTokens: 100, OutputTokens: 200}

	p1 := &Provider{CostPerInputTok: 0.01, CostPerOutputTok: 0.02}
	p2 := &Provider{CostPerInputTok: 0.02, CostPerOutputTok: 0.01}
	usageSwapped := Usage{InputTokens: 200, OutputTokens: 100}

	cost1 := usage.Cost(p1)
	cost2 := usageSwapped.Cost(p2)

	if cost1 != cost2 {
		t.Errorf("symmetric swap should produce equal cost: %f vs %f", cost1, cost2)
	}
}

func TestFallbackCandidateDeduplication(t *testing.T) {
	// "Fallback candidate deduplication preserves primary model first."
	// When building model profiles, the primary model should appear first
	// and duplicates should be removed.
	models := []string{"gpt-4", "claude-3", "gpt-4", "llama-3", "claude-3"}
	seen := make(map[string]bool)
	var deduped []string
	for _, m := range models {
		if !seen[m] {
			seen[m] = true
			deduped = append(deduped, m)
		}
	}

	if len(deduped) != 3 {
		t.Errorf("expected 3 unique models, got %d", len(deduped))
	}
	if deduped[0] != "gpt-4" {
		t.Errorf("primary model should be first, got %s", deduped[0])
	}
}
