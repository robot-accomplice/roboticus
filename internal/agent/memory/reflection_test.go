package memory

import (
	"strings"
	"testing"
	"time"
)

func TestReflect_SimpleConversation(t *testing.T) {
	summary := Reflect("Hello, how are you?", nil, 2*time.Second)

	if summary == nil {
		t.Fatal("expected non-nil summary for conversation")
	}
	if summary.Outcome != "conversation" {
		t.Errorf("expected outcome=conversation, got %s", summary.Outcome)
	}
	if summary.Goal == "" {
		t.Error("goal should be extracted from user content")
	}
}

func TestReflect_SuccessfulToolRun(t *testing.T) {
	events := []ToolEvent{
		{ToolName: "search", Success: true, Duration: 100 * time.Millisecond},
		{ToolName: "read_file", Success: true, Duration: 50 * time.Millisecond},
	}
	summary := Reflect("Find the deployment config.", events, 5*time.Second)

	if summary.Outcome != "success" {
		t.Errorf("expected outcome=success, got %s", summary.Outcome)
	}
	if len(summary.Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(summary.Actions))
	}
}

func TestReflect_PartialFailure(t *testing.T) {
	events := []ToolEvent{
		{ToolName: "deploy", Success: false},
		{ToolName: "deploy", Success: true},
	}
	summary := Reflect("Deploy to production.", events, 10*time.Second)

	if summary.Outcome != "partial" {
		t.Errorf("expected outcome=partial, got %s", summary.Outcome)
	}
}

func TestReflect_AllFailed(t *testing.T) {
	events := []ToolEvent{
		{ToolName: "deploy", Success: false},
		{ToolName: "rollback", Success: false},
	}
	summary := Reflect("Fix the outage.", events, 30*time.Second)

	if summary.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", summary.Outcome)
	}
	// Should detect all-fail pattern.
	hasAllFail := false
	for _, l := range summary.Learnings {
		if strings.Contains(l, "all tool calls failed") {
			hasAllFail = true
		}
	}
	if !hasAllFail {
		t.Error("expected 'all tool calls failed' learning")
	}
}

func TestReflect_RetryPattern(t *testing.T) {
	events := []ToolEvent{
		{ToolName: "api_call", Success: false},
		{ToolName: "api_call", Success: true},
	}
	summary := Reflect("Call the API.", events, 5*time.Second)

	hasRetry := false
	for _, l := range summary.Learnings {
		if strings.Contains(l, "retry pattern") {
			hasRetry = true
		}
	}
	if !hasRetry {
		t.Error("expected retry pattern learning for repeated tool with failure")
	}
}

func TestReflect_EmptyInput(t *testing.T) {
	if summary := Reflect("", nil, 0); summary != nil {
		t.Error("expected nil for empty input")
	}
}

func TestReflect_GoalExtraction(t *testing.T) {
	summary := Reflect("Deploy the new version to staging. Then verify the health checks.", nil, 0)

	if summary.Goal != "Deploy the new version to staging." {
		t.Errorf("goal should be first sentence, got %q", summary.Goal)
	}
}

func TestEpisodeSummary_FormatForStorage(t *testing.T) {
	es := &EpisodeSummary{
		Goal:      "deploy to production",
		Actions:   []string{"build", "test", "deploy"},
		Outcome:   "success",
		Learnings: []string{"retry pattern on deploy"},
		Duration:  45 * time.Second,
	}

	formatted := es.FormatForStorage()

	if !strings.Contains(formatted, "deploy to production") {
		t.Error("formatted should contain goal")
	}
	if !strings.Contains(formatted, "build → test → deploy") {
		t.Error("formatted should contain action chain")
	}
	if !strings.Contains(formatted, "success") {
		t.Error("formatted should contain outcome")
	}
	if !strings.Contains(formatted, "45s") {
		t.Error("formatted should contain duration")
	}
}
