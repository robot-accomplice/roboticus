package memory

import (
	"strings"
	"testing"
	"time"
)

func TestAnalyzeEpisode_CarriesEvidenceAndQuality(t *testing.T) {
	input := EpisodeInput{
		UserContent:     "Explain the root cause and propose a remediation plan.",
		AssistantAnswer: "The root cause was a stale cache. Remediation: invalidate on deploy.",
		ToolEvents: []ToolEvent{
			{ToolName: "search", Success: true},
			{ToolName: "read", Success: true},
		},
		EvidenceItems: []string{
			"Billing cache TTL was set to 24h in release v2",
			"Cache invalidation hook removed during migration",
		},
		VerifierPassed: true,
		Duration:       2 * time.Second,
	}
	summary := AnalyzeEpisode(input)
	if summary == nil {
		t.Fatal("expected summary, got nil")
	}
	if len(summary.EvidenceRefs) != 2 {
		t.Fatalf("expected 2 evidence refs, got %+v", summary.EvidenceRefs)
	}
	if !summary.VerifierPassed {
		t.Fatal("expected VerifierPassed=true")
	}
	if summary.ResultQuality < 0.85 {
		t.Fatalf("expected high result quality (all tools passed, verifier passed, evidence present), got %f", summary.ResultQuality)
	}
}

func TestAnalyzeEpisode_DetectsFixPattern(t *testing.T) {
	input := EpisodeInput{
		UserContent: "Deploy the service",
		ToolEvents: []ToolEvent{
			{ToolName: "shell", Success: false},
			{ToolName: "shell", Success: true},
		},
	}
	summary := AnalyzeEpisode(input)
	if len(summary.FixPatterns) != 1 {
		t.Fatalf("expected one fix pattern, got %+v", summary.FixPatterns)
	}
	if !strings.Contains(summary.FixPatterns[0], "fail→success on retry") {
		t.Fatalf("unexpected fix pattern content: %q", summary.FixPatterns[0])
	}
}

func TestAnalyzeEpisode_ExtractsFailedHypotheses(t *testing.T) {
	input := EpisodeInput{
		UserContent:     "Who owns the ledger?",
		AssistantAnswer: "The ledger is owned by the accounting team. Correction: I was mistaken; it is actually owned by the platform team.",
	}
	summary := AnalyzeEpisode(input)
	if len(summary.FailedHypotheses) == 0 {
		t.Fatalf("expected at least one failed hypothesis, got %+v", summary.FailedHypotheses)
	}
	joined := strings.Join(summary.FailedHypotheses, " | ")
	if !strings.Contains(strings.ToLower(joined), "correction") && !strings.Contains(strings.ToLower(joined), "mistaken") {
		t.Fatalf("expected marker in failed hypothesis, got %q", joined)
	}
}

func TestAnalyzeEpisode_CapturesErrorMessages(t *testing.T) {
	input := EpisodeInput{
		UserContent: "Fix the build",
		ToolEvents: []ToolEvent{
			{ToolName: "shell", Success: false},
		},
		ErrorMessages: []string{
			"Error: target not found",
			"Error: target not found", // duplicate — must dedupe
		},
	}
	summary := AnalyzeEpisode(input)
	if len(summary.ErrorsSeen) != 1 {
		t.Fatalf("expected deduplicated error list, got %+v", summary.ErrorsSeen)
	}
}

func TestAnalyzeEpisode_CapturesOutcomePatterns(t *testing.T) {
	input := EpisodeInput{
		UserContent:     "Update the deployment note",
		AssistantAnswer: "Correction: the first path was wrong.",
		ToolEvents: []ToolEvent{
			{ToolName: "shell", Success: false},
			{ToolName: "shell", Success: true},
		},
		ErrorMessages: []string{
			"permission denied",
			"permission denied",
		},
		TurnStatus:     "degraded",
		VerifierPassed: false,
	}
	summary := AnalyzeEpisode(input)
	if summary == nil {
		t.Fatal("expected summary, got nil")
	}
	if len(summary.OutcomePatterns) == 0 {
		t.Fatal("expected reusable outcome patterns")
	}
	var sawFix, sawError bool
	for _, pat := range summary.OutcomePatterns {
		if pat.Kind == "fix_pattern" && pat.Outcome == "success" {
			sawFix = true
		}
		if pat.Kind == "error_mode" && pat.Outcome == "partial" && strings.Contains(strings.ToLower(pat.Value), "permission denied") {
			sawError = true
		}
	}
	if !sawFix || !sawError {
		t.Fatalf("expected both success and partial outcome patterns, got %+v", summary.OutcomePatterns)
	}
}

func TestAnalyzeEpisode_DegradedVerifierFailedTurnIsPartialNotSuccess(t *testing.T) {
	input := EpisodeInput{
		UserContent: "Create the rollout files",
		ToolEvents: []ToolEvent{
			{ToolName: "write_file", Success: true},
			{ToolName: "write_file", Success: true},
		},
		EvidenceItems: []string{
			"[tool_artifact, canonical] workspace_file tmp/procedural-canary/rollout-config.json",
		},
		TurnStatus:     "degraded",
		VerifierPassed: false,
	}
	summary := AnalyzeEpisode(input)
	if summary == nil {
		t.Fatal("expected summary, got nil")
	}
	if summary.Outcome != "partial" {
		t.Fatalf("expected partial outcome, got %q", summary.Outcome)
	}
	for _, pat := range summary.OutcomePatterns {
		if pat.Kind == "learning" && pat.Outcome == "success" {
			t.Fatalf("unexpected success learning pattern on degraded turn: %+v", summary.OutcomePatterns)
		}
	}
}

func TestAnalyzeEpisode_LowQualityWhenVerifierFailsWithNoEvidence(t *testing.T) {
	input := EpisodeInput{
		UserContent:    "Is this compliant?",
		ToolEvents:     []ToolEvent{{ToolName: "search", Success: false}},
		VerifierPassed: false,
	}
	summary := AnalyzeEpisode(input)
	if summary.ResultQuality > 0.5 {
		t.Fatalf("expected low quality when all tools failed and verifier failed with no evidence, got %f", summary.ResultQuality)
	}
}

func TestFormatForStorage_IncludesEnrichedFields(t *testing.T) {
	summary := &EpisodeSummary{
		Goal:                "deploy",
		Actions:             []string{"shell"},
		Outcome:             "success",
		FixPatterns:         []string{"shell: fail→success on retry"},
		EvidenceRefs:        []string{"cache TTL 24h"},
		FailedHypotheses:    []string{"I was mistaken about the owner"},
		ErrorsSeen:          []string{"Error: target not found"},
		ModelUsed:           "ollama/llama3",
		ReactTurns:          2,
		GuardViolations:     []string{"rewrite_tracking"},
		GuardRetried:        true,
		VerifiedRecorded:    1,
		QuestionsOpened:     2,
		QuestionsResolved:   1,
		AssumptionsRecorded: 3,
		ResultQuality:       0.87,
		Duration:            2 * time.Second,
	}
	out := summary.FormatForStorage()
	for _, needle := range []string{
		"FixPatterns", "EvidenceRefs", "FailedHypotheses", "Errors", "Quality: high",
		"Model: ollama/llama3", "ReactTurns: 2", "GuardViolations: rewrite_tracking", "GuardRetried: yes",
		"ExecutiveVerified: 1", "ExecutiveQuestionsOpened: 2", "ExecutiveQuestionsResolved: 1", "ExecutiveAssumptions: 3",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected %q in format output, got %q", needle, out)
		}
	}
}

func TestAnalyzeEpisode_UsesStructuredInferenceArtifacts(t *testing.T) {
	input := EpisodeInput{
		UserContent:         "Deploy safely",
		ToolEvents:          []ToolEvent{{ToolName: "shell", Success: true}},
		VerifierPassed:      true,
		ModelUsed:           "openai/gpt-5.4",
		ReactTurns:          3,
		GuardViolations:     []string{"policy_risk", "policy_risk"},
		GuardRetried:        true,
		VerifiedRecorded:    1,
		QuestionsOpened:     1,
		QuestionsResolved:   1,
		AssumptionsRecorded: 2,
	}
	summary := AnalyzeEpisode(input)
	if summary.ModelUsed != "openai/gpt-5.4" {
		t.Fatalf("ModelUsed = %q", summary.ModelUsed)
	}
	if summary.ReactTurns != 3 {
		t.Fatalf("ReactTurns = %d", summary.ReactTurns)
	}
	if len(summary.GuardViolations) != 1 || summary.GuardViolations[0] != "policy_risk" {
		t.Fatalf("GuardViolations = %+v", summary.GuardViolations)
	}
	if !summary.GuardRetried {
		t.Fatal("expected GuardRetried=true")
	}
	if summary.VerifiedRecorded != 1 || summary.QuestionsOpened != 1 || summary.QuestionsResolved != 1 || summary.AssumptionsRecorded != 2 {
		t.Fatalf("unexpected executive growth counts: %+v", summary)
	}
	if !strings.Contains(strings.Join(summary.Learnings, " | "), "guard-triggered revision") {
		t.Fatalf("expected structured guard learning, got %+v", summary.Learnings)
	}
}

func TestEpisodeSummary_JSONRoundTrip(t *testing.T) {
	original := &EpisodeSummary{
		Goal:                "deploy",
		Outcome:             "success",
		Learnings:           []string{"guard-triggered revision required before final answer"},
		OutcomePatterns:     []EpisodeOutcomePattern{{Outcome: "success", Kind: "learning", Value: "guard-triggered revision required before final answer"}},
		ModelUsed:           "openai/gpt-5.4",
		ReactTurns:          3,
		VerifiedRecorded:    1,
		QuestionsOpened:     2,
		QuestionsResolved:   1,
		AssumptionsRecorded: 4,
		ResultQuality:       0.9,
	}
	decoded, err := ParseEpisodeSummaryJSON(original.JSON())
	if err != nil {
		t.Fatalf("ParseEpisodeSummaryJSON: %v", err)
	}
	if decoded == nil {
		t.Fatal("expected decoded summary")
	}
	if decoded.ModelUsed != original.ModelUsed || decoded.ReactTurns != original.ReactTurns {
		t.Fatalf("decoded inference metadata = %+v", decoded)
	}
	if decoded.VerifiedRecorded != 1 || decoded.QuestionsOpened != 2 || decoded.QuestionsResolved != 1 || decoded.AssumptionsRecorded != 4 {
		t.Fatalf("decoded executive counts = %+v", decoded)
	}
	if len(decoded.Learnings) != 1 || decoded.Learnings[0] != original.Learnings[0] {
		t.Fatalf("decoded learnings = %+v", decoded.Learnings)
	}
	if len(decoded.OutcomePatterns) != 1 || decoded.OutcomePatterns[0] != original.OutcomePatterns[0] {
		t.Fatalf("decoded outcome patterns = %+v", decoded.OutcomePatterns)
	}
}
