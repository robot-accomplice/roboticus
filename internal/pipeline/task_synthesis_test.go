package pipeline

import (
	"testing"
)

func TestFormatPlannedAction(t *testing.T) {
	tests := []struct {
		action string
		want   string
	}{
		{"execute_directly", "Execute Directly"},
		{"delegate_to_specialist", "Delegate to Specialist"},
		{"compose_subagent", "Compose Sub-Agent"},
		{"unknown", "Execute Directly"},
	}
	for _, tt := range tests {
		got := FormatPlannedAction(tt.action)
		if got != tt.want {
			t.Errorf("FormatPlannedAction(%q) = %q, want %q", tt.action, got, tt.want)
		}
	}
}

func TestMapPlannedAction_ExecuteDirectly(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "execute_directly", Confidence: 0.8}
	decomp := &DecompositionResult{Decision: DecompCentralized}

	decision := MapPlannedAction(synthesis, decomp)
	if decision != ActionGateContinue {
		t.Errorf("expected ActionGateContinue, got %d", decision)
	}
}

func TestMapPlannedAction_DelegateWithAgreement(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "delegate_to_specialist", Confidence: 0.7}
	decomp := &DecompositionResult{Decision: DecompDelegated}

	decision := MapPlannedAction(synthesis, decomp)
	if decision != ActionGateDelegate {
		t.Errorf("expected ActionGateDelegate, got %d", decision)
	}
}

func TestMapPlannedAction_DelegateHighConfidenceOverride(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "delegate_to_specialist", Confidence: 0.75}
	// Decomp is nil — planner's high confidence should override.
	decision := MapPlannedAction(synthesis, nil)
	if decision != ActionGateDelegate {
		t.Errorf("expected ActionGateDelegate with high confidence, got %d", decision)
	}
}

func TestMapPlannedAction_DelegateLowConfidenceFallsThrough(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "delegate_to_specialist", Confidence: 0.5}
	decomp := &DecompositionResult{Decision: DecompCentralized}

	decision := MapPlannedAction(synthesis, decomp)
	if decision != ActionGateContinue {
		t.Errorf("expected ActionGateContinue with low confidence, got %d", decision)
	}
}

func TestMapPlannedAction_ComposeSubagent(t *testing.T) {
	synthesis := TaskSynthesis{
		PlannedAction: "compose_subagent",
		Confidence:    0.7,
		CapabilityFit: 0.2, // Low fit → specialist proposal
	}

	decision := MapPlannedAction(synthesis, nil)
	if decision != ActionGateSpecialistPropose {
		t.Errorf("expected ActionGateSpecialistPropose, got %d", decision)
	}
}

func TestMapPlannedAction_ComposeSubagentHighFitFallsThrough(t *testing.T) {
	synthesis := TaskSynthesis{
		PlannedAction: "compose_subagent",
		Confidence:    0.7,
		CapabilityFit: 0.8, // High fit → no specialist needed
	}

	decision := MapPlannedAction(synthesis, nil)
	if decision != ActionGateContinue {
		t.Errorf("expected ActionGateContinue with high fit, got %d", decision)
	}
}

func TestSynthesizeTaskState_TreatsColloquialGreetingAsConversational(t *testing.T) {
	result := SynthesizeTaskState("What's the good word?", 2, nil)
	if result.Intent != "conversational" {
		t.Fatalf("intent = %q, want conversational", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.RetrievalNeeded {
		t.Fatal("colloquial greeting should not require retrieval")
	}
}

func TestSynthesizeTaskState_TreatsWhatsNewAsConversational(t *testing.T) {
	result := SynthesizeTaskState("What's new, Duncan?", 2, nil)
	if result.Intent != "conversational" {
		t.Fatalf("intent = %q, want conversational", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.RetrievalNeeded {
		t.Fatal("phatic what's-new turn should not require retrieval")
	}
}

func TestSynthesizeTaskState_SimpleDirectTaskDoesNotRequireRetrieval(t *testing.T) {
	result := SynthesizeTaskState("Create a new markdown document in the Obsidian vault for today's notes.", 2, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("simple direct vault authoring should not require retrieval")
	}
}

func TestSynthesizeTaskState_TaskWithContinuityCueRequiresRetrieval(t *testing.T) {
	result := SynthesizeTaskState("Update the existing Obsidian note we discussed earlier with today's decisions.", 2, nil)
	if !result.RetrievalNeeded {
		t.Fatal("task with explicit continuity cue should require retrieval")
	}
}

func TestSynthesizeTaskState_InvestigativeTaskRequiresRetrieval(t *testing.T) {
	result := SynthesizeTaskState("Create a report that explains the root cause and identifies which systems were affected.", 1, nil)
	if !result.RetrievalNeeded {
		t.Fatal("investigative reporting task should require retrieval")
	}
}

func TestSynthesizeTaskState_NoteTitleWithTestDoesNotBecomeCode(t *testing.T) {
	result := SynthesizeTaskState("Create a new Obsidian note named codex-live-test.md in the vault containing exactly: # Codex Live Test.", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.RetrievalNeeded {
		t.Fatal("simple vault note creation should not require retrieval")
	}
}

func TestSynthesizeTaskState_CapabilityFitRecognizesHyphenatedSkillConcepts(t *testing.T) {
	result := SynthesizeTaskState(
		"Create a new markdown document in the Obsidian vault for today's notes.",
		1,
		[]string{"obsidian-vault Read and write to the shared Obsidian vault for persistent memory"},
	)
	if result.CapabilityFit <= 0 {
		t.Fatalf("capability fit = %v, want > 0", result.CapabilityFit)
	}
	for _, missing := range result.MissingSkills {
		if missing == "obsidian" || missing == "vault" {
			t.Fatalf("missing skills unexpectedly includes %q", missing)
		}
	}
}
