package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundWorker_Submit(t *testing.T) {
	w := NewBackgroundWorker(4)
	var called atomic.Bool
	w.Submit("test", func(ctx context.Context) {
		called.Store(true)
	})
	w.Drain(time.Second)
	if !called.Load() {
		t.Error("submitted function was not called")
	}
}

func TestBackgroundWorker_ConcurrentSubmit(t *testing.T) {
	w := NewBackgroundWorker(4)
	var counter atomic.Int64
	for i := 0; i < 100; i++ {
		w.Submit("count", func(ctx context.Context) {
			counter.Add(1)
		})
	}
	w.Drain(5 * time.Second)
	if counter.Load() != 100 {
		t.Errorf("counter = %d, want 100", counter.Load())
	}
}

func TestBackgroundWorker_DrainStopsNew(t *testing.T) {
	w := NewBackgroundWorker(1)
	var called atomic.Bool

	w.Drain(time.Second) // drain immediately

	// Submit after drain should be skipped.
	w.Submit("after-drain", func(ctx context.Context) {
		called.Store(true)
	})
	time.Sleep(50 * time.Millisecond)
	if called.Load() {
		t.Error("task submitted after Drain should not run")
	}
}

func TestBackgroundWorker_PanicRecovery(t *testing.T) {
	w := NewBackgroundWorker(4)
	var afterPanic atomic.Bool
	w.Submit("panicker", func(ctx context.Context) {
		panic("test panic")
	})
	w.Submit("after", func(ctx context.Context) {
		afterPanic.Store(true)
	})
	w.Drain(time.Second)
	if !afterPanic.Load() {
		t.Error("panic in one task should not prevent others from running")
	}
}
