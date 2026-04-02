package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSpeculativeExecutor_ParallelExecution(t *testing.T) {
	se := NewSpeculativeExecutor(5 * time.Second)

	branches := []Branch{
		{
			Name: "branch-a",
			Execute: func(ctx context.Context) (string, error) {
				return "result from A", nil
			},
		},
		{
			Name: "branch-b",
			Execute: func(ctx context.Context) (string, error) {
				return "result from B", nil
			},
		},
	}

	results := se.EvaluateBranches(context.Background(), branches)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify both completed without error
	for _, r := range results {
		if r.Error != nil {
			t.Errorf("unexpected error in branch %s: %v", r.Name, r.Error)
		}
		if r.Content == "" {
			t.Errorf("expected non-empty content for branch %s", r.Name)
		}
	}
}

func TestSpeculativeExecutor_TimeoutHandling(t *testing.T) {
	se := NewSpeculativeExecutor(50 * time.Millisecond)

	branches := []Branch{
		{
			Name: "slow-branch",
			Execute: func(ctx context.Context) (string, error) {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(1 * time.Second):
					return "late result", nil
				}
			},
		},
		{
			Name: "fast-branch",
			Execute: func(ctx context.Context) (string, error) {
				return "fast result", nil
			},
		},
	}

	results := se.EvaluateBranches(context.Background(), branches)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// slow branch should have an error (context cancelled/timeout)
	var slowResult, fastResult *BranchResult
	for i := range results {
		if results[i].Name == "slow-branch" {
			slowResult = &results[i]
		} else if results[i].Name == "fast-branch" {
			fastResult = &results[i]
		}
	}

	if slowResult == nil || fastResult == nil {
		t.Fatal("missing expected branch results")
	}
	if slowResult.Error == nil {
		t.Error("expected slow branch to have context error")
	}
	if fastResult.Error != nil {
		t.Errorf("expected fast branch to succeed, got: %v", fastResult.Error)
	}
}

func TestSpeculativeExecutor_BestResult(t *testing.T) {
	se := NewSpeculativeExecutor(5 * time.Second)
	results := []BranchResult{
		{Name: "short", Content: "hi", Error: nil},
		{Name: "long", Content: "this is a much longer response", Error: nil},
		{Name: "error", Content: "", Error: errors.New("failed")},
	}

	best := se.BestResult(results)
	if best == nil {
		t.Fatal("expected a best result")
	}
	if best.Name != "long" {
		t.Errorf("expected 'long' to be the best result, got %q", best.Name)
	}
}

func TestSpeculativeExecutor_EmptyBranches(t *testing.T) {
	se := NewSpeculativeExecutor(5 * time.Second)
	results := se.EvaluateBranches(context.Background(), nil)
	if results != nil {
		t.Errorf("expected nil results for empty branches, got %v", results)
	}
}

func TestSpeculativeExecutor_AllErrors_ReturnsNil(t *testing.T) {
	se := NewSpeculativeExecutor(5 * time.Second)
	results := []BranchResult{
		{Name: "err1", Content: "", Error: errors.New("fail 1")},
		{Name: "err2", Content: "", Error: errors.New("fail 2")},
	}
	best := se.BestResult(results)
	if best != nil {
		t.Errorf("expected nil when all results have errors, got %+v", best)
	}
}

func TestSpeculativeExecutor_DurationTracked(t *testing.T) {
	se := NewSpeculativeExecutor(5 * time.Second)
	branches := []Branch{
		{
			Name: "timed-branch",
			Execute: func(ctx context.Context) (string, error) {
				time.Sleep(10 * time.Millisecond)
				return "done", nil
			},
		},
	}
	results := se.EvaluateBranches(context.Background(), branches)
	if len(results) != 1 {
		t.Fatalf("expected 1 result")
	}
	if results[0].DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", results[0].DurationMs)
	}
}
