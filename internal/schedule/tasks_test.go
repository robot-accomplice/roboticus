package schedule

import (
	"context"
	"testing"
)

func TestMemoryLoopTask_Run_UsesConsolidationFunc(t *testing.T) {
	called := false
	task := &MemoryLoopTask{
		Consolidate: func(ctx context.Context, force bool) string {
			called = true
			if force {
				t.Fatal("force should be false on heartbeat consolidation")
			}
			return "indexed=1 deduped=2 promoted=3 pruned=4"
		},
	}

	result := task.Run(context.Background(), &TickContext{})
	if !called {
		t.Fatal("consolidation function was not called")
	}
	if !result.Success {
		t.Fatal("task should succeed")
	}
	if result.Message != "indexed=1 deduped=2 promoted=3 pruned=4" {
		t.Fatalf("message = %q", result.Message)
	}
}
