package pipeline

import (
	"context"
	"sort"
	"strings"
	"testing"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/session"
	"roboticus/testutil"
)

func TestDiffPlanSubgoals_AddsAndRemoves(t *testing.T) {
	before := []string{"A", "B", "C"}
	after := []string{"B", "C", "D"}
	added, removed := diffPlanSubgoals(before, after)
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) != 1 || added[0] != "D" {
		t.Fatalf("expected added=[D], got %+v", added)
	}
	if len(removed) != 1 || removed[0] != "A" {
		t.Fatalf("expected removed=[A], got %+v", removed)
	}
}

func TestDiffPlanSubgoals_CaseInsensitive(t *testing.T) {
	before := []string{"Diagnose Outage"}
	after := []string{"diagnose outage"}
	added, removed := diffPlanSubgoals(before, after)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("expected no diff for case variants, got added=%+v removed=%+v", added, removed)
	}
}

func TestRecordTaskSynthesisPlan_RecordsCheckpointOnSubgoalChange(t *testing.T) {
	store := testutil.TempStore(t)
	p := &Pipeline{store: store}
	ctx := context.Background()

	pc := &pipelineContext{
		taskID: "t-plan",
		synthesis: TaskSynthesis{
			Intent:     "analysis",
			Complexity: "complex",
		},
		content: "diagnose outage and propose remediation",
	}
	pc.session = session.New("s-plan", "a1", "Bot")

	// First plan: three subgoals. No prior plan exists, so no checkpoint fires.
	p.recordTaskSynthesisPlan(ctx, pc, []string{"diagnose outage", "propose remediation", "schedule rollout"})

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	state, err := mm.LoadExecutiveState(ctx, "s-plan", "t-plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.DecisionCheckpoints) != 0 {
		t.Fatalf("expected no checkpoint on initial plan, got %+v", state.DecisionCheckpoints)
	}

	// Second plan: drop "schedule rollout", add "notify stakeholders".
	p.recordTaskSynthesisPlan(ctx, pc, []string{"diagnose outage", "propose remediation", "notify stakeholders"})

	state, err = mm.LoadExecutiveState(ctx, "s-plan", "t-plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.DecisionCheckpoints) != 1 {
		t.Fatalf("expected one decision checkpoint on plan revision, got %+v", state.DecisionCheckpoints)
	}
	cp := state.DecisionCheckpoints[0]
	if !strings.Contains(cp.Content, "notify stakeholders") {
		t.Fatalf("expected added subgoal in checkpoint content, got %q", cp.Content)
	}
	if !strings.Contains(cp.Content, "schedule rollout") {
		t.Fatalf("expected removed subgoal in checkpoint content, got %q", cp.Content)
	}

	// Third call with identical subgoals should NOT record another checkpoint.
	p.recordTaskSynthesisPlan(ctx, pc, []string{"diagnose outage", "propose remediation", "notify stakeholders"})
	state, err = mm.LoadExecutiveState(ctx, "s-plan", "t-plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.DecisionCheckpoints) != 1 {
		t.Fatalf("expected no new checkpoint when subgoals unchanged, got %+v", state.DecisionCheckpoints)
	}
}
