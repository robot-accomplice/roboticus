package pipeline_test

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/core"
	"roboticus/internal/pipeline"
	"roboticus/internal/session"

	"roboticus/testutil"
)

func TestGuardContextRetry_StandardInference(t *testing.T) {
	store := testutil.TempStore(t)

	callCount := 0
	executor := &mockToolExecutor{
		RunLoopFunc: func(ctx context.Context, sess *session.Session) (string, int, error) {
			callCount++
			if callCount == 1 {
				// First call returns repetitive content that triggers retry.
				return "", 1, nil
			}
			return "good response on retry", 1, nil
		},
	}

	// Use a guard chain that requests retry on empty content.
	retryGuard := &emptyRetryGuard{}
	guards := pipeline.NewGuardChain(retryGuard)

	bgw := core.NewBackgroundWorker(4)
	defer bgw.Drain(2 * time.Second)

	p := pipeline.New(pipeline.PipelineDeps{
		Store:    store,
		Executor: executor,
		Guards:   guards,
		BGWorker: bgw,
	})

	cfg := pipeline.PresetAPI()
	cfg.PostTurnIngest = false // disable for test
	cfg.NicknameRefinement = false

	input := pipeline.Input{
		Content:   "test message",
		AgentID:   "agent-1",
		AgentName: "TestBot",
	}

	outcome, err := pipeline.RunPipeline(context.Background(), p, cfg, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The executor should have been called twice (original + retry).
	if callCount != 2 {
		t.Errorf("executor called %d times, want 2", callCount)
	}

	if outcome.Content != "good response on retry" {
		t.Errorf("content = %q, want %q", outcome.Content, "good response on retry")
	}
}

// emptyRetryGuard requests a retry when content is empty.
type emptyRetryGuard struct{}

func (g *emptyRetryGuard) Name() string { return "empty_retry" }
func (g *emptyRetryGuard) Check(content string) pipeline.GuardResult {
	if content == "" {
		return pipeline.GuardResult{
			Passed: false,
			Retry:  true,
			Reason: "empty content needs retry",
		}
	}
	return pipeline.GuardResult{Passed: true, Content: content}
}
