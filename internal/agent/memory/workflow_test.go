package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/testutil"
)

func TestRecordWorkflow_PersistsFullMetadata(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	workflow := Workflow{
		Name:          "release-canary",
		Steps:         []string{"drain traffic", "flip flag", "smoke test"},
		Preconditions: []string{"canary healthy", "legal approved"},
		ErrorModes:    []string{"5xx spike", "latency regression"},
		ContextTags:   []string{"release", "production"},
	}
	if err := mm.RecordWorkflow(ctx, workflow); err != nil {
		t.Fatalf("record workflow: %v", err)
	}

	got, err := mm.GetWorkflow(ctx, "release-canary")
	if err != nil || got == nil {
		t.Fatalf("get workflow: %v %v", got, err)
	}
	if len(got.Steps) != 3 || got.Steps[0] != "drain traffic" {
		t.Fatalf("expected steps persisted, got %+v", got.Steps)
	}
	if len(got.Preconditions) != 2 || got.Preconditions[1] != "legal approved" {
		t.Fatalf("expected preconditions persisted, got %+v", got.Preconditions)
	}
	if got.Category != WorkflowCategoryWorkflow {
		t.Fatalf("expected category=workflow, got %q", got.Category)
	}
	if got.Version != 1 {
		t.Fatalf("expected version=1 on fresh insert, got %d", got.Version)
	}
	if got.Confidence != 1.0 {
		t.Fatalf("expected default confidence=1.0, got %f", got.Confidence)
	}
}

func TestRecordWorkflow_UpdatePreservesCountersAndBumpsVersion(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:  "deploy",
		Steps: []string{"build", "push"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordWorkflowSuccess(ctx, "deploy", "session-1"); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:  "deploy",
		Steps: []string{"build", "push", "verify"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := mm.GetWorkflow(ctx, "deploy")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Version != 2 {
		t.Fatalf("expected version bumped to 2 on update, got %d", got.Version)
	}
	if got.SuccessCount != 1 {
		t.Fatalf("expected success_count preserved across revision, got %d", got.SuccessCount)
	}
	if len(got.Steps) != 3 {
		t.Fatalf("expected updated steps, got %+v", got.Steps)
	}
}

func TestRecordWorkflowSuccess_AppendsEvidenceUniquely(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordWorkflow(ctx, Workflow{Name: "deploy", Steps: []string{"build"}}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"s1", "s2", "s1"} {
		if err := mm.RecordWorkflowSuccess(ctx, "deploy", session); err != nil {
			t.Fatal(err)
		}
	}
	got, err := mm.GetWorkflow(ctx, "deploy")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.SuccessCount != 3 {
		t.Fatalf("expected success_count=3, got %d", got.SuccessCount)
	}
	// Evidence list deduplicates.
	if len(got.SuccessEvidence) != 2 {
		t.Fatalf("expected 2 unique evidence entries, got %+v", got.SuccessEvidence)
	}
}

func TestFindWorkflows_QuerySensitiveMatch(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:        "deploy-canary",
		Steps:       []string{"drain", "flip", "smoke"},
		ContextTags: []string{"rollout", "release"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:        "billing-reconcile",
		Steps:       []string{"pull invoices", "compare ledger"},
		ContextTags: []string{"accounting"},
	}); err != nil {
		t.Fatal(err)
	}

	found, err := mm.FindWorkflows(ctx, "rollout", 5)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(found) != 1 || found[0].Name != "deploy-canary" {
		t.Fatalf("expected deploy-canary match for rollout, got %+v", found)
	}

	all, err := mm.FindWorkflows(ctx, "", 5)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 workflows listed when query empty, got %d", len(all))
	}
}

func TestRetrieveProceduralMemory_PrefersWorkflowsOverToolStats(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	// A tool stat row would normally surface first under the legacy path.
	mm.recordToolStat(ctx, "deploy_cli", true)
	mm.recordToolStat(ctx, "deploy_cli", true)
	// Now register a real workflow the query should prefer.
	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:        "canary-deploy",
		Steps:       []string{"drain", "flip", "smoke"},
		ContextTags: []string{"deploy"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mm.RecordWorkflowSuccess(ctx, "canary-deploy", "session-1"); err != nil {
		t.Fatal(err)
	}

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	block := retriever.retrieveProceduralMemory(ctx, "deploy", RetrievalHybrid, 512)
	if !strings.Contains(block, "canary-deploy") {
		t.Fatalf("expected workflow surfaced in procedural block, got:\n%s", block)
	}
	// Workflow line must precede the tool-stat line.
	wfIdx := strings.Index(block, "canary-deploy")
	toolIdx := strings.Index(block, "deploy_cli")
	if wfIdx < 0 || (toolIdx >= 0 && wfIdx > toolIdx) {
		t.Fatalf("expected workflow to appear before tool stats, got:\n%s", block)
	}
}

func TestRetrieveProceduralMemory_FallsBackToToolStatsWhenNoWorkflow(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	mm.recordToolStat(ctx, "deploy_cli", true)
	mm.recordToolStat(ctx, "deploy_cli", true)

	retriever := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	block := retriever.retrieveProceduralMemory(ctx, "deploy", RetrievalHybrid, 512)
	if !strings.Contains(block, "deploy_cli") {
		t.Fatalf("expected fallback to tool stats when no workflow matches, got:\n%s", block)
	}
}

func TestConfidenceSyncAppliesWithMigration(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:  "flaky-workflow",
		Steps: []string{"try"},
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate a workflow with a poor track record.
	if _, err := store.ExecContext(ctx,
		`UPDATE procedural_memory SET success_count = 1, failure_count = 10 WHERE name = ?`,
		"flaky-workflow",
	); err != nil {
		t.Fatal(err)
	}

	// Apply the confidence sync rule the consolidation pipeline uses.
	res, err := store.ExecContext(ctx,
		`UPDATE procedural_memory SET confidence = 0.1
		  WHERE memory_state = 'active'
		    AND failure_count > success_count * 4
		    AND confidence > 0.1`,
	)
	if err != nil {
		t.Fatalf("confidence sync query failed: %v", err)
	}
	affected, _ := res.RowsAffected()
	if affected != 1 {
		t.Fatalf("expected confidence sync to affect 1 row, got %d", affected)
	}
	got, err := mm.GetWorkflow(ctx, "flaky-workflow")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Confidence > 0.11 {
		t.Fatalf("expected confidence floored to 0.1, got %f", got.Confidence)
	}
}
