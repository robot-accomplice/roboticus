package llm

import (
	"testing"
	"time"
)

func TestExercisePromptTimeout_ScalesByComplexity(t *testing.T) {
	base := 120 * time.Second

	tests := []struct {
		name   string
		prompt ExercisePrompt
		want   time.Duration
	}{
		{
			name:   "trivial rows are capped lower than generic cloud timeout",
			prompt: ExercisePrompt{Intent: IntentExecution, Complexity: ComplexityTrivial},
			want:   90 * time.Second,
		},
		{
			name:   "simple rows keep base timeout",
			prompt: ExercisePrompt{Intent: IntentExecution, Complexity: ComplexitySimple},
			want:   120 * time.Second,
		},
		{
			name:   "moderate rows get 1.5x budget",
			prompt: ExercisePrompt{Intent: IntentExecution, Complexity: ComplexityModerate},
			want:   180 * time.Second,
		},
		{
			name:   "complex rows get 2x budget",
			prompt: ExercisePrompt{Intent: IntentDelegation, Complexity: ComplexityComplex},
			want:   240 * time.Second,
		},
		{
			name:   "expert rows get 2.5x budget",
			prompt: ExercisePrompt{Intent: IntentCoding, Complexity: ComplexityExpert},
			want:   300 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExercisePromptTimeout(base, tt.prompt)
			if got != tt.want {
				t.Fatalf("ExercisePromptTimeout(%s) = %v, want %v", tt.prompt.Complexity.String(), got, tt.want)
			}
		})
	}
}

func TestExerciseTurnTimeout_IsSeparateFromModelCallTimeout(t *testing.T) {
	modelCallTimeout := 120 * time.Second

	got := ExerciseTurnTimeout(modelCallTimeout)
	want := 360 * time.Second
	if got != want {
		t.Fatalf("ExerciseTurnTimeout() = %v, want %v", got, want)
	}
}

func TestExerciseTurnTimeout_HasFiniteBaselineCap(t *testing.T) {
	got := ExerciseTurnTimeout(10 * time.Minute)
	want := 15 * time.Minute
	if got != want {
		t.Fatalf("ExerciseTurnTimeout() = %v, want cap %v", got, want)
	}
}
