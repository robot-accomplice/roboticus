package agent

import (
	"strings"
	"testing"
)

func TestDetectNormalizationFailure_MalformedToolCall_UnbalancedBraces(t *testing.T) {
	content := "Action: web_search\nAction Input: {\"query\": \"rust async\""
	got := DetectNormalizationFailure(content)
	if got != NormMalformedToolCall {
		t.Errorf("expected MalformedToolCall, got %v", got)
	}
}

func TestDetectNormalizationFailure_MalformedToolCall_NoJSON(t *testing.T) {
	content := "Action: web_search\nAction Input: query rust async"
	got := DetectNormalizationFailure(content)
	if got != NormMalformedToolCall {
		t.Errorf("expected MalformedToolCall, got %v", got)
	}
}

func TestDetectNormalizationFailure_NarratedToolUse(t *testing.T) {
	content := "I would use web_search to find recent articles on the topic."
	got := DetectNormalizationFailure(content)
	if got != NormNarratedToolUse {
		t.Errorf("expected NarratedToolUse, got %v", got)
	}
}

func TestDetectNormalizationFailure_EmptyAction_WhitespaceOnly(t *testing.T) {
	content := "   \n\t  "
	got := DetectNormalizationFailure(content)
	if got != NormEmptyAction {
		t.Errorf("expected EmptyAction, got %v", got)
	}
}

func TestDetectNormalizationFailure_NormalToolCall(t *testing.T) {
	content := "Action: web_search\nAction Input: {\"query\": \"rust async\"}"
	got := DetectNormalizationFailure(content)
	if got != 0 {
		t.Errorf("expected no failure, got %v", got)
	}
}

func TestDetectNormalizationFailure_NormalTextResponse(t *testing.T) {
	content := "The answer is 42. Rust is a systems programming language."
	got := DetectNormalizationFailure(content)
	if got != 0 {
		t.Errorf("expected no failure, got %v", got)
	}
}

func TestBuildNormalizationRetryPrompt_IncludesToolCount(t *testing.T) {
	prompt := BuildNormalizationRetryPrompt(NormNarratedToolUse, 7)
	if !strings.Contains(prompt, "7 tools available") {
		t.Errorf("expected prompt to contain '7 tools available', got %q", prompt)
	}
}

func TestDetectNormalizationFailure_EmptyActionLine(t *testing.T) {
	content := "Thought: I should search for this.\nAction:   "
	got := DetectNormalizationFailure(content)
	if got != NormEmptyAction {
		t.Errorf("expected EmptyAction, got %v", got)
	}
}

func TestNormalizationPattern_String(t *testing.T) {
	tests := []struct {
		p    NormalizationPattern
		want string
	}{
		{NormMalformedToolCall, "malformed_tool_call"},
		{NormNarratedToolUse, "narrated_tool_use"},
		{NormEmptyAction, "empty_action"},
		{0, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("NormalizationPattern(%d).String() = %q, want %q", tt.p, got, tt.want)
		}
	}
}
