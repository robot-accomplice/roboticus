package memory

import (
	"sync"
	"time"
)

// QuiescenceGate tracks session activity and determines when the system is
// quiescent (no activity for a configurable threshold). Used to gate expensive
// consolidation operations that should only run during quiet periods.
type QuiescenceGate struct {
	mu           sync.RWMutex
	lastActivity time.Time
	threshold    time.Duration
}

// NewQuiescenceGate creates a gate with a 5-second default threshold.
func NewQuiescenceGate() *QuiescenceGate {
	return &QuiescenceGate{
		lastActivity: time.Now(),
		threshold:    5 * time.Second,
	}
}

// NewQuiescenceGateWithThreshold creates a gate with a custom threshold.
func NewQuiescenceGateWithThreshold(threshold time.Duration) *QuiescenceGate {
	return &QuiescenceGate{
		lastActivity: time.Now(),
		threshold:    threshold,
	}
}

// RecordActivity marks the current time as the last activity timestamp.
// Call this on every session interaction (message, tool call, etc.).
func (q *QuiescenceGate) RecordActivity() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lastActivity = time.Now()
}

// IsQuiescent returns true when no session activity has occurred within the
// threshold window. Safe for concurrent access.
func (q *QuiescenceGate) IsQuiescent() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return time.Since(q.lastActivity) >= q.threshold
}

// LastActivity returns the timestamp of the most recent activity.
func (q *QuiescenceGate) LastActivity() time.Time {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.lastActivity
}

// TimeSinceActivity returns the duration since the last activity.
func (q *QuiescenceGate) TimeSinceActivity() time.Duration {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return time.Since(q.lastActivity)
}

// ConfidenceDecayGate limits confidence decay to once per 24 hours using a
// sentinel timestamp. Prevents rapid confidence erosion from frequent
// consolidation runs.
type ConfidenceDecayGate struct {
	mu       sync.RWMutex
	lastRun  time.Time
	interval time.Duration
}

// NewConfidenceDecayGate creates a gate that allows decay once per 24 hours.
func NewConfidenceDecayGate() *ConfidenceDecayGate {
	return &ConfidenceDecayGate{
		interval: 24 * time.Hour,
	}
}

// NewConfidenceDecayGateWithInterval creates a gate with a custom interval.
func NewConfidenceDecayGateWithInterval(interval time.Duration) *ConfidenceDecayGate {
	return &ConfidenceDecayGate{
		interval: interval,
	}
}

// ShouldDecay returns true if enough time has elapsed since the last decay run.
// Automatically marks the current time as the last run if it returns true.
func (g *ConfidenceDecayGate) ShouldDecay() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastRun.IsZero() || time.Since(g.lastRun) >= g.interval {
		g.lastRun = time.Now()
		return true
	}
	return false
}

// LastRun returns the timestamp of the most recent decay run.
func (g *ConfidenceDecayGate) LastRun() time.Time {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.lastRun
}

// Reset clears the last run timestamp, allowing the next ShouldDecay call
// to return true. Useful for testing.
func (g *ConfidenceDecayGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastRun = time.Time{}
}
