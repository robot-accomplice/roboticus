package pipeline

import (
	"strings"

	agenttools "roboticus/internal/agent/tools"
)

// ExecutionProgress captures whether the current turn has already made
// substantive tool-backed progress. Guard and verifier retry policy use this
// to distinguish "the answer is a little ugly" from "the claimed work is not
// trustworthy yet".
type ExecutionProgress struct {
	SuccessfulToolResults    int
	SuccessfulArtifactWrites int
}

func executionProgressFromGuardContext(ctx *GuardContext) ExecutionProgress {
	if ctx == nil {
		return ExecutionProgress{}
	}
	var progress ExecutionProgress
	for _, tr := range ctx.ToolResults {
		if toolResultSignalsFailure(tr) {
			continue
		}
		progress.SuccessfulToolResults++
		if agenttools.WritesPersistentArtifact(tr.ToolName) {
			progress.SuccessfulArtifactWrites++
		}
	}
	return progress
}

func (p ExecutionProgress) HasSubstantiveExecutionProgress() bool {
	return p.SuccessfulToolResults > 0
}

type RetryDisposition struct {
	Allow  bool
	Reason string
}

func decideGuardRetryAfterProgress(result ApplyResult, ctx *GuardContext) RetryDisposition {
	if !result.RetryRequested {
		return RetryDisposition{Allow: false}
	}
	progress := executionProgressFromGuardContext(ctx)
	if !progress.HasSubstantiveExecutionProgress() {
		return RetryDisposition{Allow: true}
	}

	for _, violation := range result.Violations {
		if guardRetryRemainsBlockingAfterProgress(violation) {
			return RetryDisposition{Allow: true}
		}
	}

	return RetryDisposition{
		Allow:  false,
		Reason: "turn already made substantive execution progress and the guard finding is narrative-only",
	}
}

func guardRetryRemainsBlockingAfterProgress(name string) bool {
	switch name {
	case "empty_response",
		"subagent_claim",
		"execution_truth",
		"action_verification",
		"task_deferral",
		"clarification_deflection",
		"output_contract",
		"model_identity_truth",
		"current_events_truth",
		"declared_action",
		"placeholder_content",
		"execution_block",
		"delegation_metadata",
		"filesystem_denial",
		"config_protection",
		"financial_action_truth":
		return true
	default:
		return false
	}
}

func guardExhaustionMustFailClosed(violations []string) bool {
	for _, violation := range violations {
		switch strings.TrimSpace(strings.SplitN(violation, ":", 2)[0]) {
		case "execution_truth",
			"filesystem_denial",
			"action_verification",
			"config_protection",
			"financial_action_truth":
			return true
		}
	}
	return false
}

func decideVerifierRetryAfterProgress(result VerificationResult, ctx VerificationContext, progress ExecutionProgress) RetryDisposition {
	if result.Passed {
		return RetryDisposition{Allow: false}
	}
	if !progress.HasSubstantiveExecutionProgress() {
		return RetryDisposition{Allow: true}
	}

	for _, issue := range result.Issues {
		if verifierRetryRemainsBlockingAfterProgress(issue.Code) {
			return RetryDisposition{Allow: true}
		}
	}

	return RetryDisposition{
		Allow:  false,
		Reason: "turn already completed a tool-backed action and the remaining verifier findings are narrative-quality concerns",
	}
}

func verifierRetryRemainsBlockingAfterProgress(code string) bool {
	switch code {
	case "unsupported_subgoal",
		"missing_action_plan",
		"abandoned_unresolved_question",
		"stopping_criteria_unmet",
		"source_artifact_unread",
		"artifact_content_mismatch",
		"artifact_set_overclaim",
		"artifact_unexpected_write":
		return true
	default:
		return false
	}
}
