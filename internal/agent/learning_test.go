package agent

import (
	"testing"
)

func TestLearningExtractor_ExtractToolUse(t *testing.T) {
	le := NewLearningExtractor()

	results := []string{"tool output A", "tool output B"}
	patterns := le.ExtractFromTurn("Run the thing", results, true)

	// Should produce one pattern per tool result on success
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns from 2 tool results, got %d", len(patterns))
	}
	for _, p := range patterns {
		if p.Pattern != "successful_tool_use" {
			t.Errorf("expected pattern 'successful_tool_use', got %q", p.Pattern)
		}
		if p.SuccessCount != 1 {
			t.Errorf("expected SuccessCount 1, got %d", p.SuccessCount)
		}
	}
}

func TestLearningExtractor_ExtractToolUseFailure(t *testing.T) {
	le := NewLearningExtractor()

	results := []string{"some output"}
	patterns := le.ExtractFromTurn("Do something", results, false)

	// Failed turn should not produce successful_tool_use patterns
	for _, p := range patterns {
		if p.Pattern == "successful_tool_use" {
			t.Errorf("should not extract successful_tool_use for failed turn")
		}
	}
}

func TestLearningExtractor_ProceduralQueryDetection(t *testing.T) {
	le := NewLearningExtractor()

	patterns := le.ExtractFromTurn("How to deploy the service?", nil, true)
	if len(patterns) == 0 {
		t.Fatal("expected at least one pattern for 'how to' query")
	}
	found := false
	for _, p := range patterns {
		if p.Pattern == "procedural_query" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'procedural_query' pattern for 'how to' content")
	}

	// Case insensitive check
	patterns2 := le.ExtractFromTurn("HOW TO restart nginx?", nil, false)
	found2 := false
	for _, p := range patterns2 {
		if p.Pattern == "procedural_query" {
			found2 = true
		}
	}
	if !found2 {
		t.Error("expected 'procedural_query' to be case-insensitive")
	}
}

func TestLearningExtractor_RecordOutcomeAndSuccessRate(t *testing.T) {
	le := NewLearningExtractor()

	le.Register(LearnedPattern{ID: "p1", Pattern: "some_pattern"})

	// Unknown pattern — should be no-op
	le.RecordOutcome("nonexistent", true)

	le.RecordOutcome("p1", true)
	le.RecordOutcome("p1", true)
	le.RecordOutcome("p1", false)

	rate := le.SuccessRate("p1")
	expected := 2.0 / 3.0
	if rate < expected-0.001 || rate > expected+0.001 {
		t.Errorf("expected success rate ~%.4f, got %.4f", expected, rate)
	}
}

func TestLearningExtractor_SuccessRateUnknownPattern(t *testing.T) {
	le := NewLearningExtractor()
	rate := le.SuccessRate("does_not_exist")
	if rate != 0 {
		t.Errorf("expected 0 for unknown pattern, got %f", rate)
	}
}

func TestLearningExtractor_SuccessRateNoOutcomes(t *testing.T) {
	le := NewLearningExtractor()
	le.Register(LearnedPattern{ID: "p2", Pattern: "empty"})
	rate := le.SuccessRate("p2")
	if rate != 0 {
		t.Errorf("expected 0 rate with no outcomes, got %f", rate)
	}
}

func TestLearningExtractor_EmptyTurn(t *testing.T) {
	le := NewLearningExtractor()
	patterns := le.ExtractFromTurn("", nil, true)
	if len(patterns) != 0 {
		t.Errorf("empty turn with no tool results should produce no patterns, got %d", len(patterns))
	}
}
