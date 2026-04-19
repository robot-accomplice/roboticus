package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/testutil"
)

func seedWorkflow(t *testing.T, mm *agentmemory.Manager, w agentmemory.Workflow, successes []string, failures []string) {
	t.Helper()
	ctx := context.Background()
	if err := mm.RecordWorkflow(ctx, w); err != nil {
		t.Fatalf("seed %q: %v", w.Name, err)
	}
	for _, ev := range successes {
		if err := mm.RecordWorkflowSuccess(ctx, w.Name, ev); err != nil {
			t.Fatalf("record success on %q: %v", w.Name, err)
		}
	}
	for _, ev := range failures {
		if err := mm.RecordWorkflowFailure(ctx, w.Name, ev); err != nil {
			t.Fatalf("record failure on %q: %v", w.Name, err)
		}
	}
}

func TestRankWorkflowMatches_TagFitBeatsRawSuccessRate(t *testing.T) {
	now := time.Now().UTC()
	cands := []agentmemory.Workflow{
		{
			Name: "generic-deploy", Steps: []string{"build", "push"},
			Confidence: 0.9, SuccessCount: 20, FailureCount: 1,
			LastUsedAt: now,
		},
		{
			Name: "canary-release", Steps: []string{"drain", "flip", "smoke"},
			ContextTags: []string{"release", "canary", "production"},
			Confidence:  0.9, SuccessCount: 3, FailureCount: 0,
			LastUsedAt: now,
		},
	}
	ranked := rankWorkflowMatches("", []string{"canary", "release"}, cands)
	if len(ranked) == 0 {
		t.Fatal("expected matches")
	}
	if ranked[0].Workflow.Name != "canary-release" {
		t.Fatalf("expected tag-matched workflow first, got %q (scores: %+v)", ranked[0].Workflow.Name, ranked)
	}
}

func TestRankWorkflowMatches_LaplaceSmoothingPrefersLargerSample(t *testing.T) {
	// Two workflows with the same apparent success rate (100%), one with a
	// 1/1 sample and one with a 9/9 sample. The larger sample should win.
	now := time.Now().UTC()
	cands := []agentmemory.Workflow{
		{
			Name: "lucky-one", Steps: []string{"run"},
			Confidence: 0.9, SuccessCount: 1, FailureCount: 0,
			LastUsedAt: now,
		},
		{
			Name: "battle-tested", Steps: []string{"run"},
			Confidence: 0.9, SuccessCount: 9, FailureCount: 0,
			LastUsedAt: now,
		},
	}
	ranked := rankWorkflowMatches("", nil, cands)
	if len(ranked) < 2 {
		t.Fatalf("expected both workflows ranked, got %+v", ranked)
	}
	if ranked[0].Workflow.Name != "battle-tested" {
		t.Fatalf("expected larger sample to win under Laplace smoothing, got %q first", ranked[0].Workflow.Name)
	}
}

func TestRankWorkflowMatches_FailurePenaltyHurtsRiskyWorkflow(t *testing.T) {
	now := time.Now().UTC()
	cands := []agentmemory.Workflow{
		{
			Name: "clean-run", Steps: []string{"deploy"},
			Confidence: 0.9, SuccessCount: 5, FailureCount: 0,
			LastUsedAt: now,
		},
		{
			Name: "risky-run", Steps: []string{"deploy"},
			Confidence: 0.9, SuccessCount: 10, FailureCount: 2,
			LastUsedAt: now,
		},
	}
	ranked := rankWorkflowMatches("", nil, cands)
	if len(ranked) < 2 {
		t.Fatalf("expected both ranked, got %+v", ranked)
	}
	if ranked[0].Workflow.Name != "clean-run" {
		t.Fatalf("expected clean-run to outrank risky-run via failure penalty, got %q first", ranked[0].Workflow.Name)
	}
}

func TestRankWorkflowMatches_RecencyDecayWorks(t *testing.T) {
	now := time.Now().UTC()
	cands := []agentmemory.Workflow{
		{
			Name: "fresh", Steps: []string{"run"},
			Confidence: 0.9, SuccessCount: 3, FailureCount: 0,
			LastUsedAt: now,
		},
		{
			Name: "ancient", Steps: []string{"run"},
			Confidence: 0.9, SuccessCount: 3, FailureCount: 0,
			LastUsedAt: now.Add(-120 * 24 * time.Hour), // 120 days ago
		},
	}
	ranked := rankWorkflowMatches("", nil, cands)
	if ranked[0].Workflow.Name != "fresh" {
		t.Fatalf("expected fresh workflow ahead of ancient via recency decay, got %q first", ranked[0].Workflow.Name)
	}
}

func TestRankWorkflowMatches_DropsBelowFloor(t *testing.T) {
	// A workflow with no successes, no tags, no query fit, and floored
	// confidence should fall below the floor and be dropped.
	cands := []agentmemory.Workflow{
		{
			Name: "orphan", Steps: []string{"run"},
			Confidence:   0.1,
			SuccessCount: 0, FailureCount: 3,
		},
	}
	ranked := rankWorkflowMatches("", nil, cands)
	if len(ranked) != 0 {
		t.Fatalf("expected below-floor candidate to be dropped, got %+v", ranked)
	}
}

func TestRankWorkflowMatches_QueryTokenOverlapBoostsScore(t *testing.T) {
	now := time.Now().UTC()
	cands := []agentmemory.Workflow{
		{
			Name: "billing-reconciliation", Steps: []string{"pull", "compare"},
			ContextTags: []string{"accounting"},
			Confidence:  0.9, SuccessCount: 5, FailureCount: 0,
			LastUsedAt: now,
		},
		{
			Name: "generic-task", Steps: []string{"step-a", "step-b"},
			Confidence: 0.9, SuccessCount: 50, FailureCount: 0,
			LastUsedAt: now,
		},
	}
	// Query targets billing — even though generic-task has a better
	// track record, billing-reconciliation should win via query fit.
	ranked := rankWorkflowMatches("billing reconciliation", nil, cands)
	if ranked[0].Workflow.Name != "billing-reconciliation" {
		t.Fatalf("expected query fit to beat raw count, got %q first (ranked=%+v)", ranked[0].Workflow.Name, ranked)
	}
}

func TestTokenizeForRanking_DropsStopWordsAndShortTokens(t *testing.T) {
	toks := tokenizeForRanking("the billing of a refund")
	for _, tok := range toks {
		if tok == "the" || tok == "of" || tok == "a" {
			t.Fatalf("stop word leaked into tokens: %+v", toks)
		}
		if len(tok) < 3 {
			t.Fatalf("short token leaked: %q in %+v", tok, toks)
		}
	}
	if len(toks) == 0 || toks[0] != "billing" {
		t.Fatalf("expected meaningful tokens, got %+v", toks)
	}
}

func TestWorkflowSearchTool_FindReturnsRankedMatches(t *testing.T) {
	store := testutil.TempStore(t)
	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	seedWorkflow(t, mm, agentmemory.Workflow{
		Name: "canary-release", Steps: []string{"drain", "flip", "smoke"},
		ContextTags: []string{"release", "canary"}, Confidence: 0.9,
	}, []string{"sess-1", "sess-2", "sess-3"}, nil)
	seedWorkflow(t, mm, agentmemory.Workflow{
		Name: "billing-reconcile", Steps: []string{"pull", "compare"},
		ContextTags: []string{"accounting"}, Confidence: 0.9,
	}, []string{"sess-x"}, nil)

	tool := NewWorkflowSearchTool(store)
	res, err := tool.Execute(context.Background(),
		`{"operation":"find","query":"canary release","limit":3}`, &Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out workflowSearchResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	if len(out.Matches) == 0 {
		t.Fatalf("expected matches, got %s", res.Output)
	}
	if out.Matches[0].Name != "canary-release" {
		t.Fatalf("expected canary-release first, got %q", out.Matches[0].Name)
	}
	if out.Matches[0].SuccessRate == 0 {
		t.Fatalf("expected non-zero success rate on top match, got %+v", out.Matches[0])
	}
}

func TestWorkflowSearchTool_FindRespectsTagFilter(t *testing.T) {
	store := testutil.TempStore(t)
	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	seedWorkflow(t, mm, agentmemory.Workflow{
		Name: "release-canary", Steps: []string{"drain"},
		ContextTags: []string{"release", "canary"}, Confidence: 0.9,
	}, []string{"s1"}, nil)
	seedWorkflow(t, mm, agentmemory.Workflow{
		Name: "billing-reconcile", Steps: []string{"pull"},
		ContextTags: []string{"accounting"}, Confidence: 0.9,
	}, []string{"s1", "s2", "s3", "s4", "s5"}, nil)

	tool := NewWorkflowSearchTool(store)
	res, _ := tool.Execute(context.Background(),
		`{"operation":"find","tags":["release"]}`, &Context{})
	var out workflowSearchResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	if len(out.Matches) == 0 || out.Matches[0].Name != "release-canary" {
		t.Fatalf("expected release-canary first under tag filter, got %+v", out.Matches)
	}
}

func TestWorkflowSearchTool_GetFetchesByName(t *testing.T) {
	store := testutil.TempStore(t)
	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	seedWorkflow(t, mm, agentmemory.Workflow{
		Name: "exact-name", Steps: []string{"run"}, Confidence: 0.8,
	}, nil, nil)

	tool := NewWorkflowSearchTool(store)
	res, _ := tool.Execute(context.Background(),
		`{"operation":"get","name":"exact-name"}`, &Context{})
	var out workflowGetResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	if !out.Found || out.Workflow == nil || out.Workflow.Name != "exact-name" {
		t.Fatalf("expected Found workflow, got %+v", out)
	}

	// Non-existent name returns Found=false, not a Go error.
	missing, _ := tool.Execute(context.Background(),
		`{"operation":"get","name":"does-not-exist"}`, &Context{})
	var missingOut workflowGetResult
	if err := json.Unmarshal([]byte(missing.Output), &missingOut); err != nil {
		t.Fatal(err)
	}
	if missingOut.Found {
		t.Fatalf("expected Found=false for missing workflow, got %+v", missingOut)
	}
}

func TestWorkflowSearchTool_RejectsUnknownOperation(t *testing.T) {
	tool := NewWorkflowSearchTool(testutil.TempStore(t))
	res, _ := tool.Execute(context.Background(), `{"operation":"summon"}`, &Context{})
	if !strings.Contains(res.Output, "unknown operation") {
		t.Fatalf("expected unknown-operation message, got %q", res.Output)
	}
}

func TestWorkflowSearchTool_NilStoreReturnsFriendlyMessage(t *testing.T) {
	tool := NewWorkflowSearchTool(nil)
	res, _ := tool.Execute(context.Background(), `{"operation":"find","query":"x"}`, &Context{})
	if !strings.Contains(res.Output, "not available") {
		t.Fatalf("expected friendly unavailability message, got %q", res.Output)
	}
}

func TestWorkflowSearchTool_ParameterSchemaIsValidJSON(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(NewWorkflowSearchTool(nil).ParameterSchema(), &schema); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("expected schema type=object, got %v", schema["type"])
	}
}
