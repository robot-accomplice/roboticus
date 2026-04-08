package llm

import (
	"sync"
	"time"
)

// CascadeStrategy determines whether to try a cheap model first or go direct.
type CascadeStrategy int

const (
	// StrategyCascade tries a weak (cheap) model first, escalating only on low confidence.
	StrategyCascade CascadeStrategy = iota
	// StrategyDirect skips the cheap model and goes to the strong model immediately.
	StrategyDirect
)

// CascadeOutcome records the result of a cascade decision for learning.
type CascadeOutcome struct {
	QueryClass    string
	WeakModelUsed bool
	WeakSucceeded bool
	WeakLatency   time.Duration
	StrongLatency time.Duration
	RecordedAt    time.Time
}

// CascadeOptimizer tracks per-query-class statistics to decide whether
// cascading (try cheap model first) has positive expected utility vs going direct.
type CascadeOptimizer struct {
	mu            sync.Mutex
	windowSize    int
	latencyWeight float64
	outcomes      map[string][]CascadeOutcome
}

// NewCascadeOptimizer creates an optimizer with the given sliding window size.
func NewCascadeOptimizer(windowSize int) *CascadeOptimizer {
	if windowSize < 10 {
		windowSize = 50
	}
	return &CascadeOptimizer{
		windowSize:    windowSize,
		latencyWeight: 0.001,
		outcomes:      make(map[string][]CascadeOutcome),
	}
}

// Record stores a cascade outcome for future decisions.
func (co *CascadeOptimizer) Record(outcome CascadeOutcome) {
	co.mu.Lock()
	defer co.mu.Unlock()

	outcome.RecordedAt = time.Now()
	window := co.outcomes[outcome.QueryClass]
	window = append(window, outcome)
	if len(window) > co.windowSize {
		window = window[len(window)-co.windowSize:]
	}
	co.outcomes[outcome.QueryClass] = window
}

// ShouldCascade returns the recommended strategy for a given query class.
func (co *CascadeOptimizer) ShouldCascade(queryClass string) CascadeStrategy {
	co.mu.Lock()
	defer co.mu.Unlock()

	window, ok := co.outcomes[queryClass]
	if !ok || len(window) < 3 {
		// Unknown class — default to cascade (try cheap first).
		return StrategyCascade
	}

	cascadeUtility, directUtility := co.expectedUtility(window)
	if cascadeUtility >= directUtility {
		return StrategyCascade
	}
	return StrategyDirect
}

// expectedUtility computes expected utility for cascade vs direct strategies.
//
// The utility formula aligns with the Rust reference:
//
//	U = P(weak_ok) * saved_latency - (1 - P(weak_ok)) * wasted_latency - cost_delta
//
// Where:
//   - P(weak_ok) = success rate of the weak model
//   - saved_latency = strong_latency - weak_latency (latency saved when weak succeeds)
//   - wasted_latency = weak_latency (latency wasted when weak fails and we must retry)
//   - cost_delta = latencyWeight * avgWeakLatency (proxy for the extra cost of trying weak)
//
// Cascade is preferred when U > 0 (positive expected savings).
func (co *CascadeOptimizer) expectedUtility(window []CascadeOutcome) (cascade, direct float64) {
	if len(window) == 0 {
		return 0.5, 0.5
	}

	var weakAttempts, weakSuccesses int
	var avgWeakLatency, avgStrongLatency float64

	for _, o := range window {
		if o.WeakModelUsed {
			weakAttempts++
			avgWeakLatency += float64(o.WeakLatency.Milliseconds())
			if o.WeakSucceeded {
				weakSuccesses++
			}
		}
		avgStrongLatency += float64(o.StrongLatency.Milliseconds())
	}

	n := float64(len(window))
	avgStrongLatency /= n

	if weakAttempts == 0 {
		return 0.5, 0.5
	}

	successRate := float64(weakSuccesses) / float64(weakAttempts)
	avgWeakLatency /= float64(weakAttempts)

	// Rust-aligned utility formula:
	// U = P(weak_ok) * saved_latency - (1-P(weak_ok)) * wasted_latency - cost_delta
	savedLatency := avgStrongLatency - avgWeakLatency
	wastedLatency := avgWeakLatency
	costDelta := co.latencyWeight * avgWeakLatency

	cascadeU := successRate*co.latencyWeight*savedLatency -
		(1-successRate)*co.latencyWeight*wastedLatency -
		costDelta

	// Direct utility is the baseline (0); cascade is chosen when U > 0.
	// We normalize to the same scale as before for backward compatibility.
	direct = 1.0 - co.latencyWeight*avgStrongLatency
	cascade = direct + cascadeU

	return cascade, direct
}

// Stats returns the current success rate for a query class.
func (co *CascadeOptimizer) Stats(queryClass string) (successRate float64, sampleSize int) {
	co.mu.Lock()
	defer co.mu.Unlock()

	window := co.outcomes[queryClass]
	if len(window) == 0 {
		return 0, 0
	}

	var successes int
	for _, o := range window {
		if o.WeakModelUsed && o.WeakSucceeded {
			successes++
		}
	}
	return float64(successes) / float64(len(window)), len(window)
}
