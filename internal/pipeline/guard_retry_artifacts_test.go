package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"roboticus/internal/session"
	"roboticus/testutil"
)

type retryAwareExecutor struct {
	calls int
}

func (e *retryAwareExecutor) RunLoop(_ context.Context, sess *session.Session) (string, int, error) {
	e.calls++
	if e.calls == 1 {
		return "draft", 1, nil
	}
	sess.AddToolResult("call-1", "fixer", "applied fix", false)
	return "The fixer tool result is available, and the revised answer is ready.", 1, nil
}

type fixerContextGuard struct{}

func (g *fixerContextGuard) Name() string { return "fixer_context" }

func (g *fixerContextGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true, Content: content}
}

func (g *fixerContextGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	hasFixer := ctx != nil && ctx.HasToolResult("fixer")
	switch {
	case content == "draft" && !hasFixer:
		return GuardResult{
			Passed:  false,
			Retry:   true,
			Reason:  "missing fixer evidence",
			Verdict: GuardRetryRequested,
		}
	case content == "The fixer tool result is available, and the revised answer is ready." && !hasFixer:
		return GuardResult{
			Passed:  false,
			Content: "blocked without fixer",
			Reason:  "missing fixer evidence",
			Verdict: GuardRewritten,
		}
	default:
		return GuardResult{Passed: true, Content: content}
	}
}

type rewriteTrackingGuard struct{}

func (g *rewriteTrackingGuard) Name() string { return "rewrite_tracking" }

func (g *rewriteTrackingGuard) Check(content string) GuardResult {
	if content == "raw" {
		return GuardResult{
			Passed:  false,
			Content: "clean",
			Reason:  "rewrote raw content",
			Verdict: GuardRewritten,
		}
	}
	return GuardResult{Passed: true, Content: content}
}

type staticExecutor struct {
	content string
}

func (e *staticExecutor) RunLoop(_ context.Context, _ *session.Session) (string, int, error) {
	return e.content, 1, nil
}

type promptEchoContextGuard struct{}

func (g *promptEchoContextGuard) Name() string { return "prompt_echo_context" }

func (g *promptEchoContextGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true, Content: content}
}

func (g *promptEchoContextGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx != nil && ctx.UserPrompt == "context please" {
		return GuardResult{
			Passed:  false,
			Content: "context-aware rewrite",
			Reason:  "used session-derived user prompt",
			Verdict: GuardRewritten,
		}
	}
	return GuardResult{Passed: true, Content: content}
}

func TestStandardInference_GuardRetryUsesFreshContext(t *testing.T) {
	store := testutil.TempStore(t)
	executor := &retryAwareExecutor{}
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: executor,
		Guards:   NewGuardChain(&fixerContextGuard{}),
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	cfg.PostTurnIngest = false
	cfg.NicknameRefinement = false

	outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content:   "say hello",
		AgentID:   "agent-1",
		AgentName: "TestBot",
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if executor.calls != 2 {
		t.Fatalf("executor calls = %d, want 2", executor.calls)
	}
	want := "The fixer tool result is available, and the revised answer is ready."
	if outcome.Content != want {
		t.Fatalf("outcome.Content = %q, want %q", outcome.Content, want)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	if trace.InferenceParams == nil {
		t.Fatal("InferenceParams = nil, want recorded guard metadata")
	}
	params, err := ParseInferenceParams(*trace.InferenceParams)
	if err != nil {
		t.Fatalf("ParseInferenceParams: %v", err)
	}
	if params == nil || !params.GuardRetried {
		t.Fatalf("GuardRetried = %v, want true", params != nil && params.GuardRetried)
	}
	if len(params.GuardViolations) != 0 {
		t.Fatalf("GuardViolations = %v, want none after successful retry", params.GuardViolations)
	}
}

func TestStandardInference_InferenceParamsCaptureAppliedGuardViolations(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &staticExecutor{content: "raw"},
		Guards:   NewGuardChain(&rewriteTrackingGuard{}),
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	cfg.PostTurnIngest = false
	cfg.NicknameRefinement = false

	outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content:   "rewrite this",
		AgentID:   "agent-1",
		AgentName: "TestBot",
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if outcome.Content != "clean" {
		t.Fatalf("outcome.Content = %q, want clean", outcome.Content)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	if trace.InferenceParams == nil {
		t.Fatal("InferenceParams = nil, want recorded guard metadata")
	}
	params, err := ParseInferenceParams(*trace.InferenceParams)
	if err != nil {
		t.Fatalf("ParseInferenceParams: %v", err)
	}
	if params == nil {
		t.Fatal("ParseInferenceParams returned nil")
	}
	if params.GuardRetried {
		t.Fatal("GuardRetried = true, want false")
	}
	if len(params.GuardViolations) != 1 || params.GuardViolations[0] != "rewrite_tracking" {
		t.Fatalf("GuardViolations = %v, want [rewrite_tracking]", params.GuardViolations)
	}

	var turnID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&turnID); err != nil {
		t.Fatalf("query turn id: %v", err)
	}
	var detailsJSON string
	if err := store.QueryRowContext(context.Background(),
		`SELECT COALESCE(details_json, '')
		   FROM turn_diagnostic_events
		  WHERE turn_id = ? AND event_type = 'guard_contract_evaluated'
		  ORDER BY seq DESC LIMIT 1`, turnID,
	).Scan(&detailsJSON); err != nil {
		t.Fatalf("query guard contract event: %v", err)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		t.Fatalf("unmarshal guard contract details: %v", err)
	}
	rawEvents, ok := details["contract_events"].([]any)
	if !ok || len(rawEvents) != 1 {
		t.Fatalf("contract_events = %#v, want one event", details["contract_events"])
	}
	event, ok := rawEvents[0].(map[string]any)
	if !ok {
		t.Fatalf("contract event type = %T, want object", rawEvents[0])
	}
	if event["contract_id"] != "rewrite_tracking" {
		t.Fatalf("contract_id = %v, want rewrite_tracking", event["contract_id"])
	}
	if event["recovery_action"] != "rewrite" {
		t.Fatalf("recovery_action = %v, want rewrite", event["recovery_action"])
	}
}

func TestGuardOutcome_UsesContextualGuardsWhenSessionAvailable(t *testing.T) {
	sess := session.New("s1", "agent-1", "TestBot")
	sess.AddUserMessage("context please")

	pipe := &Pipeline{guards: NewGuardChain(&promptEchoContextGuard{})}
	outcome := &Outcome{Content: "original"}
	result := pipe.guardOutcome(Config{GuardSet: GuardSetFull}, sess, outcome)
	if result == nil {
		t.Fatal("result = nil")
	}
	if result.Content != "context-aware rewrite" {
		t.Fatalf("result.Content = %q, want context-aware rewrite", result.Content)
	}
}

func TestCacheHit_UsesContextualGuardsWhenSessionAvailable(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &staticExecutor{content: "unused because cache should hit"},
		Guards:   NewGuardChain(&promptEchoContextGuard{}),
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	cfg.PostTurnIngest = false
	cfg.NicknameRefinement = false
	cfg.GuardSet = GuardSetNone
	cfg.CacheGuardSet = GuardSetCached

	prompt := "context please"
	cacheSess := session.New("", "agent-1", "TestBot")
	cacheSess.AddUserMessage(prompt)
	pipe.StoreInCacheForSession(context.Background(), cacheSess, cfg, prompt, "cached raw response with enough length", "cache-model")

	pc := &pipelineContext{
		cfg:     cfg,
		input:   Input{Content: prompt},
		session: cacheSess,
		content: prompt,
		msgID:   "msg-1",
		tr:      NewTraceRecorder(),
	}

	outcome, err := pipe.stageCacheCheck(context.Background(), pc)
	if err != nil {
		t.Fatalf("stageCacheCheck: %v", err)
	}
	if outcome == nil {
		t.Fatal("expected cache hit outcome")
	}
	if !outcome.FromCache {
		t.Fatal("expected cache hit")
	}
	if outcome.Content != "context-aware rewrite" {
		t.Fatalf("outcome.Content = %q, want context-aware rewrite", outcome.Content)
	}
}
