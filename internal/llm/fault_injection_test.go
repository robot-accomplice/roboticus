package llm

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Fault Injection / Chaos Tests ---
// Ported from Rust: crates/roboticus-tests/src/fault_injection.rs
// Verifies circuit breaker behavioral properties under failure conditions.

func TestFaultInjection_CascadingFailure_FallbackSelected(t *testing.T) {
	// When the primary provider's breaker opens, the router must fall back
	// to the next available provider.
	cfg := CircuitBreakerConfig{Threshold: 2, Window: time.Minute, Cooldown: time.Second, MaxCooldown: time.Second, HalfOpenProbes: 1}
	reg := NewBreakerRegistry(cfg)

	primary := reg.Get("primary")
	_ = reg.Get("fallback")

	// Trip the primary breaker.
	primary.RecordFailure()
	primary.RecordFailure()

	if primary.State() != CircuitOpen {
		t.Fatal("primary should be open after threshold failures")
	}

	// Primary should block, fallback should allow.
	if primary.Allow() {
		t.Fatal("primary should not allow requests when open")
	}
	fallback := reg.Get("fallback")
	if !fallback.Allow() {
		t.Fatal("fallback should allow requests")
	}
}

func TestFaultInjection_TransientFailures_DontPermanentlyDisable(t *testing.T) {
	// Transient failures within the window but below threshold don't trip.
	cfg := CircuitBreakerConfig{Threshold: 5, Window: time.Minute, Cooldown: time.Second, MaxCooldown: time.Second, HalfOpenProbes: 1}
	cb := NewCircuitBreaker(cfg)

	// Record 3 failures (below threshold of 5).
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitClosed {
		t.Fatal("should remain closed with failures below threshold")
	}

	// Intersperse successes — the provider recovers.
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Fatal("should remain closed after recovery")
	}
	if !cb.Allow() {
		t.Fatal("should allow requests when closed")
	}
}

func TestFaultInjection_FullStateTransitionCycle(t *testing.T) {
	// CLOSED → OPEN → HALF_OPEN → CLOSED (full recovery cycle).
	cfg := CircuitBreakerConfig{
		Threshold:      3,
		Window:         time.Minute,
		Cooldown:       10 * time.Millisecond,
		MaxCooldown:    10 * time.Millisecond,
		HalfOpenProbes: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Phase 1: CLOSED → OPEN.
	if cb.State() != CircuitClosed {
		t.Fatal("should start closed")
	}
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Fatal("should be open after threshold failures")
	}

	// Phase 2: OPEN → HALF_OPEN (after cooldown).
	time.Sleep(15 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("should allow probe request after cooldown")
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatal("should be half-open after cooldown probe")
	}

	// Phase 3: HALF_OPEN → CLOSED (on success).
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Fatal("should be closed after successful probe")
	}
}

func TestFaultInjection_HalfOpenFailure_ReOpens(t *testing.T) {
	// Failure during half-open state re-opens the breaker.
	cfg := CircuitBreakerConfig{
		Threshold:      2,
		Window:         time.Minute,
		Cooldown:       10 * time.Millisecond,
		MaxCooldown:    100 * time.Millisecond,
		HalfOpenProbes: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Trip the breaker.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("should be open")
	}

	// Wait for cooldown, enter half-open.
	time.Sleep(15 * time.Millisecond)
	cb.Allow() // transitions to half-open
	if cb.State() != CircuitHalfOpen {
		t.Fatal("should be half-open")
	}

	// Fail the probe — should re-open.
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("should re-open after half-open failure")
	}
}

func TestFaultInjection_CreditError_StickyTrip(t *testing.T) {
	// Credit errors (402) create a sticky trip that doesn't auto-recover.
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	cb.RecordCreditError()
	if !cb.IsCreditTripped() {
		t.Fatal("should be credit-tripped")
	}

	// Even after cooldown, credit trip persists.
	time.Sleep(50 * time.Millisecond)
	if cb.Allow() {
		t.Fatal("credit-tripped breaker should never auto-allow")
	}

	// Only explicit reset clears it.
	cb.Reset()
	if cb.IsCreditTripped() {
		t.Fatal("reset should clear credit trip")
	}
	if !cb.Allow() {
		t.Fatal("should allow after reset")
	}
}

func TestFaultInjection_ConcurrentFailures_ThreadSafe(t *testing.T) {
	// Multiple goroutines recording failures concurrently must not cause data races.
	cfg := CircuitBreakerConfig{Threshold: 100, Window: time.Minute, Cooldown: time.Second, MaxCooldown: time.Second, HalfOpenProbes: 1}
	cb := NewCircuitBreaker(cfg)

	var wg sync.WaitGroup
	var allowed atomic.Int64

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if cb.Allow() {
					allowed.Add(1)
				}
				if j%2 == 0 {
					cb.RecordFailure()
				} else {
					cb.RecordSuccess()
				}
			}
		}()
	}
	wg.Wait()

	// No panic or data race = pass. At least some requests should have been allowed.
	if allowed.Load() == 0 {
		t.Fatal("some requests should have been allowed")
	}
}

func TestFaultInjection_BreakerRegistry_IsolatesProviders(t *testing.T) {
	// Breaker state for one provider must not affect another.
	reg := NewBreakerRegistry(CircuitBreakerConfig{
		Threshold: 2, Window: time.Minute, Cooldown: time.Minute, MaxCooldown: time.Minute, HalfOpenProbes: 1,
	})

	a := reg.Get("provider-a")
	b := reg.Get("provider-b")

	// Trip provider A.
	a.RecordFailure()
	a.RecordFailure()
	if a.State() != CircuitOpen {
		t.Fatal("provider-a should be open")
	}

	// Provider B should be unaffected.
	if b.State() != CircuitClosed {
		t.Fatal("provider-b should still be closed")
	}
	if !b.Allow() {
		t.Fatal("provider-b should allow requests")
	}
}

func TestFaultInjection_BreakerWrappedCompleter_BlocksWhenOpen(t *testing.T) {
	// WithBreaker wrapper should return an error when the breaker is open.
	cfg := CircuitBreakerConfig{Threshold: 1, Window: time.Minute, Cooldown: time.Minute, MaxCooldown: time.Minute, HalfOpenProbes: 1}
	cb := NewCircuitBreaker(cfg)

	inner := &stubCompleter{response: &Response{Content: "ok"}}
	wrapped := WithBreaker(inner, cb)

	// First call should succeed.
	resp, err := wrapped.Complete(context.Background(), &Request{})
	if err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatal("unexpected response")
	}

	// Trip the breaker.
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("should be open after 1 failure with threshold=1")
	}

	// Second call should be blocked by the breaker.
	_, err = wrapped.Complete(context.Background(), &Request{})
	if err == nil {
		t.Fatal("should return error when breaker is open")
	}
	if !strings.Contains(err.Error(), "circuit breaker open") {
		t.Fatalf("expected circuit breaker error, got: %v", err)
	}
}

// stubCompleter is a minimal Completer for testing.
type stubCompleter struct {
	response *Response
	err      error
}

func (s *stubCompleter) Complete(_ context.Context, _ *Request) (*Response, error) {
	return s.response, s.err
}

func (s *stubCompleter) Stream(_ context.Context, _ *Request) (<-chan StreamChunk, <-chan error) {
	ch := make(chan StreamChunk)
	errs := make(chan error, 1)
	close(ch)
	if s.err != nil {
		errs <- s.err
	}
	close(errs)
	return ch, errs
}

func TestFaultInjection_CapacityPressure_ReducesThroughput(t *testing.T) {
	// Capacity pressure should reduce throughput to approximately 1-in-4 requests.
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	cb.SetCapacityPressure(true)

	allowed := 0
	total := 100
	for i := 0; i < total; i++ {
		if cb.Allow() {
			allowed++
		}
	}

	// With 1-in-4 throttling, expect roughly 25% ± tolerance.
	if allowed == 0 {
		t.Fatal("capacity pressure should still allow some requests")
	}
	if allowed == total {
		t.Fatal("capacity pressure should reduce throughput")
	}
	ratio := float64(allowed) / float64(total)
	if ratio > 0.5 {
		t.Errorf("expected ~25%% throughput under capacity pressure, got %.0f%%", ratio*100)
	}
}
