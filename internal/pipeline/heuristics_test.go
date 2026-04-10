package pipeline

import (
	"strings"
	"testing"
)

func TestIsShortFollowupForPreviousReply(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"what's that from?", true},
		{"Source?", true},
		{"Tell me about quantum computing", false},
		{strings.Repeat("a", 100), false},
	}
	for _, tt := range tests {
		if got := isShortFollowupForPreviousReply(tt.input); got != tt.want {
			t.Errorf("isShortFollowupForPreviousReply(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsShortReactiveSarcasm(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"sure", true},
		{"wow", true},
		{"right.", true},
		{"right...", true},
		{"lol sure", false}, // not exact match
		{"Can you explain the delegation architecture?", false},
		{strings.Repeat("a", 40), false}, // too long
	}
	for _, tt := range tests {
		if got := isShortReactiveSarcasm(tt.input); got != tt.want {
			t.Errorf("isShortReactiveSarcasm(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsShortContradictionFollowup(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"no that's wrong", true},
		{"that's incorrect", true},
		{"incorrect", true},
		{"Tell me more about that", false},
	}
	for _, tt := range tests {
		if got := isShortContradictionFollowup(tt.input); got != tt.want {
			t.Errorf("isShortContradictionFollowup(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestContextualizeShortFollowup_NormalPassthrough(t *testing.T) {
	sess := newTestSession("s1")
	expanded, correction := ContextualizeShortFollowup(sess, "Normal question")
	if expanded != "Normal question" {
		t.Errorf("expected passthrough, got %q", expanded)
	}
	if correction {
		t.Error("normal question should not be correction")
	}
}

func TestContextualizeShortFollowup_SarcasmExpands(t *testing.T) {
	sess := newTestSession("s-sarcasm")
	sess.AddUserMessage("What is 2+2?")
	sess.AddAssistantMessage("2+2 equals 5.", nil)
	expanded, correction := ContextualizeShortFollowup(sess, "sure")
	if !strings.Contains(expanded, "sarcasm") {
		t.Errorf("expected sarcasm context, got %q", expanded)
	}
	if !strings.Contains(expanded, "2+2 equals 5") {
		t.Errorf("expected previous reply excerpt, got %q", expanded)
	}
	if !correction {
		t.Error("sarcasm should be correction turn")
	}
}

func TestContextualizeShortFollowup_ContradictionExpands(t *testing.T) {
	sess := newTestSession("s-contra")
	sess.AddUserMessage("What color is the sky?")
	sess.AddAssistantMessage("The sky is green.", nil)
	expanded, correction := ContextualizeShortFollowup(sess, "no that's wrong")
	if !strings.Contains(expanded, "disputed") {
		t.Errorf("expected dispute context, got %q", expanded)
	}
	if !strings.Contains(expanded, "sky is green") {
		t.Errorf("expected previous reply excerpt, got %q", expanded)
	}
	if !correction {
		t.Error("contradiction should be correction turn")
	}
}

func TestContextualizeShortFollowup_NoHistory(t *testing.T) {
	sess := newTestSession("s-empty")
	expanded, correction := ContextualizeShortFollowup(sess, "sure")
	if expanded != "sure" {
		t.Errorf("expected passthrough without history, got %q", expanded)
	}
	if !correction {
		t.Error("sarcasm should still be correction even without history")
	}
}

func newTestSession(id string) *Session {
	return NewSession(id, "test-agent", "Test Agent")
}
