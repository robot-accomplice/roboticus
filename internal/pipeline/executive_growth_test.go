package pipeline

import (
	"context"
	"testing"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/db"
	"roboticus/internal/session"
	"roboticus/testutil"
)

// newGrowthTestPipeline builds a minimal Pipeline wired only with the store so
// post-turn growth can be exercised without spinning up the full dependency
// graph.
func newGrowthTestPipeline(t *testing.T) *Pipeline {
	t.Helper()
	store := testutil.TempStore(t)
	return &Pipeline{store: store}
}

func seedPlan(t *testing.T, store *db.Store, sessionID, taskID string, subgoals []string) {
	t.Helper()
	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	if err := mm.RecordPlan(context.Background(), sessionID, taskID, "task plan", agentmemory.PlanPayload{
		Subgoals: subgoals,
	}); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

func TestGrowExecutiveState_RecordsVerifiedConclusion(t *testing.T) {
	p := newGrowthTestPipeline(t)
	seedPlan(t, p.store, "s1", "t1", []string{"identify affected systems"})

	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("What systems are affected by the cache bug?")
	sess.SetTaskVerificationHints("analysis", "moderate", "execute_directly", []string{"identify affected systems"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] Billing service depends on Ledger service\n")

	p.growExecutiveState(context.Background(), sess,
		"The affected systems are billing and ledger based on the dependency evidence.")

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	state, err := mm.LoadExecutiveState(context.Background(), "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.VerifiedConclusions) != 1 {
		t.Fatalf("expected one verified conclusion, got %+v", state.VerifiedConclusions)
	}
	if len(state.UnresolvedQuestions) != 0 {
		t.Fatalf("expected no unresolved questions when subgoal is covered, got %+v", state.UnresolvedQuestions)
	}
}

func TestGrowExecutiveState_OpensUnresolvedQuestion(t *testing.T) {
	p := newGrowthTestPipeline(t)
	seedPlan(t, p.store, "s1", "t1", []string{"identify affected systems", "propose remediation plan"})

	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Identify affected systems and propose a remediation plan.")
	sess.SetTaskVerificationHints("analysis", "complex", "execute_directly",
		[]string{"identify affected systems", "propose remediation plan"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] Billing service depends on Ledger service\n")

	// Response only covers the first subgoal; the second remains unresolved.
	p.growExecutiveState(context.Background(), sess,
		"The affected systems are billing and ledger.")

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	state, err := mm.LoadExecutiveState(context.Background(), "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.UnresolvedQuestions) != 1 {
		t.Fatalf("expected one unresolved question, got %+v", state.UnresolvedQuestions)
	}
	if state.UnresolvedQuestions[0].Payload["blocking_subgoal"] != "propose remediation plan" {
		t.Fatalf("expected blocking_subgoal payload, got %+v", state.UnresolvedQuestions[0].Payload)
	}
}

func TestGrowExecutiveState_ResolvesPriorOpenQuestion(t *testing.T) {
	p := newGrowthTestPipeline(t)
	ctx := context.Background()
	seedPlan(t, p.store, "s1", "t1", []string{"identify affected systems"})

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	if err := mm.RecordUnresolvedQuestion(ctx, "s1", "t1",
		"unresolved: identify affected systems",
		agentmemory.UnresolvedQuestionPayload{BlockingSubgoal: "identify affected systems"}); err != nil {
		t.Fatal(err)
	}

	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Which systems were affected?")
	sess.SetTaskVerificationHints("analysis", "moderate", "execute_directly", []string{"identify affected systems"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] Billing and ledger are affected\n")

	p.growExecutiveState(ctx, sess,
		"The affected systems are billing and ledger, with the ledger service downstream.")

	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.UnresolvedQuestions) != 0 {
		t.Fatalf("expected prior question resolved, got %+v", state.UnresolvedQuestions)
	}
	if len(state.VerifiedConclusions) != 1 {
		t.Fatalf("expected verified conclusion recorded, got %+v", state.VerifiedConclusions)
	}
}

func TestGrowExecutiveState_DoesNotResolveWhenResponseIsUncertain(t *testing.T) {
	p := newGrowthTestPipeline(t)
	ctx := context.Background()
	seedPlan(t, p.store, "s1", "t1", []string{"identify affected systems"})

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	if err := mm.RecordUnresolvedQuestion(ctx, "s1", "t1",
		"unresolved: identify affected systems",
		agentmemory.UnresolvedQuestionPayload{BlockingSubgoal: "identify affected systems"}); err != nil {
		t.Fatal(err)
	}

	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Which systems were affected?")
	sess.SetTaskVerificationHints("analysis", "moderate", "execute_directly", []string{"identify affected systems"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] Billing may be affected\n[Gaps]\n- No relevant procedures\n")

	// Response uses hedged language — must not auto-resolve the open question.
	p.growExecutiveState(ctx, sess,
		"Based on the available evidence, I'm not certain which systems were affected. We need more data before we can confirm.")

	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.UnresolvedQuestions) < 1 {
		t.Fatalf("expected unresolved question to remain when response is uncertain, got %+v", state.UnresolvedQuestions)
	}
}

func TestGrowExecutiveState_ReturnsCountsForTelemetry(t *testing.T) {
	p := newGrowthTestPipeline(t)
	ctx := context.Background()
	seedPlan(t, p.store, "s1", "t1", []string{"identify affected systems", "propose remediation plan"})

	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Identify affected systems and propose a remediation plan.")
	sess.SetTaskVerificationHints("analysis", "complex", "execute_directly",
		[]string{"identify affected systems", "propose remediation plan"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] Billing and ledger affected\n")

	result := p.growExecutiveState(ctx, sess,
		"The affected systems are billing and ledger.")

	if result.TaskID != "t1" {
		t.Fatalf("expected TaskID=t1, got %q", result.TaskID)
	}
	if result.VerifiedRecorded != 1 {
		t.Fatalf("expected one verified recorded, got %+v", result)
	}
	if result.QuestionsOpened != 1 {
		t.Fatalf("expected one question opened (remediation plan uncovered), got %+v", result)
	}
}

func TestAnnotateExecutivePlanWrite_RecordsSubgoalsAndDiff(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("task_synthesis")
	AnnotateExecutivePlanWrite(tr, "t-1",
		[]string{"diagnose", "remediate", "notify"},
		[]string{"notify"},
		[]string{"rollback"},
	)
	tr.EndSpan("ok")

	trace := tr.Finish("turn-1", "test")
	if len(trace.Stages) == 0 {
		t.Fatal("expected stage to be recorded")
	}
	meta := trace.Stages[0].Metadata
	if got, ok := meta["executive.plan_recorded"].(bool); !ok || !got {
		t.Fatalf("expected executive.plan_recorded=true, got %+v", meta["executive.plan_recorded"])
	}
	if got, ok := meta["executive.checkpoint_recorded"].(bool); !ok || !got {
		t.Fatalf("expected executive.checkpoint_recorded=true, got %+v", meta["executive.checkpoint_recorded"])
	}
	if _, ok := meta["executive.subgoals_added"]; !ok {
		t.Fatalf("expected executive.subgoals_added annotation")
	}
	if _, ok := meta["executive.subgoals_removed"]; !ok {
		t.Fatalf("expected executive.subgoals_removed annotation")
	}
}

func TestAnnotateExecutivePlanWrite_OmitsCheckpointWhenUnchanged(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("task_synthesis")
	AnnotateExecutivePlanWrite(tr, "t-1", []string{"diagnose"}, nil, nil)
	tr.EndSpan("ok")

	meta := tr.Finish("turn-1", "test").Stages[0].Metadata
	if _, ok := meta["executive.checkpoint_recorded"]; ok {
		t.Fatal("should not annotate checkpoint when subgoals unchanged")
	}
}

func TestExtractAssumptions_PicksUpExplicitMarkers(t *testing.T) {
	response := "I'll assume that production uses postgres 15. Presumably, the ledger service is available. Assuming we have admin access, the upgrade is straightforward."
	got := extractAssumptions(response)
	if len(got) != 3 {
		t.Fatalf("expected 3 assumptions, got %d: %+v", len(got), got)
	}
}

func TestExtractAssumptions_IgnoresFalsePositives(t *testing.T) {
	response := "Reassuming the prior session is a bad idea."
	got := extractAssumptions(response)
	if len(got) != 0 {
		t.Fatalf("expected no assumptions (word-boundary false positive), got %+v", got)
	}
}

func TestExtractAssumptions_Deduplicates(t *testing.T) {
	response := "I'll assume the service is up. I will assume the service is up."
	got := extractAssumptions(response)
	if len(got) != 1 {
		t.Fatalf("expected deduplicated assumptions, got %+v", got)
	}
}

func TestGrowExecutiveState_RecordsAssumptionsFromResponse(t *testing.T) {
	p := newGrowthTestPipeline(t)
	ctx := context.Background()
	seedPlan(t, p.store, "s1", "t1", []string{"migrate auth service"})

	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Migrate the auth service.")
	sess.SetTaskVerificationHints("analysis", "complex", "execute_directly", []string{"migrate auth service"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] auth service migration runbook\n")

	result := p.growExecutiveState(ctx, sess,
		"I'll assume that the new auth service runs on the same ports. Presumably, the ledger team has signed off on the rollout.")

	if result.AssumptionsRecorded != 2 {
		t.Fatalf("expected 2 assumptions recorded, got %+v", result)
	}

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Assumptions) != 2 {
		t.Fatalf("expected two assumption entries persisted, got %+v", state.Assumptions)
	}
}

func TestGrowExecutiveState_IdempotentOnRepeatedRuns(t *testing.T) {
	p := newGrowthTestPipeline(t)
	ctx := context.Background()
	seedPlan(t, p.store, "s1", "t1", []string{"identify affected systems"})

	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("What systems are affected?")
	sess.SetTaskVerificationHints("analysis", "moderate", "execute_directly", []string{"identify affected systems"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] Billing and ledger affected\n")

	for i := 0; i < 3; i++ {
		p.growExecutiveState(ctx, sess,
			"The affected systems are billing and ledger.")
	}

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.VerifiedConclusions) != 1 {
		t.Fatalf("expected exactly one verified conclusion after repeat runs, got %d", len(state.VerifiedConclusions))
	}
}
