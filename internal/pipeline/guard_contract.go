package pipeline

func guardContractEventDetails(events []GuardContractEvent, outcome string) []map[string]any {
	if len(events) == 0 {
		return nil
	}
	details := make([]map[string]any, 0, len(events))
	for _, event := range events {
		if outcome != "" {
			event.RecoveryOutcome = outcome
		}
		details = append(details, map[string]any{
			"contract_id":        event.ContractID,
			"contract_group":     event.ContractGroup,
			"phase":              event.Phase,
			"severity":           event.Severity,
			"precondition_state": event.PreconditionState,
			"violation_state":    event.ViolationState,
			"recovery_action":    event.RecoveryAction,
			"recovery_attempt":   event.RecoveryAttempt,
			"recovery_window":    event.RecoveryWindow,
			"recovery_outcome":   event.RecoveryOutcome,
			"confidence_effect":  event.ConfidenceEffect,
		})
	}
	return details
}

func verifierContractEventDetails(result VerificationResult, disposition RetryDisposition) []map[string]any {
	if result.Passed || len(result.Issues) == 0 {
		return nil
	}
	events := make([]map[string]any, 0, len(result.Issues))
	for _, issue := range result.Issues {
		severity := "soft"
		if verifierRetryRemainsBlockingAfterProgress(issue.Code) {
			severity = "hard"
		}
		recoveryAction := "record"
		recoveryOutcome := "recorded"
		if disposition.Allow {
			recoveryAction = "retry"
			recoveryOutcome = "scheduled"
		}
		events = append(events, map[string]any{
			"contract_id":        "verifier." + issue.Code,
			"contract_group":     "verifier",
			"phase":              "reflect",
			"severity":           severity,
			"precondition_state": "assistant output must satisfy verifier policy",
			"violation_state":    issue.Detail,
			"recovery_action":    recoveryAction,
			"recovery_attempt":   0,
			"recovery_window":    1,
			"recovery_outcome":   recoveryOutcome,
			"confidence_effect":  "-1",
		})
	}
	return events
}

func guardContractEventStatus(events []GuardContractEvent) string {
	for _, event := range events {
		if event.Severity == "hard" {
			return "error"
		}
	}
	return "warning"
}
