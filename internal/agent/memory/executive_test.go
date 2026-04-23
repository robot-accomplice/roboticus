package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"roboticus/testutil"
)

func TestRecordPlan_ReplacesPriorPlanForSameTask(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordPlan(ctx, "s1", "t1", "Investigate auth outage", PlanPayload{
		Subgoals: []string{"identify root cause", "propose remediation"},
	}); err != nil {
		t.Fatalf("first plan: %v", err)
	}
	if err := mm.RecordPlan(ctx, "s1", "t1", "Investigate auth outage v2", PlanPayload{
		Subgoals: []string{"identify root cause", "propose remediation", "schedule rollout"},
	}); err != nil {
		t.Fatalf("second plan: %v", err)
	}

	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.Plans) != 1 {
		t.Fatalf("expected exactly one plan after replacement, got %d", len(state.Plans))
	}
	if !strings.Contains(state.Plans[0].Content, "v2") {
		t.Fatalf("expected latest plan content to win, got %q", state.Plans[0].Content)
	}
}

func TestExecutiveState_RoundTripsAllEntryTypes(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordPlan(ctx, "s1", "t1", "the plan", PlanPayload{Subgoals: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordAssumption(ctx, "s1", "t1", "production uses postgres 15", AssumptionPayload{Confidence: 0.7}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordUnresolvedQuestion(ctx, "s1", "t1", "is rollout gated by legal?", UnresolvedQuestionPayload{BlockingSubgoal: "schedule rollout"}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordVerifiedConclusion(ctx, "s1", "t1", "cache invalidation was the trigger", VerifiedConclusionPayload{SupportingEvidence: []string{"incident-123"}}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordDecisionCheckpoint(ctx, "s1", "t1", "chose option B", DecisionCheckpointPayload{Chosen: "B", Considered: []string{"A", "B", "C"}, Rationale: "lower blast radius"}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordStoppingCriteria(ctx, "s1", "t1", "ship a PR with tests", StoppingCriteriaPayload{Condition: "all tests pass"}); err != nil {
		t.Fatal(err)
	}

	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.Plans) != 1 || len(state.Assumptions) != 1 || len(state.UnresolvedQuestions) != 1 ||
		len(state.VerifiedConclusions) != 1 || len(state.DecisionCheckpoints) != 1 || len(state.StoppingCriteria) != 1 {
		t.Fatalf("expected one of each entry, got %+v", state)
	}
}

func TestFormatForContext_RendersStructuredBlock(t *testing.T) {
	state := &ExecutiveState{
		TaskID: "t1",
		Plans: []ExecutiveEntry{
			{EntryType: EntryPlan, Content: "investigate auth outage", Payload: map[string]any{"steps": []any{"s1", "s2"}}},
		},
		Assumptions: []ExecutiveEntry{
			{EntryType: EntryAssumption, Content: "prod uses pg 15", Payload: map[string]any{"confidence": 0.7}},
		},
		UnresolvedQuestions: []ExecutiveEntry{
			{EntryType: EntryUnresolvedQuestion, Content: "is rollout gated by legal?"},
		},
		StoppingCriteria: []ExecutiveEntry{
			{EntryType: EntryStoppingCriteria, Content: "ship PR", Payload: map[string]any{"condition": "all tests pass"}},
		},
	}
	block := state.FormatForContext()
	if !strings.Contains(block, "Plan:") || !strings.Contains(block, "Assumptions:") ||
		!strings.Contains(block, "Unresolved questions:") || !strings.Contains(block, "Stopping criteria:") {
		t.Fatalf("expected block to carry all sections, got %q", block)
	}
	if !strings.Contains(block, "steps=s1 → s2") {
		t.Fatalf("expected plan steps rendered in payload detail, got %q", block)
	}
	if !strings.Contains(block, "confidence=0.70") {
		t.Fatalf("expected assumption confidence rendered, got %q", block)
	}
}

func TestVetWorkingMemory_PreservesExecutiveState(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	// Seed executive state plus a transient turn summary that should be discarded.
	if err := mm.RecordPlan(ctx, "s1", "t1", "active plan", PlanPayload{Subgoals: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordUnresolvedQuestion(ctx, "s1", "t1", "still open", UnresolvedQuestionPayload{}); err != nil {
		t.Fatal(err)
	}
	mm.storeWorkingMemoryWithImportance(ctx, "s1", "turn_summary", "noise", 3)
	mm.storeWorkingMemoryWithImportance(ctx, "s1", "note", "noise2", 2)

	// Simulate a shutdown + restart cycle.
	mm.PersistWorkingMemory(ctx)
	cfg := DefaultVetConfig()
	result := mm.VetWorkingMemory(ctx, cfg)
	if result.Retained < 2 {
		t.Fatalf("expected at least two executive entries retained, got %+v", result)
	}

	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.Plans) != 1 {
		t.Fatalf("expected plan to survive vetting, got %+v", state.Plans)
	}
	if len(state.UnresolvedQuestions) != 1 {
		t.Fatalf("expected unresolved question to survive vetting, got %+v", state.UnresolvedQuestions)
	}

	// Transient entries must be discarded.
	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE session_id = ? AND entry_type IN ('turn_summary', 'note')`,
		"s1",
	).Scan(&count)
	if count != 0 {
		t.Fatalf("expected transient working-memory entries discarded, got %d", count)
	}
}

func TestVetWorkingMemory_HonorsExecutiveMaxAge(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordPlan(ctx, "s1", "t1", "plan", PlanPayload{Subgoals: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	// Backdate the plan beyond the standard MaxAge (24h) but within
	// ExecutiveMaxAge (7d).
	older := time.Now().UTC().Add(-3 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := store.ExecContext(ctx,
		`UPDATE working_memory SET created_at = ? WHERE session_id = ? AND entry_type = ?`,
		older, "s1", EntryPlan,
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	mm.PersistWorkingMemory(ctx)
	mm.VetWorkingMemory(ctx, DefaultVetConfig())

	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.Plans) != 1 {
		t.Fatalf("expected executive plan to survive standard MaxAge cutoff, got %+v", state.Plans)
	}

	// Backdate beyond ExecutiveMaxAge; now it should be discarded.
	veryOld := time.Now().UTC().Add(-10 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := store.ExecContext(ctx,
		`UPDATE working_memory SET created_at = ? WHERE session_id = ? AND entry_type = ?`,
		veryOld, "s1", EntryPlan,
	); err != nil {
		t.Fatalf("backdate far: %v", err)
	}
	mm.PersistWorkingMemory(ctx)
	mm.VetWorkingMemory(ctx, DefaultVetConfig())

	state, err = mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.Plans) != 0 {
		t.Fatalf("expected executive plan discarded beyond ExecutiveMaxAge, got %+v", state.Plans)
	}
}

func TestLoadExecutiveState_DefaultsToLatestTask(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordPlan(ctx, "s1", "old", "old plan", PlanPayload{Subgoals: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	// Backdate the older plan in UTC so it sits firmly behind the fresh insert.
	// SQLite's default datetime('now') is UTC, so backdating must match.
	older := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := store.ExecContext(ctx,
		`UPDATE working_memory SET created_at = ? WHERE session_id = ? AND task_id = ?`,
		older, "s1", "old",
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if err := mm.RecordPlan(ctx, "s1", "new", "new plan", PlanPayload{Subgoals: []string{"x"}}); err != nil {
		t.Fatal(err)
	}

	state, err := mm.LoadExecutiveState(ctx, "s1", "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if state.TaskID != "new" {
		t.Fatalf("expected latest task=new, got %q", state.TaskID)
	}
}

func TestResolveQuestion_RemovesEntry(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordUnresolvedQuestion(ctx, "s1", "t1", "still open", UnresolvedQuestionPayload{}); err != nil {
		t.Fatal(err)
	}
	if err := mm.ResolveQuestion(ctx, "s1", "t1", ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	state, err := mm.LoadExecutiveState(ctx, "s1", "t1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.UnresolvedQuestions) != 0 {
		t.Fatalf("expected question resolved, got %+v", state.UnresolvedQuestions)
	}
}
