package llm

import (
	"testing"
)

// Regression: IntentMemoryRecall must exist as a valid intent class.

func TestIntentMemoryRecall_StringRoundTrip(t *testing.T) {
	s := IntentMemoryRecall.String()
	if s != "MEMORY_RECALL" {
		t.Errorf("IntentMemoryRecall.String() = %q, want MEMORY_RECALL", s)
	}
	parsed := ParseIntentClass(s)
	if parsed != IntentMemoryRecall {
		t.Errorf("ParseIntentClass(%q) = %v, want IntentMemoryRecall", s, parsed)
	}
}

func TestAllIntentClasses_IncludesMemoryRecall(t *testing.T) {
	all := AllIntentClasses()
	found := false
	for _, ic := range all {
		if ic == IntentMemoryRecall {
			found = true
			break
		}
	}
	if !found {
		t.Error("AllIntentClasses() should include IntentMemoryRecall")
	}
	if len(all) != 5 {
		t.Errorf("AllIntentClasses() count = %d, want 5", len(all))
	}
}

func TestExerciseMatrix_HasMemoryRecallPrompts(t *testing.T) {
	count := 0
	for _, p := range ExerciseMatrix {
		if p.Intent == IntentMemoryRecall {
			count++
		}
	}
	if count != 5 {
		t.Errorf("ExerciseMatrix has %d MEMORY_RECALL prompts, want 5 (one per complexity)", count)
	}
}

func TestExerciseMatrix_TotalCount(t *testing.T) {
	if len(ExerciseMatrix) != 25 {
		t.Errorf("ExerciseMatrix has %d prompts, want 25 (5 complexity x 5 intent)", len(ExerciseMatrix))
	}
}

// Regression: scoring must reward tool use and penalize confabulation for memory recall.

func TestScoreMemoryRecall_ToolUseRewarded(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexityModerate}
	// Good response: mentions using search_memories, reports findings.
	good := "I used search_memories to look for deployment-related entries. Found 3 results in the memory store including project timeline discussions."
	score := scoreExerciseResponse(prompt, good)
	if score < 0.5 {
		t.Errorf("tool-using memory response scored %.2f, want >= 0.5", score)
	}
}

func TestScoreMemoryRecall_HonestyRewarded(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexitySimple}
	honest := "Let me search my memories. I don't have any stored memories about that topic. No results were found matching your query."
	score := scoreExerciseResponse(prompt, honest)
	if score < 0.4 {
		t.Errorf("honest 'no memories' response scored %.2f, want >= 0.4", score)
	}
}

func TestScoreMemoryRecall_ConfabulationPenalized(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexityModerate}
	// Bad response: claims memories without tool evidence — confabulation.
	confab := "As I recall from our previous discussions, you mentioned wanting to refactor the authentication layer. Based on our history, I remember that the deployment was scheduled for Friday."
	score := scoreExerciseResponse(prompt, confab)
	if score > 0.5 {
		t.Errorf("confabulated memory response scored %.2f, want <= 0.5 — should be penalized", score)
	}
}

func TestScoreMemoryRecall_EmptyIsZero(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexityTrivial}
	score := scoreExerciseResponse(prompt, "")
	if score != 0.0 {
		t.Errorf("empty response scored %.2f, want 0.0", score)
	}
}

// Regression: every model must have a MEMORY_RECALL baseline.

func TestCommonIntentBaselines_AllModelsHaveMemoryRecall(t *testing.T) {
	// Collect all unique models from baselines.
	models := make(map[string]bool)
	hasRecall := make(map[string]bool)
	for _, b := range CommonIntentBaselines {
		models[b.Model] = true
		if b.IntentClass == "MEMORY_RECALL" {
			hasRecall[b.Model] = true
		}
	}
	for model := range models {
		if !hasRecall[model] {
			t.Errorf("model %q has baselines but no MEMORY_RECALL entry", model)
		}
	}
}

func TestLookupBaselineQuality_IncludesMemoryRecall(t *testing.T) {
	info := LookupBaselineQuality("moonshot/kimi-k2-turbo-preview")
	if !info.Known {
		t.Fatal("Kimi K2 should have baseline data")
	}
	recall, ok := info.ByIntent["MEMORY_RECALL"]
	if !ok {
		t.Fatal("Kimi K2 should have MEMORY_RECALL baseline")
	}
	if recall < 0.5 || recall > 1.0 {
		t.Errorf("Kimi K2 MEMORY_RECALL baseline = %.2f, want 0.5-1.0", recall)
	}
}
