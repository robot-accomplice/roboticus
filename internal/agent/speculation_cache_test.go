package agent

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// --- #68: SpeculationCache ---

func TestSpeculationCache_PutAndGet(t *testing.T) {
	sc := NewSpeculationCache(10)
	key := SpeculationKey{ContentHash: "abc123", ToolNames: []string{"echo", "read"}}
	result := &BranchResult{Name: "branch-a", Content: "hello", DurationMs: 50}

	sc.Put(key, result)

	got, ok := sc.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Name != "branch-a" || got.Content != "hello" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestSpeculationCache_Miss(t *testing.T) {
	sc := NewSpeculationCache(10)
	key := SpeculationKey{ContentHash: "missing", ToolNames: []string{"nope"}}

	got, ok := sc.Get(key)
	if ok {
		t.Errorf("expected cache miss, got %+v", got)
	}
}

func TestSpeculationCache_Eviction(t *testing.T) {
	sc := NewSpeculationCache(2)

	key1 := SpeculationKey{ContentHash: "hash1", ToolNames: []string{"a"}}
	key2 := SpeculationKey{ContentHash: "hash2", ToolNames: []string{"b"}}
	key3 := SpeculationKey{ContentHash: "hash3", ToolNames: []string{"c"}}

	sc.Put(key1, &BranchResult{Name: "r1"})
	sc.Put(key2, &BranchResult{Name: "r2"})

	if sc.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", sc.Len())
	}

	// Adding a third should evict the oldest (key1).
	sc.Put(key3, &BranchResult{Name: "r3"})
	if sc.Len() != 2 {
		t.Fatalf("expected 2 entries after eviction, got %d", sc.Len())
	}

	_, ok := sc.Get(key1)
	if ok {
		t.Error("key1 should have been evicted")
	}
	_, ok = sc.Get(key2)
	if !ok {
		t.Error("key2 should still be present")
	}
	_, ok = sc.Get(key3)
	if !ok {
		t.Error("key3 should be present")
	}
}

func TestSpeculationCache_UpdateExisting(t *testing.T) {
	sc := NewSpeculationCache(10)
	key := SpeculationKey{ContentHash: "same", ToolNames: []string{"x"}}

	sc.Put(key, &BranchResult{Name: "v1", Content: "old"})
	sc.Put(key, &BranchResult{Name: "v2", Content: "new"})

	if sc.Len() != 1 {
		t.Fatalf("expected 1 entry after update, got %d", sc.Len())
	}

	got, ok := sc.Get(key)
	if !ok {
		t.Fatal("expected cache hit after update")
	}
	if got.Content != "new" {
		t.Errorf("expected updated content 'new', got %q", got.Content)
	}
}

func TestSpeculationCache_ToolNameOrderIndependent(t *testing.T) {
	sc := NewSpeculationCache(10)

	key1 := SpeculationKey{ContentHash: "hash", ToolNames: []string{"b", "a"}}
	key2 := SpeculationKey{ContentHash: "hash", ToolNames: []string{"a", "b"}}

	sc.Put(key1, &BranchResult{Name: "result"})

	got, ok := sc.Get(key2)
	if !ok {
		t.Fatal("tool name order should not matter for cache key")
	}
	if got.Name != "result" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestSpeculationCache_ReturnsCopy(t *testing.T) {
	sc := NewSpeculationCache(10)
	key := SpeculationKey{ContentHash: "hash", ToolNames: []string{"x"}}

	sc.Put(key, &BranchResult{Name: "original", Content: "data"})

	got, _ := sc.Get(key)
	got.Content = "mutated"

	// Original should be unchanged.
	got2, _ := sc.Get(key)
	if got2.Content != "data" {
		t.Errorf("cache should return copies; got mutated content %q", got2.Content)
	}
}

func TestSpeculationCache_DefaultMaxSize(t *testing.T) {
	sc := NewSpeculationCache(0) // should default to 256
	if sc.maxSize != 256 {
		t.Errorf("expected default maxSize 256, got %d", sc.maxSize)
	}
}

func TestSpeculationCache_ConcurrentAccess(t *testing.T) {
	sc := NewSpeculationCache(100)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := SpeculationKey{ContentHash: string(rune('a' + n%26)), ToolNames: []string{"t"}}
			sc.Put(key, &BranchResult{Name: "r"})
			sc.Get(key)
		}(i)
	}
	wg.Wait()

	// No panic = pass. Just verify non-negative length.
	if sc.Len() < 0 {
		t.Error("negative length")
	}
}

// --- #69: Speculation Slot Guard ---

func TestSpeculationSlotGuard_AcquireAndRelease(t *testing.T) {
	se := NewSpeculativeExecutorWithSlots(5*time.Second, 2)

	guard1, err := se.AcquireSlot()
	if err != nil {
		t.Fatalf("failed to acquire slot 1: %v", err)
	}
	if se.ActiveSlots() != 1 {
		t.Errorf("expected 1 active slot, got %d", se.ActiveSlots())
	}

	guard2, err := se.AcquireSlot()
	if err != nil {
		t.Fatalf("failed to acquire slot 2: %v", err)
	}
	if se.ActiveSlots() != 2 {
		t.Errorf("expected 2 active slots, got %d", se.ActiveSlots())
	}

	// Third acquire should fail.
	_, err = se.AcquireSlot()
	if err == nil {
		t.Error("expected error when all slots in use")
	}
	if !errors.Is(err, ErrNoSlotAvailable) {
		t.Errorf("expected ErrNoSlotAvailable, got %v", err)
	}

	// Release one and try again.
	guard1.Release()
	if se.ActiveSlots() != 1 {
		t.Errorf("expected 1 active slot after release, got %d", se.ActiveSlots())
	}

	guard3, err := se.AcquireSlot()
	if err != nil {
		t.Fatalf("failed to acquire slot after release: %v", err)
	}

	guard2.Release()
	guard3.Release()
	if se.ActiveSlots() != 0 {
		t.Errorf("expected 0 active slots, got %d", se.ActiveSlots())
	}
}

func TestSpeculationSlotGuard_DoubleRelease(t *testing.T) {
	se := NewSpeculativeExecutorWithSlots(5*time.Second, 2)

	guard, _ := se.AcquireSlot()
	guard.Release()
	guard.Release() // should be a no-op

	if se.ActiveSlots() != 0 {
		t.Errorf("double release should not decrement below 0, got %d", se.ActiveSlots())
	}
}

func TestSpeculationSlotGuard_DefaultSlots(t *testing.T) {
	se := NewSpeculativeExecutor(5 * time.Second)

	// Default is 4 slots.
	for i := 0; i < 4; i++ {
		_, err := se.AcquireSlot()
		if err != nil {
			t.Fatalf("failed to acquire slot %d: %v", i+1, err)
		}
	}

	_, err := se.AcquireSlot()
	if err == nil {
		t.Error("expected error after 4 slots exhausted")
	}
}

func TestSpeculationSlotGuard_ConcurrentAcquire(t *testing.T) {
	se := NewSpeculativeExecutorWithSlots(5*time.Second, 10)
	var wg sync.WaitGroup
	acquired := make(chan *SpeculationSlotGuard, 20)
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			guard, err := se.AcquireSlot()
			if err != nil {
				errs <- err
				return
			}
			acquired <- guard
		}()
	}
	wg.Wait()
	close(acquired)
	close(errs)

	successCount := 0
	for guard := range acquired {
		successCount++
		guard.Release()
	}
	failCount := 0
	for range errs {
		failCount++
	}

	if successCount != 10 {
		t.Errorf("expected exactly 10 successful acquires, got %d", successCount)
	}
	if failCount != 10 {
		t.Errorf("expected exactly 10 failures, got %d", failCount)
	}
}
