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
		Goal:             "deploy",
		Actions:          []string{"shell"},
		Outcome:          "success",
		FixPatterns:      []string{"shell: fail→success on retry"},
		EvidenceRefs:     []string{"cache TTL 24h"},
		FailedHypotheses: []string{"I was mistaken about the owner"},
		ErrorsSeen:       []string{"Error: target not found"},
		ResultQuality:    0.87,
		Duration:         2 * time.Second,
	}
	out := summary.FormatForStorage()
	for _, needle := range []string{
		"FixPatterns", "EvidenceRefs", "FailedHypotheses", "Errors", "Quality: high",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected %q in format output, got %q", needle, out)
		}
	}
}
