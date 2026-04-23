package pipeline

import (
	"fmt"
	"strings"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/llm"
)

type verifierRetryPlan struct {
	Message          string
	ToolDefs         []llm.ToolDef
	CorrectionReason string
}

func buildVerifierRetryPlan(result VerificationResult, ctx VerificationContext, selected []llm.ToolDef) verifierRetryPlan {
	plan := verifierRetryPlan{
		Message:  result.RetryMessage(),
		ToolDefs: selected,
	}

	if !result.HasIssue("source_artifact_unread") || len(ctx.SourceArtifacts) == 0 {
		return plan
	}

	plan.Message = buildSourceArtifactRetryMessage(result, ctx)

	filtered := prioritizeSourceReadRetryTools(selected)
	if len(filtered) != len(selected) {
		plan.ToolDefs = filtered
		plan.CorrectionReason = "source_artifact_unread requires authoritative source-read correction before further memory hydration"
	}

	return plan
}

func buildSourceArtifactRetryMessage(result VerificationResult, ctx VerificationContext) string {
	base := result.RetryMessage()
	if len(ctx.SourceArtifacts) == 0 {
		return base
	}

	artifactList := strings.Join(ctx.SourceArtifacts, ", ")
	return base + " Before revising the answer, gather authoritative source proof by reading the prompt-named source artifact(s) with the file-read tool: " +
		artifactList + ". Do not treat memory recall or memory search as a substitute for reading those source artifacts."
}

func prioritizeSourceReadRetryTools(selected []llm.ToolDef) []llm.ToolDef {
	if len(selected) == 0 {
		return selected
	}

	filtered := make([]llm.ToolDef, 0, len(selected))
	for _, def := range selected {
		switch agenttools.OperationClassForName(def.Function.Name) {
		case agenttools.OperationArtifactRead,
			agenttools.OperationArtifactWrite,
			agenttools.OperationRuntimeContextRead,
			agenttools.OperationWorkspaceInspect:
			filtered = append(filtered, def)
		}
	}

	if !hasOperationClass(filtered, agenttools.OperationArtifactRead) {
		return selected
	}

	return filtered
}

func hasOperationClass(defs []llm.ToolDef, class agenttools.OperationClass) bool {
	for _, def := range defs {
		if agenttools.OperationClassForName(def.Function.Name) == class {
			return true
		}
	}
	return false
}

func formatVerifierRetryCorrectionSummary(plan verifierRetryPlan) string {
	if plan.CorrectionReason == "" {
		return ""
	}
	names := make([]string, 0, len(plan.ToolDefs))
	for _, def := range plan.ToolDefs {
		names = append(names, def.Function.Name)
	}
	return fmt.Sprintf("%s; retry tools now constrained to: %s", plan.CorrectionReason, strings.Join(names, ", "))
}
