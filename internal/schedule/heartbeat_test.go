package schedule

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"goboticus/internal/core"
)

// mockTask implements HeartbeatTask for testing.
type mockTask struct {
	kind      HeartbeatTaskKind
	callCount atomic.Int32
	result    TaskResult
}

func (m *mockTask) Kind() HeartbeatTaskKind { return m.kind }
func (m *mockTask) Run(_ context.Context, _ *TickContext) TaskResult {
	m.callCount.Add(1)
	return m.result
}

func TestNewHeartbeatDaemon(t *testing.T) {
	task := &mockTask{kind: TaskMemoryPrune, result: TaskResult{Success: true}}
	d := NewHeartbeatDaemon(30*time.Second, []HeartbeatTask{task})
	if d.interval != 30*time.Second {
		t.Fatalf("expected 30s, got %v", d.interval)
	}
	if d.originalInterval != 30*time.Second {
		t.Fatalf("expected originalInterval 30s, got %v", d.originalInterval)
	}
	if len(d.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(d.tasks))
	}
}

func TestHeartbeatDaemon_AdjustInterval(t *testing.T) {
	tests := []struct {
		name     string
		base     time.Duration
		tier     core.SurvivalTier
		expected time.Duration
	}{
		{"thriving keeps base", 30 * time.Second, core.SurvivalTierThriving, 30 * time.Second},
		{"growth keeps base", 30 * time.Second, core.SurvivalTierGrowth, 30 * time.Second},
		{"stable keeps base", 30 * time.Second, core.SurvivalTierStable, 30 * time.Second},
		{"survival doubles", 30 * time.Second, core.SurvivalTierSurvival, 60 * time.Second},
		{"dead 10x", 30 * time.Second, core.SurvivalTierDead, 5 * time.Minute},
		{"clamp min", 3 * time.Second, core.SurvivalTierThriving, 10 * time.Second},
		{"clamp max normal", 10 * time.Minute, core.SurvivalTierThriving, 5 * time.Minute},
		{"dead allows up to 1h", 10 * time.Minute, core.SurvivalTierDead, 1 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewHeartbeatDaemon(tt.base, nil)
			d.adjustInterval(tt.tier)
			if d.interval != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, d.interval)
			}
		})
	}
}

func TestHeartbeatDaemon_RunCancelledImmediately(t *testing.T) {
	task := &mockTask{kind: TaskCacheEvict, result: TaskResult{Success: true}}
	d := NewHeartbeatDaemon(10*time.Second, []HeartbeatTask{task})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		d.Run(ctx, func() *TickContext {
			return &TickContext{SurvivalTier: core.SurvivalTierStable, Timestamp: time.Now()}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestHeartbeatDaemon_TasksExecuted(t *testing.T) {
	task := &mockTask{kind: TaskMetricSnapshot, result: TaskResult{Success: true}}
	d := NewHeartbeatDaemon(20*time.Millisecond, []HeartbeatTask{task})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	d.Run(ctx, func() *TickContext {
		return &TickContext{SurvivalTier: core.SurvivalTierStable, Timestamp: time.Now()}
	})

	count := task.callCount.Load()
	if count == 0 {
		t.Fatal("expected task to be called at least once")
	}
}

func TestHeartbeatDaemon_WakeTrigger(t *testing.T) {
	task := &mockTask{
		kind:   TaskSurvivalCheck,
		result: TaskResult{Success: true, ShouldWake: true},
	}
	d := NewHeartbeatDaemon(20*time.Millisecond, []HeartbeatTask{task})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	d.Run(ctx, func() *TickContext {
		return &TickContext{SurvivalTier: core.SurvivalTierStable, Timestamp: time.Now()}
	})

	// ShouldWake causes an immediate re-run, so call count should be > number of ticks.
	count := task.callCount.Load()
	if count < 2 {
		t.Fatalf("expected wake to cause extra runs, got %d calls", count)
	}
}

func TestHeartbeatTaskKind_Constants(t *testing.T) {
	kinds := []HeartbeatTaskKind{
		TaskSurvivalCheck, TaskUSDCMonitor, TaskYield, TaskMemoryPrune,
		TaskCacheEvict, TaskMetricSnapshot, TaskAgentCardRefresh, TaskSessionGovernor,
	}
	seen := make(map[HeartbeatTaskKind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate kind: %s", k)
		}
		seen[k] = true
		if k == "" {
			t.Fatal("empty kind constant")
		}
	}
}

func TestTickContext_Fields(t *testing.T) {
	tc := &TickContext{
		CreditBalance: 100.5,
		USDCBalance:   50.0,
		SurvivalTier:  core.SurvivalTierGrowth,
		Timestamp:     time.Now(),
	}
	if tc.CreditBalance != 100.5 {
		t.Fatal("CreditBalance mismatch")
	}
	if tc.SurvivalTier != core.SurvivalTierGrowth {
		t.Fatal("SurvivalTier mismatch")
	}
}
