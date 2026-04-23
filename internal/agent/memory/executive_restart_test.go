package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/testutil"
)

// TestExecutiveState_ResumesAcrossSimulatedRestart proves that a multi-step
// task resumes coherently after the process restarts. The flow:
//
//  1. Seed executive state for an in-flight task: plan, open question, a
//     stopping criterion, and some transient turn-summary noise.
//  2. Call PersistWorkingMemory to simulate a clean shutdown.
//  3. Re-open the store-backed manager (no process restart actually needed —
//     but the vet routine is idempotent, so running it is the exact same
//     behaviour the daemon performs on boot).
//  4. Call VetWorkingMemory with the default config: executive entries must
//     survive, transient notes must be discarded.
//  5. Load the executive state back and confirm it carries the same plan,
//     unresolved question, and stopping criterion.
//  6. Verify the context-assembly block renders the resumed state so the
//     next turn sees exactly what the pre-restart turn left behind.
func TestExecutiveState_ResumesAcrossSimulatedRestart(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mm := NewManager(DefaultConfig(), store)

	// Step 1: pre-restart state.
	if err := mm.RecordPlan(ctx, "s-resume", "t-resume", "migrate auth service", PlanPayload{
		Subgoals:   []string{"diagnose outage", "propose remediation", "schedule rollout"},
		Intent:     "analysis",
		Complexity: "complex",
		Steps:      []string{"drain traffic", "flip feature flag", "smoke test"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordUnresolvedQuestion(ctx, "s-resume", "t-resume",
		"is the legal approval gate required before rollout?",
		UnresolvedQuestionPayload{BlockingSubgoal: "schedule rollout"},
	); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordStoppingCriteria(ctx, "s-resume", "t-resume",
		"ship PR with passing tests and canary health checks",
		StoppingCriteriaPayload{Condition: "canary stable for 30 minutes"},
	); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordAssumption(ctx, "s-resume", "t-resume",
		"production runs postgres 15",
		AssumptionPayload{Source: "runbook", Confidence: 0.8},
	); err != nil {
		t.Fatal(err)
	}
	// Transient noise that must be discarded.
	mm.storeWorkingMemoryWithImportance(ctx, "s-resume", "turn_summary", "chatter", 2)
	mm.storeWorkingMemoryWithImportance(ctx, "s-resume", "note", "scratch", 2)

	// Step 2 & 3: persist + simulate boot-time vet.
	mm.PersistWorkingMemory(ctx)

	// Re-construct the manager to mimic a fresh process that reopens the store.
	rebooted := NewManager(DefaultConfig(), store)
	result := rebooted.VetWorkingMemory(ctx, DefaultVetConfig())
	if result.Retained < 4 {
		t.Fatalf("expected at least 4 executive entries retained after vet, got %+v", result)
	}

	// Step 4: load executive state and confirm shape.
	state, err := rebooted.LoadExecutiveState(ctx, "s-resume", "t-resume")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Plans) != 1 {
		t.Fatalf("expected one plan after restart, got %+v", state.Plans)
	}
	if len(state.UnresolvedQuestions) != 1 {
		t.Fatalf("expected one unresolved question after restart, got %+v", state.UnresolvedQuestions)
	}
	if len(state.StoppingCriteria) != 1 {
		t.Fatalf("expected one stopping criterion after restart, got %+v", state.StoppingCriteria)
	}
	if len(state.Assumptions) != 1 {
		t.Fatalf("expected assumption retained, got %+v", state.Assumptions)
	}
	// Plan payload must survive round-trip.
	if steps, ok := state.Plans[0].Payload["steps"].([]any); !ok || len(steps) != 3 {
		t.Fatalf("expected plan steps to survive, got %+v", state.Plans[0].Payload)
	}

	// Step 5: transient entries must be gone.
	var transient int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE session_id = ? AND entry_type IN ('turn_summary', 'note')`,
		"s-resume",
	).Scan(&transient)
	if transient != 0 {
		t.Fatalf("expected transient working-memory entries discarded, got %d", transient)
	}

	// Step 6: context assembly must render the resumed state so the next
	// turn's prompt includes exactly the same plan, question, and criterion.
	ac := AssembleContext(ctx, store, "s-resume",
		[]Evidence{{Content: "runbook.md", SourceTier: TierSemantic, Score: 0.8}},
		"", "",
	)
	formatted := ac.Format()
	if !strings.Contains(formatted, "migrate auth service") {
		t.Fatalf("expected resumed plan in assembled context, got %q", formatted)
	}
	if !strings.Contains(formatted, "legal approval gate") {
		t.Fatalf("expected resumed unresolved question in assembled context, got %q", formatted)
	}
	if !strings.Contains(formatted, "canary stable for 30 minutes") {
		t.Fatalf("expected resumed stopping-criterion payload, got %q", formatted)
	}
}

// TestExecutiveState_RestartHonorsDiscardRules ensures that while executive
// entries survive, transient entry types remain discardable via the vet config
// after a restart — i.e. the vet is not accidentally too permissive.
func TestExecutiveState_RestartHonorsDiscardRules(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mm := NewManager(DefaultConfig(), store)

	if err := mm.RecordPlan(ctx, "s-disc", "t-disc", "carry plan", PlanPayload{Subgoals: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	mm.storeWorkingMemoryWithImportance(ctx, "s-disc", "turn_summary", "s1", 5)
	mm.storeWorkingMemoryWithImportance(ctx, "s-disc", "note", "n1", 5)
	mm.storeWorkingMemoryWithImportance(ctx, "s-disc", "goal", "keep me", 8)

	mm.PersistWorkingMemory(ctx)
	mm.VetWorkingMemory(ctx, DefaultVetConfig())

	var planCount, goalCount, noteCount int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE entry_type = 'plan'`,
	).Scan(&planCount)
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE entry_type = 'goal'`,
	).Scan(&goalCount)
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE entry_type IN ('turn_summary', 'note')`,
	).Scan(&noteCount)

	if planCount != 1 {
		t.Fatalf("expected plan retained, got %d", planCount)
	}
	if goalCount != 1 {
		t.Fatalf("expected goal retained, got %d", goalCount)
	}
	if noteCount != 0 {
		t.Fatalf("expected transient entries discarded, got %d", noteCount)
	}
}
