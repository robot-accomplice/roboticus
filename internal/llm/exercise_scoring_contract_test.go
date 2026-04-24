package llm

import "testing"

func findExercisePrompt(t *testing.T, raw string) ExercisePrompt {
	t.Helper()
	for _, p := range ExerciseMatrix {
		if p.Prompt == raw {
			return p
		}
	}
	t.Fatalf("exercise prompt %q not found", raw)
	return ExercisePrompt{}
}

func TestScoreExerciseResponse_RespectsConciseTimeContract(t *testing.T) {
	prompt := findExercisePrompt(t, "What time is it?")
	score := ScoreExerciseResponse(prompt, "It's 7:26 PM (19:26) on April 23rd, 2026.")
	if score < 0.8 {
		t.Fatalf("concise time answer scored %.2f, want >= 0.80", score)
	}
}

func TestScoreExerciseResponse_RespectsGreetingContract(t *testing.T) {
	prompt := findExercisePrompt(t, "Say hello.")
	score := ScoreExerciseResponse(prompt, "Hey. How's it going?")
	if score < 0.8 {
		t.Fatalf("concise greeting scored %.2f, want >= 0.80", score)
	}
}

func TestScoreExerciseResponse_RespectsDirectFactContract(t *testing.T) {
	prompt := findExercisePrompt(t, "What is 2 + 2?")
	score := ScoreExerciseResponse(prompt, "2 + 2 = 4.")
	if score < 0.8 {
		t.Fatalf("concise arithmetic answer scored %.2f, want >= 0.80", score)
	}
}

func TestScoreExerciseResponse_DirectFactContractPenalizesToolTheater(t *testing.T) {
	prompt := findExercisePrompt(t, "What is 2 + 2?")
	direct := ScoreExerciseResponse(prompt, "4")
	theatrical := ScoreExerciseResponse(prompt, "I used bash and command output to compute this. The result is 4.")
	if theatrical >= direct {
		t.Fatalf("tool-theatrical arithmetic answer (%.2f) should score below direct answer (%.2f)", theatrical, direct)
	}
}
