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
