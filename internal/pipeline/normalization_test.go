package pipeline

import "testing"

func TestDetectNormalization_EmptyAction(t *testing.T) {
	tests := []struct {
		input string
		want  NormalizationPattern
	}{
		{"", PatternEmptyAction},
		{"   ", PatternEmptyAction},
		{"Action:  ", PatternEmptyAction},
	}
	for _, tt := range tests {
		got := DetectNormalizationIssue(tt.input)
		if got != tt.want {
			t.Errorf("DetectNormalizationIssue(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDetectNormalization_MalformedToolCall(t *testing.T) {
	tests := []struct {
		input string
		want  NormalizationPattern
	}{
		{"Action: search\nAction Input: {\"query\": \"test\"", PatternMalformedToolCall}, // missing close brace
		{"tool_call without any json", PatternMalformedToolCall},                         // no braces at all
	}
	for _, tt := range tests {
		got := DetectNormalizationIssue(tt.input)
		if got != tt.want {
			t.Errorf("DetectNormalizationIssue(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDetectNormalization_NarratedToolUse(t *testing.T) {
	tests := []struct {
		input string
		want  NormalizationPattern
	}{
		{"I would use the search tool to find that information.", PatternNarratedToolUse},
		{"Let me use the bash tool to run that command.", PatternNarratedToolUse},
		{"I should call the read_file function.", PatternNarratedToolUse},
	}
	for _, tt := range tests {
		got := DetectNormalizationIssue(tt.input)
		if got != tt.want {
			t.Errorf("DetectNormalizationIssue(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDetectNormalization_Clean(t *testing.T) {
	clean := "The capital of France is Paris."
	got := DetectNormalizationIssue(clean)
	if got != PatternNone {
		t.Errorf("clean text detected as %v, want PatternNone", got)
	}
}

func TestNormalizationRetryPrompt(t *testing.T) {
	p := NormalizationRetryPrompt(PatternMalformedToolCall, 10)
	if p == "" {
		t.Error("should return non-empty prompt for malformed tool call")
	}
	p = NormalizationRetryPrompt(PatternNarratedToolUse, 5)
	if p == "" {
		t.Error("should return non-empty prompt for narrated tool use")
	}
	p = NormalizationRetryPrompt(PatternEmptyAction, 0)
	if p == "" {
		t.Error("should return non-empty prompt for empty action")
	}
}
