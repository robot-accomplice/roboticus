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

func TestContextualizeShortFollowup_ReferentialExecutionTask(t *testing.T) {
	sess := newTestSession("s-reference")
	sess.AddUserMessage("Can you read my vault and give me an executive summary?")
	sess.AddAssistantMessage("The observed vault contains a `Duncan/` directory inside `/Users/jmachen/Desktop/My Vault`, which the operator identified as the shared-knowledge section.", nil)

	expanded, correction := ContextualizeShortFollowup(sess, "I want you to examine it")

	if correction {
		t.Fatal("referential execution follow-up should not be treated as correction")
	}
	for _, want := range []string{
		"referential execution request",
		"Duncan/",
		"/Users/jmachen/Desktop/My Vault",
		"child of an allowed path",
		"attempt the relevant tool",
	} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded follow-up missing %q: %q", want, expanded)
		}
	}
}

func TestContextualizeShortFollowup_PendingActionConfirmation(t *testing.T) {
	sess := newTestSession("s-pending-action")
	sess.AddUserMessage("Use the ghola tool to get the latest scores from Metacritic.")
	sess.AddAssistantMessage("I need to proceed by examining the retrieved HTML content and extracting the relevant game scores. Please confirm if you would like me to proceed with this method.", nil)

	expanded, correction := ContextualizeShortFollowup(sess, "Please do.")

	if correction {
		t.Fatal("pending action confirmation should not be treated as correction")
	}
	for _, want := range []string{
		"PENDING ACTION CONFIRMED",
		"Instruction: execute the confirmed next action now",
		"Background task",
		"Use the ghola tool",
		"Confirmed next action",
		"retrieved HTML content",
		"extracting the relevant game scores",
	} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded confirmation missing %q: %q", want, expanded)
		}
	}
}

func TestContextualizeShortFollowup_ExecutesStructuredNextStepsWithoutMagicPhrase(t *testing.T) {
	sess := newTestSession("s-structured-next-steps")
	sess.AddUserMessage("Please review the code and architecture docs.")
	sess.AddAssistantMessage(`I found the architecture documentation.

Next Steps:
1. Review ARCHITECTURE.md and architecture_rules.md.
2. Compare those rules directly with the code.
3. Summarize alignment and gaps.`, nil)

	expanded, correction := ContextualizeShortFollowup(sess, "Great, execute the next steps identified in your last message.")

	if correction {
		t.Fatal("structured next-step continuation should not be treated as correction")
	}
	for _, want := range []string{
		"PENDING ACTION CONFIRMED",
		"Instruction: execute the confirmed next action now",
		"Background task",
		"Review ARCHITECTURE.md",
		"Compare those rules directly with the code",
		"User confirmation: Great, execute the next steps identified in your last message.",
	} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded structured continuation missing %q: %q", want, expanded)
		}
	}
}

func TestContextualizeShortFollowup_StateBasedContinuationWithoutMagicPhrase(t *testing.T) {
	sess := newTestSession("s-state-continuation")
	sess.AddUserMessage("Get the Metacritic score for Vampire Crawlers.")
	sess.AddAssistantMessage("The next step is targeted parsing of score elements and structured data from the observed page.", nil)

	expanded, correction := ContextualizeShortFollowup(sess, "Fine.")

	if correction {
		t.Fatal("state-based continuation should not be treated as correction")
	}
	for _, want := range []string{
		"PENDING ACTION CONFIRMED",
		"targeted parsing",
		"observed page",
		"User confirmation: Fine.",
	} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded state continuation missing %q: %q", want, expanded)
		}
	}
}

func TestContextualizeShortFollowup_StateBasedContinuationAllowsNegativeStop(t *testing.T) {
	sess := newTestSession("s-state-negative")
	sess.AddUserMessage("Get the Metacritic score for Vampire Crawlers.")
	sess.AddAssistantMessage("The next step is targeted parsing of score elements and structured data from the observed page.", nil)

	expanded, correction := ContextualizeShortFollowup(sess, "No thanks.")

	if correction {
		t.Fatal("negative continuation should not be treated as correction")
	}
	if expanded != "No thanks." {
		t.Fatalf("negative continuation should pass through, got %q", expanded)
	}
}

func TestContextualizeShortFollowup_StateBasedContinuationDoesNotSwallowExplicitContextCorrection(t *testing.T) {
	sess := newTestSession("s-state-context-correction")
	sess.AddUserMessage("Focus on docs/architecture-gap-report.md and docs/architecture-rules-diagrams.md first")
	sess.AddAssistantMessage("I will focus on docs/architecture-gap-report.md and docs/architecture-rules-diagrams.md, then compare those rules to code paths. Should I proceed with that comparison?", nil)

	content := "The architecture documents are within the code repository you already reviewed"
	expanded, correction := ContextualizeShortFollowup(sess, content)

	if correction {
		t.Fatal("context correction should not be treated as correction turn")
	}
	if expanded != content {
		t.Fatalf("explicit context correction should pass through, got %q", expanded)
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
