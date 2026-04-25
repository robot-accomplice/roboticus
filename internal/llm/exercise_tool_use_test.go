package llm

import "testing"

func TestScoreToolUse_RequiredToolPromissoryIntentDoesNotPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Look up how many sessions were created today by querying the database.",
		IntentToolUse,
		ComplexityModerate,
	)

	score := ScoreExerciseResponse(prompt, "I'll help you look up how many sessions were created today by querying the database.")

	if score >= DefaultExercisePassQualityFloor {
		t.Fatalf("promissory tool-use score = %.2f, want below %.2f", score, DefaultExercisePassQualityFloor)
	}
}

func TestScoreToolUse_RequiredToolObservedResultCanPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Look up how many sessions were created today by querying the database.",
		IntentToolUse,
		ComplexityModerate,
	)

	score := ScoreExerciseResponse(prompt, "The database query returned 0 sessions created today.")

	if score < DefaultExercisePassQualityFloor {
		t.Fatalf("observed tool-use score = %.2f, want at least %.2f", score, DefaultExercisePassQualityFloor)
	}
}

func TestScoreToolUse_RequiredToolBlockedEvidenceCanPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Show me the contents of the README file.",
		IntentToolUse,
		ComplexitySimple,
	)

	score := ScoreExerciseResponse(prompt, "The read_file result shows no README file was found in the workspace.")

	if score < DefaultExercisePassQualityFloor {
		t.Fatalf("blocked-evidence tool-use score = %.2f, want at least %.2f", score, DefaultExercisePassQualityFloor)
	}
}

func TestScoreToolUse_RequiredToolAccessFailureDoesNotPass(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Find all files in the workspace that were modified in the last 24 hours, read the 3 most recent, and summarize the changes.",
		IntentToolUse,
		ComplexityComplex,
	)

	score := ScoreExerciseResponse(prompt, "I was unable to complete the task because the workspace directory could not be accessed. The list_directory tool failed with the error: absolute paths must be in allowed_paths list. Without being able to browse the directory, I cannot identify files modified in the last 24 hours.")

	if score >= DefaultExercisePassQualityFloor {
		t.Fatalf("access-failure tool-use score = %.2f, want below %.2f", score, DefaultExercisePassQualityFloor)
	}
}
