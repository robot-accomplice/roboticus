package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"roboticus/internal/core"
)

type countingExecutor struct {
	calls   int
	results []string
	failErr error
}

func (e *countingExecutor) RunLoop(_ context.Context, _ *Session) (string, int, error) {
	idx := e.calls
	e.calls++
	if e.failErr != nil && idx == 0 {
		return "", 0, e.failErr
	}
	if idx < len(e.results) {
		return e.results[idx], 1, nil
	}
	return "default response", 1, nil
}

// retryOnEmptyGuard rejects empty content with Retry: true (unlike EmptyResponseGuard which uses Retry: false).
type retryOnEmptyGuard struct{}

func (g *retryOnEmptyGuard) Name() string { return "retry_on_empty" }
func (g *retryOnEmptyGuard) Check(content string) GuardResult {
	if content == "" {
		return GuardResult{Passed: false, Content: content, Retry: true, Reason: "empty response"}
	}
	return GuardResult{Passed: true, Content: content}
}

func TestRetryPolicy_Defaults(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", p.MaxRetries)
	}
	if !p.InjectReason {
		t.Error("InjectReason should default to true")
	}
	if !p.PreserveChain {
		t.Error("PreserveChain should default to true")
	}
}

func TestGuardRetry_PassFirstTime(t *testing.T) {
	executor := &countingExecutor{results: []string{"good response"}}
	chain := NewGuardChain(&retryOnEmptyGuard{})

	content, turns, err := retryWithGuards(context.Background(), executor, NewSession("s", "a", "n"), chain, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "good response" {
		t.Errorf("content = %q, want %q", content, "good response")
	}
	if turns != 1 {
		t.Errorf("turns = %d, want 1", turns)
	}
	if executor.calls != 1 {
		t.Errorf("executor called %d times, want 1", executor.calls)
	}
}

func TestGuardRetry_FailThenPass(t *testing.T) {
	executor := &countingExecutor{results: []string{"", "good on retry"}}
	chain := NewGuardChain(&retryOnEmptyGuard{})

	content, _, err := retryWithGuards(context.Background(), executor, NewSession("s", "a", "n"), chain, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "good on retry" {
		t.Errorf("content = %q, want %q", content, "good on retry")
	}
	if executor.calls != 2 {
		t.Errorf("executor called %d times, want 2", executor.calls)
	}
}

func TestGuardRetry_ExhaustRetries(t *testing.T) {
	executor := &countingExecutor{results: []string{"", "", ""}}
	chain := NewGuardChain(&retryOnEmptyGuard{})

	_, _, err := retryWithGuards(context.Background(), executor, NewSession("s", "a", "n"), chain, DefaultRetryPolicy())
	if err == nil {
		t.Fatal("expected error when retries exhausted")
	}
	if !errors.Is(err, core.ErrGuardExhausted) {
		t.Errorf("expected ErrGuardExhausted, got: %v", err)
	}
	// 1 initial + 2 retries = 3 calls
	if executor.calls != 3 {
		t.Errorf("executor called %d times, want 3", executor.calls)
	}
}

func TestGuardRetry_ExecutorError(t *testing.T) {
	executor := &countingExecutor{failErr: errors.New("llm down")}
	chain := NewGuardChain()

	_, _, err := retryWithGuards(context.Background(), executor, NewSession("s", "a", "n"), chain, DefaultRetryPolicy())
	if err == nil {
		t.Fatal("expected executor error to propagate")
	}
}

func TestGuardRetry_NoGuards(t *testing.T) {
	executor := &countingExecutor{results: []string{"any content"}}

	content, _, err := retryWithGuards(context.Background(), executor, NewSession("s", "a", "n"), nil, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "any content" {
		t.Errorf("content = %q, want %q", content, "any content")
	}
}

type contextualRetryGuard struct{}

func (g *contextualRetryGuard) Name() string { return "contextual_retry" }
func (g *contextualRetryGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true, Content: content}
}
func (g *contextualRetryGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || !ctx.HasToolResult("fixer") {
		return GuardResult{Passed: false, Retry: true, Reason: "missing fixer"}
	}
	return GuardResult{Passed: true, Content: content}
}

type contextAwareExecutor struct{ calls int }

func (e *contextAwareExecutor) RunLoop(_ context.Context, sess *Session) (string, int, error) {
	e.calls++
	if e.calls > 1 {
		sess.AddToolResult("call-1", "fixer", "done", false)
	}
	return "answer", 1, nil
}

func TestGuardRetryDetailed_RebuildsContextAcrossRetry(t *testing.T) {
	executor := &contextAwareExecutor{}
	chain := NewGuardChain(&contextualRetryGuard{})
	sess := NewSession("s", "a", "n")
	sess.AddUserMessage("please fix this")

	result, err := retryWithGuardsDetailed(context.Background(), executor, sess, chain, DefaultRetryPolicy(), func() *GuardContext {
		return (&Pipeline{}).buildGuardContext(sess)
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.GuardRetried {
		t.Fatal("GuardRetried = false, want true")
	}
	if executor.calls != 2 {
		t.Fatalf("executor calls = %d, want 2", executor.calls)
	}
	if len(result.InitialGuardResult.Violations) != 1 || result.InitialGuardResult.Violations[0] != "contextual_retry" {
		t.Fatalf("initial violations = %v, want [contextual_retry]", result.InitialGuardResult.Violations)
	}
	if result.FinalGuardResult.RetryRequested || len(result.FinalGuardResult.Violations) != 0 {
		t.Fatalf("final guard result = %+v, want clean pass after retry", result.FinalGuardResult)
	}
}

type narrativeOnlyRetryGuard struct{}

func (g *narrativeOnlyRetryGuard) Name() string { return "non_repetition_v2" }
func (g *narrativeOnlyRetryGuard) Check(content string) GuardResult {
	return GuardResult{Passed: false, Retry: true, Reason: "response repeats previous assistant message"}
}

func TestGuardRetryDetailed_SuppressesNarrativeOnlyRetryAfterExecutionProgress(t *testing.T) {
	executor := &countingExecutor{results: []string{"Created the note."}}
	chain := NewGuardChain(&narrativeOnlyRetryGuard{})
	sess := NewSession("s", "a", "n")
	sess.AddUserMessage("create the note")
	sess.AddToolResult("call-1", "obsidian_write", "wrote 12 bytes", false)

	result, err := retryWithGuardsDetailed(context.Background(), executor, sess, chain, DefaultRetryPolicy(), func() *GuardContext {
		return (&Pipeline{}).buildGuardContext(sess)
	}, decideGuardRetryAfterProgress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GuardRetried {
		t.Fatal("GuardRetried = true, want false")
	}
	if !result.RetrySuppressed {
		t.Fatal("RetrySuppressed = false, want true")
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if result.RetrySuppressReason == "" {
		t.Fatal("RetrySuppressReason should explain why the retry was suppressed")
	}
}

type captureRetryInstructionExecutor struct {
	calls      int
	lastSystem string
}

func (e *captureRetryInstructionExecutor) RunLoop(_ context.Context, sess *Session) (string, int, error) {
	e.calls++
	messages := sess.Messages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "system" {
			e.lastSystem = messages[i].Content
			break
		}
	}
	return "I'll do that next.", 1, nil
}

type namedRetryGuard struct{ name string }

func (g namedRetryGuard) Name() string { return g.name }
func (g namedRetryGuard) Check(string) GuardResult {
	return GuardResult{Passed: false, Retry: true, Reason: "missing action evidence"}
}

func TestGuardRetryDetailed_UsesGuardSpecificRetryInstruction(t *testing.T) {
	executor := &captureRetryInstructionExecutor{}
	chain := NewGuardChain(namedRetryGuard{name: "task_deferral"})
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 1
	policy.ErrorOnExhaust = false
	sess := NewSession("s", "a", "n")
	sess.AddUserMessage("schedule the quiet ticker")

	_, err := retryWithGuardsDetailed(context.Background(), executor, sess, chain, policy, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executor.calls != 2 {
		t.Fatalf("executor calls = %d, want 2", executor.calls)
	}
	if !strings.Contains(executor.lastSystem, "Perform the requested action with the selected tools now") {
		t.Fatalf("retry instruction = %q, want task_deferral directive", executor.lastSystem)
	}
}
