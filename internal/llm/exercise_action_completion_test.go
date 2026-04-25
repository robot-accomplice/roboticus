package llm

import "testing"

func TestScoreActionCompletion_BlockedHypotheticalDelegationDoesNotPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Create a scheduled task that runs a health check every hour and stores the results.",
		IntentDelegation,
		ComplexityComplex,
	)

	score := ScoreExerciseResponse(prompt, "I can't complete the remaining steps with the current tool surface. If those tools become available, here's exactly what I'd do: write a script, chmod it, and install a cron entry.")

	if score >= DefaultExercisePassQualityFloor {
		t.Fatalf("blocked hypothetical delegation score = %.2f, want below %.2f", score, DefaultExercisePassQualityFloor)
	}
}

func TestScoreActionCompletion_CopyPasteWorkaroundDoesNotPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Create a scheduled task that runs a health check every hour and stores the results.",
		IntentDelegation,
		ComplexityComplex,
	)

	score := ScoreExerciseResponse(prompt, "My current tool surface doesn't include file-write or shell-execution tools, so I can't directly install this for you. But here is the exact, copy-paste-ready setup to deploy immediately.")

	if score >= DefaultExercisePassQualityFloor {
		t.Fatalf("copy-paste workaround score = %.2f, want below %.2f", score, DefaultExercisePassQualityFloor)
	}
}

func TestScoreActionCompletion_DeclaredTaskFailureDoesNotPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Create a scheduled task that runs a health check every hour and stores the results.",
		IntentDelegation,
		ComplexityComplex,
	)

	score := ScoreExerciseResponse(prompt, "The task failed due to a database schema mismatch, causing the task to fail immediately on the first storage attempt. The corrected implementation below would create a scheduled task and store future results once fixed.")

	if score >= DefaultExercisePassQualityFloor {
		t.Fatalf("declared task failure score = %.2f, want below %.2f", score, DefaultExercisePassQualityFloor)
	}
}

func TestScoreActionCompletion_PolicyDeniedBlueprintDoesNotPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Refactor the configuration parser to support hot-reload with validation, rollback on failure, and emit structured change events.",
		IntentExecution,
		ComplexityExpert,
	)

	score := ScoreExerciseResponse(prompt, "Tool execution failed with Policy denied: path traversal detected, preventing access to the existing codebase. Because filesystem discovery is blocked and tools are disabled, I cannot inspect the current parser implementation. Instead, I am finalizing with a complete refactoring blueprint.")

	if score >= DefaultExercisePassQualityFloor {
		t.Fatalf("policy-denied blueprint score = %.2f, want below %.2f", score, DefaultExercisePassQualityFloor)
	}
}

func TestScoreActionCompletion_ConcreteDelegationCanPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Create a scheduled task that runs a health check every hour and stores the results.",
		IntentDelegation,
		ComplexityComplex,
	)

	score := ScoreExerciseResponse(prompt, "Created the scheduled health-check task, wrote the script, and stored the hourly results under health_logs.")

	if score < DefaultExercisePassQualityFloor {
		t.Fatalf("concrete delegation score = %.2f, want at least %.2f", score, DefaultExercisePassQualityFloor)
	}
}
