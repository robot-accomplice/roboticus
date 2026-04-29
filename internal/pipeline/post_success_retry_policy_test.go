package pipeline

import "testing"

func TestDecideGuardRetryAfterProgress_SuppressesNarrativeOnlyGuard(t *testing.T) {
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "obsidian_write", Output: "wrote 32 bytes"}},
	}
	result := ApplyResult{
		Content:        "Created the note.",
		RetryRequested: true,
		RetryReason:    "response repeats previous assistant message",
		Violations:     []string{"non_repetition_v2"},
	}

	disposition := decideGuardRetryAfterProgress(result, ctx)
	if disposition.Allow {
		t.Fatal("Allow = true, want false")
	}
	if disposition.Reason == "" {
		t.Fatal("Reason should explain the suppression")
	}
}

func TestDecideGuardRetryAfterProgress_AllowsExecutionCriticalGuard(t *testing.T) {
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "obsidian_write", Output: "wrote 32 bytes"}},
	}
	result := ApplyResult{
		Content:        "Created the note.",
		RetryRequested: true,
		RetryReason:    "claimed execution without evidence",
		Violations:     []string{"execution_truth"},
	}

	disposition := decideGuardRetryAfterProgress(result, ctx)
	if !disposition.Allow {
		t.Fatal("Allow = false, want true")
	}
}

func TestGuardExhaustionMustFailClosedForActionAndShapeContracts(t *testing.T) {
	for _, violation := range []string{"task_deferral", "output_contract"} {
		if !guardExhaustionMustFailClosed([]string{violation}) {
			t.Fatalf("guardExhaustionMustFailClosed(%q) = false, want true", violation)
		}
	}
}

func TestDecideVerifierRetryAfterProgress_SuppressesNarrativeOnlyIssues(t *testing.T) {
	progress := ExecutionProgress{SuccessfulToolResults: 1, SuccessfulArtifactWrites: 1}
	ctx := VerificationContext{PlannedAction: "execute_directly"}
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{Code: "unsupported_certainty"}},
	}

	disposition := decideVerifierRetryAfterProgress(result, ctx, progress)
	if disposition.Allow {
		t.Fatal("Allow = true, want false")
	}
	if disposition.Reason == "" {
		t.Fatal("Reason should explain the suppression")
	}
}

func TestDecideVerifierRetryAfterProgress_SuppressesNarrativeOnlyIssuesAfterProgressEvenWhenPlannedActionIsNotExecuteDirectly(t *testing.T) {
	progress := ExecutionProgress{SuccessfulToolResults: 1}
	ctx := VerificationContext{PlannedAction: "consult_memory"}
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{Code: "unsupported_certainty"}},
	}

	disposition := decideVerifierRetryAfterProgress(result, ctx, progress)
	if disposition.Allow {
		t.Fatal("Allow = true, want false")
	}
	if disposition.Reason == "" {
		t.Fatal("Reason should explain the suppression")
	}
}

func TestDecideVerifierRetryAfterProgress_AllowsExecutionCriticalIssues(t *testing.T) {
	progress := ExecutionProgress{SuccessfulToolResults: 1, SuccessfulArtifactWrites: 1}
	ctx := VerificationContext{PlannedAction: "execute_directly"}
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{Code: "unsupported_subgoal"}},
	}

	disposition := decideVerifierRetryAfterProgress(result, ctx, progress)
	if !disposition.Allow {
		t.Fatal("Allow = false, want true")
	}
}

func TestDecideVerifierRetryAfterProgress_AllowsArtifactContentMismatchAfterProgress(t *testing.T) {
	progress := ExecutionProgress{SuccessfulToolResults: 1, SuccessfulArtifactWrites: 1}
	ctx := VerificationContext{PlannedAction: "execute_directly"}
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{Code: "artifact_content_mismatch"}},
	}

	disposition := decideVerifierRetryAfterProgress(result, ctx, progress)
	if !disposition.Allow {
		t.Fatal("Allow = false, want true")
	}
}

func TestDecideVerifierRetryAfterProgress_AllowsSourceArtifactUnreadAfterProgress(t *testing.T) {
	progress := ExecutionProgress{SuccessfulToolResults: 1, SuccessfulArtifactWrites: 1}
	ctx := VerificationContext{PlannedAction: "execute_directly", SourceArtifacts: []string{"tmp/input.txt"}}
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{Code: "source_artifact_unread"}},
	}

	disposition := decideVerifierRetryAfterProgress(result, ctx, progress)
	if !disposition.Allow {
		t.Fatal("Allow = false, want true")
	}
}

func TestDecideVerifierRetryAfterProgress_AllowsArtifactSetOverclaimAfterProgress(t *testing.T) {
	progress := ExecutionProgress{SuccessfulToolResults: 1, SuccessfulArtifactWrites: 1}
	ctx := VerificationContext{PlannedAction: "execute_directly"}
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{Code: "artifact_set_overclaim"}},
	}

	disposition := decideVerifierRetryAfterProgress(result, ctx, progress)
	if !disposition.Allow {
		t.Fatal("Allow = false, want true")
	}
}

func TestDecideVerifierRetryAfterProgress_AllowsUnexpectedArtifactWriteAfterProgress(t *testing.T) {
	progress := ExecutionProgress{SuccessfulToolResults: 1, SuccessfulArtifactWrites: 1}
	ctx := VerificationContext{PlannedAction: "execute_directly"}
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{Code: "artifact_unexpected_write"}},
	}

	disposition := decideVerifierRetryAfterProgress(result, ctx, progress)
	if !disposition.Allow {
		t.Fatal("Allow = false, want true")
	}
}
