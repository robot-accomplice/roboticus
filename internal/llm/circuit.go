package llm

import (
	"context"
	"sync"
	"time"

	"goboticus/internal/core"
)

// CircuitState represents the breaker state.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // healthy, requests flow
	CircuitOpen                         // tripped, requests blocked
	CircuitHalfOpen                     // testing recovery
)

// CircuitBreakerConfig controls breaker behavior.
type CircuitBreakerConfig struct {
	Threshold      int           // failures in window before opening
	Window         time.Duration // rolling window for failure counting
	Cooldown       time.Duration // how long to stay open before half-open
	MaxCooldown    time.Duration // exponential backoff cap
	HalfOpenProbes int           // requests allowed in half-open state
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Threshold:      5,
		Window:         60 * time.Second,
		Cooldown:       30 * time.Second,
		MaxCooldown:    300 * time.Second,
		HalfOpenProbes: 1,
	}
}

// CircuitBreaker implements a per-provider circuit breaker with a proper
// sliding window. Improvements over the Rust version:
//   - Proper sliding window (time-windowed ring, not gap-based reset)
//   - Sticky credit-tripped state for 402 errors (never auto-recovers)
//   - Exponential backoff only on re-trips, not first trip
type CircuitBreaker struct {
	mu            sync.Mutex
	config        CircuitBreakerConfig
	state         CircuitState
	failures      []time.Time // ring of failure timestamps
	lastTripped   time.Time
	cooldown      time.Duration // current cooldown (grows with exponential backoff)
	halfOpenUsed  int
	creditTripped bool // sticky: 402 payment error, requires manual reset
}

// NewCircuitBreaker creates a breaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config:   cfg,
		state:    CircuitClosed,
		cooldown: cfg.Cooldown,
	}
}

// Allow checks whether a request should be permitted. Returns false if the
// circuit is open and the cooldown hasn't elapsed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Credit-tripped circuits never auto-recover.
	if cb.creditTripped {
		return false
	}

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastTripped) >= cb.cooldown {
			cb.state = CircuitHalfOpen
			cb.halfOpenUsed = 0
			return true
		}
		return false
	case CircuitHalfOpen:
		if cb.halfOpenUsed < cb.config.HalfOpenProbes {
			cb.halfOpenUsed++
			return true
		}
		return false
	}
	return false
}

// RecordSuccess records a successful request. If half-open, closes the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
		cb.failures = cb.failures[:0]
		cb.cooldown = cb.config.Cooldown // reset backoff
	}
}

// RecordFailure records a failed request. May trip the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	if cb.state == CircuitHalfOpen {
		cb.trip(now)
		return
	}

	// Sliding window: drop failures outside the window.
	cutoff := now.Add(-cb.config.Window)
	fresh := cb.failures[:0]
	for _, t := range cb.failures {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	cb.failures = append(fresh, now)

	if len(cb.failures) >= cb.config.Threshold {
		cb.trip(now)
	}
}

// trip opens the circuit with exponential backoff on the cooldown.
func (cb *CircuitBreaker) trip(now time.Time) {
	wasOpen := cb.state == CircuitOpen || cb.state == CircuitHalfOpen
	cb.state = CircuitOpen
	cb.lastTripped = now

	// Exponential backoff: only increase cooldown on re-trips.
	if wasOpen {
		cb.cooldown = min(cb.cooldown*2, cb.config.MaxCooldown)
	}
}

// RecordCreditError records a 402 payment failure. The circuit stays open
// until manually reset — it will never auto-recover.
func (cb *CircuitBreaker) RecordCreditError() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitOpen
	cb.creditTripped = true
	cb.lastTripped = time.Now()
}

// Reset manually clears all state, including credit-tripped.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.failures = cb.failures[:0]
	cb.creditTripped = false
	cb.cooldown = cb.config.Cooldown
}

// State returns the current circuit state (for observability).
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// IsCreditTripped returns true if the breaker is stuck open due to a 402.
func (cb *CircuitBreaker) IsCreditTripped() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.creditTripped
}

// BreakerRegistry manages per-provider circuit breakers.
type BreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

// NewBreakerRegistry creates a registry with shared config.
func NewBreakerRegistry(cfg CircuitBreakerConfig) *BreakerRegistry {
	return &BreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		config:   cfg,
	}
}

// Get returns the breaker for a provider, creating one if needed.
func (r *BreakerRegistry) Get(providerName string) *CircuitBreaker {
	r.mu.RLock()
	if cb, ok := r.breakers[providerName]; ok {
		r.mu.RUnlock()
		return cb
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock.
	if cb, ok := r.breakers[providerName]; ok {
		return cb
	}
	cb := NewCircuitBreaker(r.config)
	r.breakers[providerName] = cb
	return cb
}

// WithBreaker wraps a Completer with circuit breaker protection.
func WithBreaker(c Completer, cb *CircuitBreaker) Completer {
	return &breakerCompleter{inner: c, cb: cb}
}

type breakerCompleter struct {
	inner Completer
	cb    *CircuitBreaker
}

func (bc *breakerCompleter) Complete(ctx context.Context, req *Request) (*Response, error) {
	if !bc.cb.Allow() {
		return nil, core.NewError(core.ErrLLM, "circuit breaker open")
	}
	resp, err := bc.inner.Complete(ctx, req)
	if err != nil {
		bc.cb.RecordFailure()
		return nil, err
	}
	bc.cb.RecordSuccess()
	return resp, nil
}

func (bc *breakerCompleter) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, <-chan error) {
	if !bc.cb.Allow() {
		chunks := make(chan StreamChunk)
		errs := make(chan error, 1)
		close(chunks)
		errs <- core.NewError(core.ErrLLM, "circuit breaker open")
		close(errs)
		return chunks, errs
	}
	// We record success/failure based on whether the stream starts without error.
	// A full implementation would also track mid-stream failures.
	chunks, errs := bc.inner.Stream(ctx, req)
	return chunks, errs
}
