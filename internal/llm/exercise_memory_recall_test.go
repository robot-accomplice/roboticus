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
	// v1.0.6 added IntentCoding. If the count changes again, bump
	// this expectation and the matrix-total-count test together.
	if len(all) != 7 {
		t.Errorf("AllIntentClasses() count = %d, want 7", len(all))
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
	// v1.0.6: 5 complexity × 7 intent (added CODING) = 35.
	if len(ExerciseMatrix) != 35 {
		t.Errorf("ExerciseMatrix has %d prompts, want 35 (5 complexity × 7 intent)", len(ExerciseMatrix))
	}
}

// Regression: scoring must reward tool use and penalize confabulation for memory recall.

func TestScoreMemoryRecall_ToolUseRewarded(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexityModerate}
	// Good response: mentions using search_memories, reports findings.
	good := "I used search_memories to look for deployment-related entries. Found 3 results in the memory store including project timeline discussions."
	score := ScoreExerciseResponse(prompt, good)
	if score < 0.5 {
		t.Errorf("tool-using memory response scored %.2f, want >= 0.5", score)
	}
}

func TestScoreMemoryRecall_HonestyRewarded(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexitySimple}
	honest := "Let me search my memories. I don't have any stored memories about that topic. No results were found matching your query."
	score := ScoreExerciseResponse(prompt, honest)
	if score < 0.4 {
		t.Errorf("honest 'no memories' response scored %.2f, want >= 0.4", score)
	}
}

func TestScoreMemoryRecall_ConfabulationPenalized(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexityModerate}
	// Bad response: claims memories without tool evidence — confabulation.
	confab := "As I recall from our previous discussions, you mentioned wanting to refactor the authentication layer. Based on our history, I remember that the deployment was scheduled for Friday."
	score := ScoreExerciseResponse(prompt, confab)
	if score > 0.5 {
		t.Errorf("confabulated memory response scored %.2f, want <= 0.5 — should be penalized", score)
	}
}

func TestScoreMemoryRecall_EmptyIsZero(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentMemoryRecall, Complexity: ComplexityTrivial}
	score := ScoreExerciseResponse(prompt, "")
	if score != 0.0 {
		t.Errorf("empty response scored %.2f, want 0.0", score)
	}
}

// Regression: every model must have a MEMORY_RECALL baseline.

func TestCommonIntentBaselines_AllModelsHaveAllIntents(t *testing.T) {
	// Collect all unique models from baselines.
	models := make(map[string]bool)
	hasIntent := make(map[string]map[string]bool)
	for _, b := range CommonIntentBaselines {
		models[b.Model] = true
		if hasIntent[b.Model] == nil {
			hasIntent[b.Model] = make(map[string]bool)
		}
		hasIntent[b.Model][b.IntentClass] = true
	}
	required := []string{"MEMORY_RECALL", "TOOL_USE"}
	for model := range models {
		for _, intent := range required {
			if !hasIntent[model][intent] {
				t.Errorf("model %q has baselines but no %s entry", model, intent)
			}
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

func TestScoreExerciseDebug(t *testing.T) {
	tests := []struct {
		intent     IntentClass
		complexity ComplexityLevel
		content    string
	}{
		{IntentToolUse, ComplexityTrivial, "2 + 2 = 4"},
		{IntentExecution, ComplexitySimple, "The workspace contains your Go port with 24 packages and tools."},
		{IntentIntrospection, ComplexityTrivial, "I refuse requests when they cross safety boundaries or when I lack capability."},
		{IntentConversation, ComplexityTrivial, "No problem, glad it helped."},
		{IntentMemoryRecall, ComplexityTrivial, "I used search_memories to look but found no stored memories."},
	}
	for _, tt := range tests {
		p := ExercisePrompt{Intent: tt.intent, Complexity: tt.complexity}
		q := ScoreExerciseResponse(p, tt.content)
		preview := tt.content
		if len(preview) > 50 {
			preview = preview[:50]
		}
		t.Logf("%-15s C%d → Q=%.3f  %q", tt.intent, tt.complexity, q, preview)
		if q == 0.5 {
			t.Errorf("Q=0.5 exactly for %s — scoring not differentiating", tt.intent)
		}
	}
}
