package llm

import (
	"testing"
	"time"
)

func TestCircuitBreaker_StartsClose(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	if cb.State() != CircuitClosed {
		t.Error("new breaker should be closed")
	}
	if !cb.Allow() {
		t.Error("closed breaker should allow requests")
	}
}

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	cfg := CircuitBreakerConfig{
		Threshold:      3,
		Window:         10 * time.Second,
		Cooldown:       100 * time.Millisecond,
		MaxCooldown:    1 * time.Second,
		HalfOpenProbes: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Record failures up to threshold.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Error("should still be closed after 2 failures")
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("should be open after %d failures, got %v", cfg.Threshold, cb.State())
	}

	if cb.Allow() {
		t.Error("open breaker should not allow requests")
	}
}

func TestCircuitBreaker_RecoversThroughHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		Threshold:      2,
		Window:         10 * time.Second,
		Cooldown:       50 * time.Millisecond,
		MaxCooldown:    1 * time.Second,
		HalfOpenProbes: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Trip the breaker.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("should be open")
	}

	// Wait for cooldown.
	time.Sleep(60 * time.Millisecond)

	// Should transition to half-open and allow one probe.
	if !cb.Allow() {
		t.Error("should allow probe request after cooldown")
	}
	if cb.State() != CircuitHalfOpen {
		t.Error("should be half-open")
	}

	// Success closes the circuit.
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Error("success in half-open should close circuit")
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cfg := CircuitBreakerConfig{
		Threshold:      2,
		Window:         10 * time.Second,
		Cooldown:       50 * time.Millisecond,
		MaxCooldown:    1 * time.Second,
		HalfOpenProbes: 1,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow() // transition to half-open

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("failure in half-open should reopen circuit")
	}
}

func TestCircuitBreaker_SlidingWindow(t *testing.T) {
	cfg := CircuitBreakerConfig{
		Threshold:      3,
		Window:         100 * time.Millisecond,
		Cooldown:       50 * time.Millisecond,
		MaxCooldown:    1 * time.Second,
		HalfOpenProbes: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Two failures, then wait for them to expire.
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(150 * time.Millisecond)

	// These are the only failures in the window now.
	cb.RecordFailure()
	cb.RecordFailure()

	// Should still be closed since old failures fell out of window.
	if cb.State() != CircuitClosed {
		t.Error("old failures outside window should not count")
	}

	// One more should trip it.
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("3 failures within window should trip")
	}
}

func TestCircuitBreaker_ForceOpenBlocksAllRequests(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	if !cb.Allow() {
		t.Fatal("closed breaker should allow requests")
	}

	cb.ForceOpen()

	if cb.Allow() {
		t.Error("force-opened breaker must block all requests")
	}
	if cb.State() != CircuitOpen {
		t.Errorf("force-opened breaker state should be Open, got %v", cb.State())
	}
	if !cb.IsForcedOpen() {
		t.Error("IsForcedOpen should return true")
	}
}

func TestCircuitBreaker_ForceOpenNotClearedByCooldown(t *testing.T) {
	cfg := CircuitBreakerConfig{
		Threshold:      2,
		Window:         10 * time.Second,
		Cooldown:       10 * time.Millisecond,
		MaxCooldown:    1 * time.Second,
		HalfOpenProbes: 1,
	}
	cb := NewCircuitBreaker(cfg)
	cb.ForceOpen()

	// Wait well past the cooldown.
	time.Sleep(30 * time.Millisecond)

	if cb.Allow() {
		t.Error("force-opened breaker must NOT auto-recover via cooldown")
	}
	if cb.State() != CircuitOpen {
		t.Error("force-opened breaker should remain Open after cooldown")
	}
}

func TestCircuitBreaker_ForceOpenClearedByReset(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	cb.ForceOpen()

	if cb.Allow() {
		t.Fatal("force-opened breaker must block")
	}

	cb.Reset()

	if !cb.Allow() {
		t.Error("breaker should allow requests after Reset")
	}
	if cb.IsForcedOpen() {
		t.Error("IsForcedOpen should be false after Reset")
	}
	if cb.State() != CircuitClosed {
		t.Error("state should be Closed after Reset")
	}
}

func TestCircuitBreaker_CapacityPressureReducesThroughput(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	cb.SetCapacityPressure(true)

	if !cb.HasCapacityPressure() {
		t.Fatal("HasCapacityPressure should return true")
	}

	// Effective state should be half-open under pressure.
	if cb.State() != CircuitHalfOpen {
		t.Errorf("closed breaker under pressure should report HalfOpen, got %v", cb.State())
	}

	// Allow should permit roughly 1 in 4 requests.
	allowed := 0
	const total = 20
	for range total {
		if cb.Allow() {
			allowed++
		}
	}
	expected := total / 4
	if allowed != expected {
		t.Errorf("expected %d allowed out of %d under pressure, got %d", expected, total, allowed)
	}
}

func TestCircuitBreaker_ClearCapacityPressureRestoresThroughput(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	cb.SetCapacityPressure(true)

	// Drain a few requests under pressure.
	for range 8 {
		cb.Allow()
	}

	cb.SetCapacityPressure(false)

	if cb.HasCapacityPressure() {
		t.Error("HasCapacityPressure should be false after clearing")
	}
	if cb.State() != CircuitClosed {
		t.Errorf("state should be Closed after clearing pressure, got %v", cb.State())
	}

	// All requests should be allowed now.
	for i := range 10 {
		if !cb.Allow() {
			t.Errorf("request %d should be allowed after clearing pressure", i)
		}
	}
}

func TestBreakerRegistry(t *testing.T) {
	reg := NewBreakerRegistry(DefaultCircuitBreakerConfig())

	cb1 := reg.Get("openai")
	cb2 := reg.Get("openai")
	if cb1 != cb2 {
		t.Error("same provider should return same breaker")
	}

	cb3 := reg.Get("anthropic")
	if cb1 == cb3 {
		t.Error("different providers should get different breakers")
	}
}
