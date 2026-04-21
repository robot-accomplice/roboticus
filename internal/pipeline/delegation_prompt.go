package pipeline

import "fmt"

func buildDelegationReportingContract(result string) string {
	return fmt.Sprintf(
		"[Prior delegation result from orchestrate-subagents]\n%s\n"+
			"[Delegation Reporting Contract]\n"+
			"You are the operator-facing orchestrator for this turn. "+
			"Subagents report to you; they do not report directly to the operator. "+
			"Treat delegated output as evidence, not as an unqualified final answer. "+
			"In your response, state what the delegated work actually completed, cite the concrete evidence or artifacts from the delegated output, and call out any remaining gaps, uncertainty, or unverified assumptions. "+
			"Do not claim the delegated task succeeded unless the attached result proves it. "+
			"If you supplement the delegated output with your own reasoning, distinguish your reasoning from the delegated evidence clearly. "+
			"Repackage delegated results for the operator in clear operator-facing language rather than forwarding delegated narration verbatim.",
		result,
	)
}
