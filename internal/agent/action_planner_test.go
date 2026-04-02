package agent

import (
	"testing"

	"goboticus/internal/llm"
)

func TestPlanNextAction(t *testing.T) {
	tests := []struct {
		name       string
		state      *OperatingState
		input      string
		history    []llm.Message
		wantAction PlannedAction
	}{
		{
			name:       "default is infer",
			state:      &OperatingState{},
			input:      "hello",
			wantAction: ActionInfer,
		},
		{
			name:       "pending approval triggers wait",
			state:      &OperatingState{PendingApproval: true},
			input:      "check status",
			wantAction: ActionWait,
		},
		{
			name:       "pending delegation triggers delegate",
			state:      &OperatingState{PendingDelegation: true},
			input:      "what happened",
			wantAction: ActionDelegate,
		},
		{
			name:       "matched skill triggers skill exec",
			state:      &OperatingState{MatchedSkill: "weather"},
			input:      "weather in NYC",
			wantAction: ActionSkillExec,
		},
		{
			name:       "memory-only query",
			state:      &OperatingState{},
			input:      "what did we discuss yesterday?",
			wantAction: ActionRetrieve,
		},
		{
			name:       "escalation on low confidence",
			state:      &OperatingState{Confidence: 0.2, CanEscalate: true},
			input:      "explain quantum computing",
			wantAction: ActionEscalate,
		},
		{
			name:       "no escalation when not available",
			state:      &OperatingState{Confidence: 0.2, CanEscalate: false},
			input:      "explain quantum computing",
			wantAction: ActionInfer,
		},
		{
			name:       "approval takes priority over delegation",
			state:      &OperatingState{PendingApproval: true, PendingDelegation: true},
			input:      "anything",
			wantAction: ActionWait,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := PlanNextAction(tt.state, tt.input, tt.history)
			if plan.Action != tt.wantAction {
				t.Errorf("PlanNextAction() = %v, want %v (reason: %s)", plan.Action, tt.wantAction, plan.Reason)
			}
			if plan.Reason == "" {
				t.Error("plan should always have a reason")
			}
		})
	}
}

func TestActionPlan_Confidence(t *testing.T) {
	plan := PlanNextAction(&OperatingState{PendingApproval: true}, "test", nil)
	if plan.Confidence <= 0 || plan.Confidence > 1 {
		t.Errorf("confidence = %.2f, should be in (0, 1]", plan.Confidence)
	}
}
