package llm

import (
	"math"
	"sync"
	"testing"
	"time"
)

// ---------- backward-compatible tests ----------

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
	ct := NewCapacityTracker(0, 3)
	ct.Record("openai", 10)
	ct.Record("openai", 10)
	ct.Record("openai", 10)

	if ct.Available("openai") {
		t.Error("should be unavailable at RPM limit")
	}
}

func TestCapacityTracker_TPMLimit(t *testing.T) {
	ct := NewCapacityTracker(100, 0)
	ct.Record("openai", 60)
	ct.Record("openai", 50)

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

func TestCapacityTracker_UsageUnknownProvider(t *testing.T) {
	ct := NewCapacityTracker(1000, 10)
	rpm, tpm := ct.Usage("nonexistent")
	if rpm != 0 || tpm != 0 {
		t.Errorf("expected (0,0) for unknown provider, got (%d,%d)", rpm, tpm)
	}
}

func TestCapacityTracker_MultipleProviders(t *testing.T) {
	ct := NewCapacityTracker(100, 5)
	for i := 0; i < 5; i++ {
		ct.Record("providerA", 10)
	}

	if !ct.Available("providerB") {
		t.Error("providerB should still be available")
	}
	if ct.Available("providerA") {
		t.Error("providerA should be at RPM limit")
	}
}

// ---------- per-provider registration ----------

func TestCapacityTracker_Register(t *testing.T) {
	ct := NewCapacityTracker(100, 10) // defaults
	ct.Register("fast", 5000, 50)
	ct.Register("slow", 500, 5)

	// Fill slow to its custom RPM limit.
	for i := 0; i < 5; i++ {
		ct.Record("slow", 10)
	}
	if ct.Available("slow") {
		t.Error("slow should be at its registered RPM limit of 5")
	}

	// fast should still have room (50 RPM).
	for i := 0; i < 5; i++ {
		ct.Record("fast", 10)
	}
	if !ct.Available("fast") {
		t.Error("fast should be available; only 5 of 50 RPM used")
	}
}

func TestCapacityTracker_RegisterOverridesDefaults(t *testing.T) {
	ct := NewCapacityTracker(100, 5)
	ct.Register("big", 100000, 1000)

	// Record 6 events -- exceeds default RPM (5) but not registered RPM (1000).
	for i := 0; i < 6; i++ {
		ct.Record("big", 10)
	}
	if !ct.Available("big") {
		t.Error("big should use registered limits, not defaults")
	}
}

// ---------- headroom ----------

func TestCapacityTracker_Headroom_UnknownProvider(t *testing.T) {
	ct := NewCapacityTracker(1000, 10)
	h := ct.Headroom("unknown")
	if h != 1.0 {
		t.Errorf("expected headroom=1.0 for unknown, got %f", h)
	}
}

func TestCapacityTracker_Headroom_Idle(t *testing.T) {
	ct := NewCapacityTracker(1000, 100)
	ct.Register("p", 1000, 100)
	// No events recorded.
	h := ct.Headroom("p")
	if h != 1.0 {
		t.Errorf("expected headroom=1.0 for idle provider, got %f", h)
	}
}

func TestCapacityTracker_Headroom_HalfUsed(t *testing.T) {
	ct := NewCapacityTracker(1000, 100)
	ct.Register("p", 1000, 100)

	// Use 500 of 1000 TPM, 1 of 100 RPM.
	ct.Record("p", 500)
	h := ct.Headroom("p")

	// TPM headroom = 0.5, RPM headroom = 0.99 -> min = 0.5
	if math.Abs(h-0.5) > 0.01 {
		t.Errorf("expected headroom ~0.5, got %f", h)
	}
}

func TestCapacityTracker_Headroom_Saturated(t *testing.T) {
	ct := NewCapacityTracker(100, 10)
	ct.Register("p", 100, 10)

	for i := 0; i < 10; i++ {
		ct.Record("p", 10) // 100 TPM, 10 RPM -> both at limit
	}
	h := ct.Headroom("p")
	if h != 0.0 {
		t.Errorf("expected headroom=0.0 for saturated provider, got %f", h)
	}
}

func TestCapacityTracker_Headroom_UnlimitedDimension(t *testing.T) {
	ct := NewCapacityTracker(0, 10) // no TPM limit
	ct.Register("p", 0, 10)

	ct.Record("p", 999999)
	h := ct.Headroom("p")

	// TPM headroom = 1.0 (unlimited), RPM headroom = 0.9 -> min = 0.9
	if math.Abs(h-0.9) > 0.01 {
		t.Errorf("expected headroom ~0.9, got %f", h)
	}
}

// ---------- sustained hot ----------

func TestCapacityTracker_IsSustainedHot_UnknownProvider(t *testing.T) {
	ct := NewCapacityTracker(1000, 10)
	if ct.IsSustainedHot("unknown") {
		t.Error("unknown provider should not be sustained hot")
	}
}

func TestCapacityTracker_IsSustainedHot_True(t *testing.T) {
	ct := NewCapacityTracker(100, 10)
	ct.Register("p", 100, 10)

	// 9 of 10 RPM used, 90 of 100 TPM -> 90% utilization + enough requests.
	for i := 0; i < 9; i++ {
		ct.Record("p", 10)
	}
	if !ct.IsSustainedHot("p") {
		t.Error("should be sustained hot at 90% utilization with 9 requests")
	}
}

func TestCapacityTracker_IsSustainedHot_HighUtilLowActivity(t *testing.T) {
	ct := NewCapacityTracker(100, 10)
	ct.Register("p", 100, 10)

	// 2 requests, 20 tokens -- high RPM utilization (20%) but below both
	// activity thresholds (3 requests, 1024 tokens).
	ct.Record("p", 10)
	ct.Record("p", 10)
	if ct.IsSustainedHot("p") {
		t.Error("should not be hot with only 2 requests and 20 tokens")
	}
}

func TestCapacityTracker_IsSustainedHot_TokenThreshold(t *testing.T) {
	ct := NewCapacityTracker(2000, 0) // no RPM limit
	ct.Register("p", 2000, 0)

	// 2 requests (below 3) but 1800 tokens (>= 1024), 90% TPM utilization.
	ct.Record("p", 900)
	ct.Record("p", 900)
	if !ct.IsSustainedHot("p") {
		t.Error("should be hot: tokens >= 1024 and 90% TPM utilization")
	}
}

func TestCapacityTracker_IsSustainedHot_LowUtilization(t *testing.T) {
	ct := NewCapacityTracker(10000, 100)
	ct.Register("p", 10000, 100)

	// 5 requests, 50 tokens -- well below 90%.
	for i := 0; i < 5; i++ {
		ct.Record("p", 10)
	}
	if ct.IsSustainedHot("p") {
		t.Error("should not be hot at low utilization")
	}
}

// ---------- ring buffer overflow ----------

func TestCapacityTracker_RingBufferOverflow(t *testing.T) {
	ct := NewCapacityTracker(0, 0) // unlimited
	ct.Register("p", 0, 0)

	// Write more than maxRingSize entries.
	for i := 0; i < maxRingSize+500; i++ {
		ct.Record("p", 1)
	}

	rpm, tpm := ct.Usage("p")

	// The ring retains exactly maxRingSize entries; all should be within the window.
	if rpm != maxRingSize {
		t.Errorf("expected rpm=%d after overflow, got %d", maxRingSize, rpm)
	}
	if tpm != maxRingSize {
		t.Errorf("expected tpm=%d after overflow, got %d", maxRingSize, tpm)
	}
}

func TestCapacityTracker_RingBufferOverflow_OldEvicted(t *testing.T) {
	ct := NewCapacityTracker(0, 0)
	ct.Register("p", 0, 0)

	// Freeze time so we can control it.
	now := time.Now()
	ct.nowFunc = func() time.Time { return now }

	// Fill the ring with "old" entries (2 minutes ago).
	ct.nowFunc = func() time.Time { return now.Add(-2 * time.Minute) }
	for i := 0; i < maxRingSize; i++ {
		ct.Record("p", 10)
	}

	// Now add 100 "fresh" entries that overwrite the oldest ring slots.
	ct.nowFunc = func() time.Time { return now }
	for i := 0; i < 100; i++ {
		ct.Record("p", 5)
	}

	rpm, tpm := ct.Usage("p")
	if rpm != 100 {
		t.Errorf("expected rpm=100 (only fresh entries), got %d", rpm)
	}
	if tpm != 500 {
		t.Errorf("expected tpm=500, got %d", tpm)
	}
}

// ---------- window expiry via nowFunc ----------

func TestCapacityTracker_WindowExpiry(t *testing.T) {
	ct := NewCapacityTracker(100, 10)

	now := time.Now()
	// Record at t-90s (outside window).
	ct.nowFunc = func() time.Time { return now.Add(-90 * time.Second) }
	ct.Record("p", 50)

	// Record at t-30s (inside window).
	ct.nowFunc = func() time.Time { return now.Add(-30 * time.Second) }
	ct.Record("p", 30)

	// Query at t=now.
	ct.nowFunc = func() time.Time { return now }
	rpm, tpm := ct.Usage("p")
	if rpm != 1 {
		t.Errorf("expected rpm=1 (old entry expired), got %d", rpm)
	}
	if tpm != 30 {
		t.Errorf("expected tpm=30 (old entry expired), got %d", tpm)
	}
}

// ---------- concurrency ----------

func TestCapacityTracker_ConcurrentAccess(t *testing.T) {
	ct := NewCapacityTracker(0, 0) // unlimited
	ct.Register("p", 0, 0)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ct.Record("p", 1)
			ct.Available("p")
			ct.Usage("p")
			ct.Headroom("p")
			ct.IsSustainedHot("p")
		}()
	}
	wg.Wait()

	rpm, _ := ct.Usage("p")
	if rpm != 100 {
		t.Errorf("expected 100 concurrent records, got %d", rpm)
	}
}

// ---------- unknown provider defaults ----------

func TestCapacityTracker_UnknownProviderUsesDefaults(t *testing.T) {
	ct := NewCapacityTracker(100, 5)

	// Record into unregistered provider -- should pick up default limits.
	for i := 0; i < 5; i++ {
		ct.Record("unregistered", 20)
	}

	// RPM limit 5 hit.
	if ct.Available("unregistered") {
		t.Error("unregistered provider should use default RPM=5")
	}

	// TPM = 100, limit = 100 -> headroom = 0.
	h := ct.Headroom("unregistered")
	if h != 0.0 {
		t.Errorf("expected headroom=0.0 for saturated unregistered provider, got %f", h)
	}
}
