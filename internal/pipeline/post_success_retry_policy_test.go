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
