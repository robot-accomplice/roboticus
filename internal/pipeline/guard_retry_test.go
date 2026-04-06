package pipeline

import (
	"context"
	"errors"
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
