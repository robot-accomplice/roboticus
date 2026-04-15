package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/testutil"
)

func TestAssembleContext_FullStructure(t *testing.T) {
	evidence := []Evidence{
		{Content: "deploy requires rolling updates", SourceTier: TierSemantic, Score: 0.87, IsCanonical: true, AuthorityScore: 0.95, SourceLabel: "policy/deploy"},
		{Content: "server crashed during deploy", SourceTier: TierEpisodic, Score: 0.92},
		{Content: "deploy-to-prod: 4 runs", SourceTier: TierProcedural, Score: 0.71, AgeDays: 45},
	}

	ac := AssembleContext(nil, nil, "", evidence, "- goal: finish deployment", "- [14:32] checked logs")

	if ac.WorkingState == "" {
		t.Error("working state should be populated")
	}
	if ac.Evidence == "" {
		t.Error("evidence should be populated")
	}

	formatted := ac.Format()
	if !strings.Contains(formatted, "[Working State]") {
		t.Error("formatted should contain [Working State]")
	}
	if !strings.Contains(formatted, "[Retrieved Evidence]") {
		t.Error("formatted should contain [Retrieved Evidence]")
	}
	if !strings.Contains(formatted, "canonical") {
		t.Error("canonical flag should appear in evidence")
	}
	if !strings.Contains(formatted, "source=policy/deploy") {
		t.Error("source label should appear in evidence")
	}
	if !strings.Contains(formatted, "authority=") {
		t.Error("authority score should appear in evidence")
	}
	if !strings.Contains(formatted, "[Freshness Risks]") {
		t.Error("freshness risks should appear in evidence for stale entries")
	}
	if !strings.Contains(formatted, "age=45d") {
		t.Error("evidence metadata should include age")
	}
}

func TestAssembleContext_DetectsGaps(t *testing.T) {
	// Only semantic evidence — episodic, procedural, relationship are gaps.
	evidence := []Evidence{
		{Content: "some fact", SourceTier: TierSemantic, Score: 0.8},
	}

	ac := AssembleContext(nil, nil, "", evidence, "", "")

	if ac.Gaps == "" {
		t.Error("gaps should be detected when tiers are missing")
	}
	if !strings.Contains(ac.Gaps, "No past experiences") {
		t.Error("should flag missing episodic tier")
	}
	if !strings.Contains(ac.Gaps, "No relevant procedures") {
		t.Error("should flag missing procedural tier")
	}
}

func TestAssembleContext_NoGapsWhenAllTiersPresent(t *testing.T) {
	evidence := []Evidence{
		{Content: "fact", SourceTier: TierSemantic, Score: 0.8},
		{Content: "event", SourceTier: TierEpisodic, Score: 0.7},
		{Content: "workflow", SourceTier: TierProcedural, Score: 0.6},
		{Content: "entity", SourceTier: TierRelationship, Score: 0.5},
	}

	ac := AssembleContext(nil, nil, "", evidence, "", "")

	if ac.Gaps != "" {
		t.Errorf("no gaps expected when all tiers present, got: %s", ac.Gaps)
	}
}

func TestAssembleContext_EmptyEvidence(t *testing.T) {
	ac := AssembleContext(nil, nil, "", nil, "", "")

	if ac.Evidence != "" {
		t.Error("evidence should be empty")
	}
	if !strings.Contains(ac.Gaps, "No evidence retrieved") {
		t.Error("should flag that no evidence was retrieved at all")
	}
}

func TestAssembleContext_EmptyFormat(t *testing.T) {
	ac := &AssembledContext{}
	if formatted := ac.Format(); formatted != "" {
		t.Errorf("empty context should format to empty string, got %q", formatted)
	}
}

func TestAssembleContext_SurfacesExecutiveStateFromStore(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordPlan(ctx, "s-exec", "t-1", "investigate auth outage", PlanPayload{
		Subgoals: []string{"root cause", "remediation"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordUnresolvedQuestion(ctx, "s-exec", "t-1", "is rollout blocked by legal?", UnresolvedQuestionPayload{}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordStoppingCriteria(ctx, "s-exec", "t-1", "ship a PR with tests", StoppingCriteriaPayload{Condition: "all tests green"}); err != nil {
		t.Fatal(err)
	}

	ac := AssembleContext(ctx, store, "s-exec",
		[]Evidence{{Content: "deploy doc", SourceTier: TierSemantic, Score: 0.9}},
		"", "",
	)
	formatted := ac.Format()

	if !strings.Contains(formatted, "Executive State:") {
		t.Fatalf("expected executive state header, got %q", formatted)
	}
	if !strings.Contains(formatted, "is rollout blocked by legal?") {
		t.Fatalf("expected unresolved question surfaced, got %q", formatted)
	}
	if !strings.Contains(formatted, "Stopping criteria:") {
		t.Fatalf("expected stopping criteria surfaced, got %q", formatted)
	}
}

func TestAssembleContext_ContradictionDetection(t *testing.T) {
	// Large score spread within a tier with 3+ entries → potential contradiction.
	evidence := []Evidence{
		{Content: "strong match", SourceTier: TierSemantic, Score: 0.95},
		{Content: "medium match", SourceTier: TierSemantic, Score: 0.6},
		{Content: "weak match", SourceTier: TierSemantic, Score: 0.3},
	}

	ac := AssembleContext(nil, nil, "", evidence, "", "")

	if ac.Contradictions == "" {
		t.Error("should detect contradiction signal from high score spread")
	}
}

func TestAssembleContext_NoContradictionHealthySpread(t *testing.T) {
	evidence := []Evidence{
		{Content: "a", SourceTier: TierSemantic, Score: 0.8},
		{Content: "b", SourceTier: TierSemantic, Score: 0.7},
		{Content: "c", SourceTier: TierSemantic, Score: 0.6},
	}

	ac := AssembleContext(nil, nil, "", evidence, "", "")

	if ac.Contradictions != "" {
		t.Errorf("healthy spread should not trigger contradiction, got: %s", ac.Contradictions)
	}
}

func TestAssembleContext_WorkingStateOnly(t *testing.T) {
	ac := AssembleContext(nil, nil, "", nil, "- goal: test", "")

	formatted := ac.Format()
	if !strings.Contains(formatted, "[Working State]") {
		t.Error("should contain working state even with no evidence")
	}
	if !strings.Contains(formatted, "goal: test") {
		t.Error("should contain the goal")
	}
}

func TestAssembleContext_DetectsFreshnessRisk(t *testing.T) {
	evidence := []Evidence{
		{Content: "old policy", SourceTier: TierSemantic, Score: 0.8, AgeDays: 91},
	}

	ac := AssembleContext(nil, nil, "", evidence, "", "")

	if ac.FreshnessRisks == "" {
		t.Fatal("expected freshness risks for stale evidence")
	}
	if !strings.Contains(ac.FreshnessRisks, "91 days") {
		t.Fatalf("expected age in freshness risk, got %q", ac.FreshnessRisks)
	}
}
