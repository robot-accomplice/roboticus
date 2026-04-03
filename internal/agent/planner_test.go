package agent

import "testing"

func TestPlanner_Conversation(t *testing.T) {
	p := NewActionPlanner(true)
	state := &TaskOperatingState{Classification: ClassConversation}
	plan := p.Plan(state)
	if plan.Selected != ActionAnswerDirectly {
		t.Errorf("conversation should select AnswerDirectly, got %s", plan.Selected)
	}
	if plan.Candidates[0].Confidence < 0.9 {
		t.Errorf("confidence should be high, got %f", plan.Candidates[0].Confidence)
	}
}

func TestPlanner_BreakerOpen(t *testing.T) {
	p := NewActionPlanner(true)
	state := &TaskOperatingState{
		Classification:    ClassTask,
		RuntimeConstraint: RuntimeConstraints{BreakerOpen: true},
	}
	plan := p.Plan(state)
	if plan.Selected != TaskActionReturnBlocker {
		t.Errorf("breaker open should select ReturnBlocker, got %s", plan.Selected)
	}
}

func TestPlanner_ExplicitWorkflowWithRoster(t *testing.T) {
	p := NewActionPlanner(true)
	state := &TaskOperatingState{
		Classification: ClassTask,
		RosterFit:      RosterFit{ExplicitWorkflow: true, FitCount: 2, FitNames: []string{"coder", "reviewer"}},
	}
	plan := p.Plan(state)
	if plan.Selected != ActionDelegateToSpecialist {
		t.Errorf("explicit workflow + roster should delegate, got %s", plan.Selected)
	}
}

func TestPlanner_RecallGap(t *testing.T) {
	p := NewActionPlanner(true)
	state := &TaskOperatingState{
		Classification:    ClassTask,
		MemoryConfidence:  MemoryConfidence{RecallGap: true, AvgSimilarity: 0.3},
		RuntimeConstraint: RuntimeConstraints{RemainingBudget: 10000},
	}
	plan := p.Plan(state)
	found := false
	for _, c := range plan.Candidates {
		if c.Action == ActionInspectMemory {
			found = true
			break
		}
	}
	if !found {
		t.Error("recall gap should produce InspectMemory candidate")
	}
}

func TestPlanner_ProtocolIssues(t *testing.T) {
	p := NewActionPlanner(true)
	state := &TaskOperatingState{
		Classification: ClassTask,
		Behavioral:     BehavioralHistory{ProtocolIssues: true, NormRetryStreak: 1},
	}
	plan := p.Plan(state)
	found := false
	for _, c := range plan.Candidates {
		if c.Action == TaskActionNormalizationRetry {
			found = true
			if c.Confidence < 0.7 {
				t.Errorf("normalization retry confidence too low: %f", c.Confidence)
			}
			break
		}
	}
	if !found {
		t.Error("protocol issues should produce NormalizationRetry candidate")
	}
}

func TestPlanner_Disabled(t *testing.T) {
	p := NewActionPlanner(false)
	plan := p.Plan(&TaskOperatingState{})
	if plan.Selected != ActionContinueCentralized {
		t.Errorf("disabled planner should default to ContinueCentralized, got %s", plan.Selected)
	}
}

func TestPlanner_Fallback(t *testing.T) {
	p := NewActionPlanner(true)
	state := &TaskOperatingState{Classification: ClassTask}
	plan := p.Plan(state)
	if len(plan.Candidates) == 0 {
		t.Error("should always have at least one candidate")
	}
}

func TestSynthesizeState_TaskClassification(t *testing.T) {
	state := SynthesizeState(TaskStateInput{Intents: []string{"execution"}})
	if state.Classification != ClassTask {
		t.Error("execution intent should classify as task")
	}
}

func TestSynthesizeState_ConversationClassification(t *testing.T) {
	state := SynthesizeState(TaskStateInput{Intents: []string{"conversation"}})
	if state.Classification != ClassConversation {
		t.Error("conversation intent should classify as conversation")
	}
}

func TestSynthesizeState_BudgetPressure(t *testing.T) {
	state := SynthesizeState(TaskStateInput{RemainingBudgetTokens: 1500})
	if !state.RuntimeConstraint.BudgetPressured {
		t.Error("1500 tokens should trigger budget pressure")
	}
}

func TestSynthesizeState_StructuralRepetition(t *testing.T) {
	state := SynthesizeState(TaskStateInput{
		RecentResponseSkeletons: []string{"intro+list", "intro+list", "intro+list"},
	})
	if !state.Behavioral.StructuralRepetition {
		t.Error("3 identical skeletons should detect repetition")
	}
	if state.Behavioral.RepetitionStreak != 3 {
		t.Errorf("streak = %d, want 3", state.Behavioral.RepetitionStreak)
	}
}
