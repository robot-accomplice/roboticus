package llm

import (
	"testing"
	"time"
)

func TestCapacityTracker_UnknownProviderAvailable(t *testing.T) {
	ct := NewCapacityTracker(1000, 10)
	if !ct.Available("unknown") {
		t.Error("unknown provider should be available")
	}
}

func TestCapacityTracker_RecordAndUsage(t *testing.T) {
	ct := NewCapacityTracker(10000, 100)
	ct.Record("openai", 100)
	ct.Record("openai", 200)

	rpm, tpm := ct.Usage("openai")
	if rpm != 2 {
		t.Errorf("expected rpm=2, got %d", rpm)
	}
	if tpm != 300 {
		t.Errorf("expected tpm=300, got %d", tpm)
	}
}

func TestCapacityTracker_RPMLimit(t *testing.T) {
	ct := NewCapacityTracker(0, 3) // rpm=3, no tpm limit

	ct.Record("openai", 10)
	ct.Record("openai", 10)
	ct.Record("openai", 10)

	// At limit now.
	if ct.Available("openai") {
		t.Error("should be unavailable at RPM limit")
	}
}

func TestCapacityTracker_TPMLimit(t *testing.T) {
	ct := NewCapacityTracker(100, 0) // tpm=100, no rpm limit

	ct.Record("openai", 60)
	ct.Record("openai", 50) // total = 110 > 100

	if ct.Available("openai") {
		t.Error("should be unavailable at TPM limit")
	}
}

func TestCapacityTracker_BelowLimits(t *testing.T) {
	ct := NewCapacityTracker(1000, 10)
	ct.Record("openai", 100)
	ct.Record("openai", 100)

	if !ct.Available("openai") {
		t.Error("should be available when below limits")
	}
}

func TestCapacityTracker_PrunesOldEntries(t *testing.T) {
	ct := NewCapacityTracker(0, 2) // rpm=2

	// Manually inject an old entry (more than 1 minute ago).
	ct.mu.Lock()
	w := &slidingWindow{}
	w.entries = append(w.entries, windowEntry{
		timestamp: time.Now().Add(-2 * time.Minute), // old, should be pruned
		tokens:    100,
	})
	ct.windows["openai"] = w
	ct.mu.Unlock()

	// Add one fresh entry.
	ct.Record("openai", 50)
	ct.Record("openai", 50)

	// rpm=2, we have 2 fresh entries → at limit.
	if ct.Available("openai") {
		t.Error("should be at RPM limit with 2 fresh entries")
	}

	// Usage should reflect only fresh entries (not the pruned one).
	rpm, _ := ct.Usage("openai")
	if rpm != 2 {
		t.Errorf("expected rpm=2 after pruning, got %d", rpm)
	}
}

func TestCapacityTracker_UsageUnknownProvider(t *testing.T) {
	ct := NewCapacityTracker(1000, 10)
	rpm, tpm := ct.Usage("nonexistent")
	if rpm != 0 || tpm != 0 {
		t.Errorf("expected (0,0) for unknown provider, got (%d,%d)", rpm, tpm)
	}
}

func TestCapacityTracker_MultipleProviders(t *testing.T) {
	ct := NewCapacityTracker(100, 5)

	// Fill provider A to limit.
	for i := 0; i < 5; i++ {
		ct.Record("providerA", 10)
	}

	// Provider B should still be available.
	if !ct.Available("providerB") {
		t.Error("providerB should still be available")
	}
	// Provider A should be at limit.
	if ct.Available("providerA") {
		t.Error("providerA should be at RPM limit")
	}
}
