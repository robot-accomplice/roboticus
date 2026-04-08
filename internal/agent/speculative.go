package agent

import (
	"context"
	"sync"
	"time"
)

type Branch struct {
	Name    string
	Execute func(ctx context.Context) (string, error)
}

type BranchResult struct {
	Name       string
	Content    string
	Error      error
	DurationMs int64
}

type SpeculativeExecutor struct {
	timeout     time.Duration
	slotMu      sync.Mutex
	activeSlots int
	maxSlots    int
}

// NewSpeculativeExecutor creates an executor with the given timeout and a
// default maximum of 4 concurrent speculation slots.
func NewSpeculativeExecutor(timeout time.Duration) *SpeculativeExecutor {
	return &SpeculativeExecutor{timeout: timeout, maxSlots: 4}
}

// NewSpeculativeExecutorWithSlots creates an executor with explicit slot limit.
func NewSpeculativeExecutorWithSlots(timeout time.Duration, maxSlots int) *SpeculativeExecutor {
	if maxSlots <= 0 {
		maxSlots = 4
	}
	return &SpeculativeExecutor{timeout: timeout, maxSlots: maxSlots}
}

func (se *SpeculativeExecutor) EvaluateBranches(ctx context.Context, branches []Branch) []BranchResult {
	if len(branches) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, se.timeout)
	defer cancel()

	results := make([]BranchResult, len(branches))
	var wg sync.WaitGroup

	for i, branch := range branches {
		wg.Add(1)
		go func(idx int, b Branch) {
			defer wg.Done()
			start := time.Now()
			content, err := b.Execute(ctx)
			results[idx] = BranchResult{
				Name:       b.Name,
				Content:    content,
				Error:      err,
				DurationMs: time.Since(start).Milliseconds(),
			}
		}(i, branch)
	}

	wg.Wait()
	return results
}

func (se *SpeculativeExecutor) BestResult(results []BranchResult) *BranchResult {
	var best *BranchResult
	for i := range results {
		if results[i].Error != nil {
			continue
		}
		if best == nil || len(results[i].Content) > len(best.Content) {
			best = &results[i]
		}
	}
	return best
}
