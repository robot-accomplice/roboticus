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
// Utility = quality - latencyWeight * latencyMs
// For cascade: quality = successRate (cheap model answers correctly) + (1-successRate)*1.0 (strong always correct)
// For direct: quality = 1.0 (strong model always correct)
// Cascade saves latency when cheap succeeds; costs extra latency when it fails.
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

	// Cascade utility: when weak succeeds, we pay weak latency only.
	// When weak fails, we pay weak + strong latency.
	cascadeLatency := successRate*avgWeakLatency + (1-successRate)*(avgWeakLatency+avgStrongLatency)
	cascade = 1.0 - co.latencyWeight*cascadeLatency

	// Direct utility: always pay strong latency.
	direct = 1.0 - co.latencyWeight*avgStrongLatency

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
