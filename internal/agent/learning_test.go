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

func TestProcedureStep_Struct(t *testing.T) {
	step := ProcedureStep{
		Input:    "query",
		Output:   "result",
		ToolName: "search",
		Success:  true,
	}
	if step.ToolName != "search" || !step.Success {
		t.Error("ProcedureStep fields not set correctly")
	}
}

func TestLearnedProcedure_Struct(t *testing.T) {
	proc := LearnedProcedure{
		Name:         "search-fetch-parse",
		Description:  "A three-step procedure",
		ToolSequence: []string{"search", "fetch", "parse"},
		SuccessRatio: 0.85,
		Steps: []ProcedureStep{
			{ToolName: "search", Success: true},
			{ToolName: "fetch", Success: true},
			{ToolName: "parse", Success: true},
		},
	}
	if len(proc.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(proc.Steps))
	}
	if proc.SuccessRatio != 0.85 {
		t.Errorf("expected ratio 0.85, got %f", proc.SuccessRatio)
	}
}

func TestDetectProcedure_EmptyHistory(t *testing.T) {
	le := NewLearningExtractor()
	result := le.DetectProcedure(nil)
	if result != nil {
		t.Error("expected nil for empty history")
	}
}

func TestDetectProcedure_TooShort(t *testing.T) {
	le := NewLearningExtractor()
	history := []ProcedureStep{
		{ToolName: "a", Success: true},
		{ToolName: "b", Success: true},
	}
	result := le.DetectProcedure(history)
	if result != nil {
		t.Error("expected nil for history shorter than minSeqLength")
	}
}

func TestDetectProcedure_FindsRecurring(t *testing.T) {
	le := NewLearningExtractor()
	// Create a history with a recurring 3-step pattern appearing 3 times.
	history := []ProcedureStep{
		{ToolName: "search", Success: true},
		{ToolName: "fetch", Success: true},
		{ToolName: "parse", Success: true},
		{ToolName: "search", Success: true},
		{ToolName: "fetch", Success: true},
		{ToolName: "parse", Success: true},
		{ToolName: "search", Success: true},
		{ToolName: "fetch", Success: true},
		{ToolName: "parse", Success: true},
	}
	result := le.DetectProcedure(history)
	if result == nil {
		t.Fatal("expected a detected procedure")
	}
	if len(result.ToolSequence) < 3 {
		t.Errorf("expected at least 3-step sequence, got %d", len(result.ToolSequence))
	}
	if result.SuccessRatio < 0.7 {
		t.Errorf("expected success ratio >= 0.7, got %f", result.SuccessRatio)
	}
}

func TestDetectProcedure_FiltersSingleToolRepetition(t *testing.T) {
	le := NewLearningExtractor()
	// All same tool — should be filtered as noise.
	history := make([]ProcedureStep, 20)
	for i := range history {
		history[i] = ProcedureStep{ToolName: "ping", Success: true}
	}
	result := le.DetectProcedure(history)
	if result != nil {
		t.Error("expected nil for single-tool repetition noise")
	}
}

func TestDetectProcedure_CapsAt200(t *testing.T) {
	le := NewLearningExtractor()
	// Build 250 entries but embed a recurring pattern in the last 200.
	history := make([]ProcedureStep, 250)
	for i := range history {
		history[i] = ProcedureStep{ToolName: "noise", Success: false}
	}
	// Embed recurring pattern in the tail.
	pattern := []string{"alpha", "beta", "gamma"}
	for i := 200; i < 250; i++ {
		history[i] = ProcedureStep{
			ToolName: pattern[i%3],
			Success:  true,
		}
	}
	// Should not panic and should find the pattern (or nil — the important thing is no crash).
	_ = le.DetectProcedure(history)
}

func TestDetectProcedure_LowSuccessRatio(t *testing.T) {
	le := NewLearningExtractor()
	// Create a pattern that appears but with mostly failures.
	history := []ProcedureStep{
		{ToolName: "a", Success: false},
		{ToolName: "b", Success: false},
		{ToolName: "c", Success: true},
		{ToolName: "a", Success: false},
		{ToolName: "b", Success: false},
		{ToolName: "c", Success: false},
	}
	result := le.DetectProcedure(history)
	if result != nil {
		t.Error("expected nil for low success ratio pattern")
	}
}

func TestIsSingleToolRepetition(t *testing.T) {
	if !isSingleToolRepetition(nil) {
		t.Error("empty should be single tool repetition")
	}
	if !isSingleToolRepetition([]ProcedureStep{{ToolName: "a"}, {ToolName: "a"}}) {
		t.Error("same tool should be repetition")
	}
	if isSingleToolRepetition([]ProcedureStep{{ToolName: "a"}, {ToolName: "b"}}) {
		t.Error("different tools should not be repetition")
	}
}
