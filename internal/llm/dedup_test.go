package llm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDedup_CollapsesConcurrentCalls(t *testing.T) {
	d := NewDedup(1 * time.Second)

	var callCount atomic.Int32

	fn := func() (*Response, error) {
		callCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		return &Response{Content: "hello"}, nil
	}

	var wg sync.WaitGroup
	results := make([]*Response, 10)
	errs := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = d.Do(context.Background(), "same-key", fn)
		}(i)
	}

	wg.Wait()

	// Should have only called fn once.
	if callCount.Load() != 1 {
		t.Errorf("expected 1 call, got %d", callCount.Load())
	}

	// All results should be the same.
	for i := 0; i < 10; i++ {
		if errs[i] != nil {
			t.Errorf("result %d: unexpected error: %v", i, errs[i])
		}
		if results[i] == nil || results[i].Content != "hello" {
			t.Errorf("result %d: expected 'hello', got %v", i, results[i])
		}
	}
}

func TestDedup_DifferentKeysRunSeparately(t *testing.T) {
	d := NewDedup(1 * time.Second)

	var callCount atomic.Int32

	fn := func() (*Response, error) {
		callCount.Add(1)
		return &Response{Content: "ok"}, nil
	}

	d.Do(context.Background(), "key-1", fn)
	d.Do(context.Background(), "key-2", fn)

	if callCount.Load() != 2 {
		t.Errorf("different keys should run separately, got %d calls", callCount.Load())
	}
}
