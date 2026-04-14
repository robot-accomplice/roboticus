package llm

import (
	"sync"
)

// SessionEscalationTracker tracks per-session quality signals and recommends
// model escalation when a session's inference quality degrades. This is distinct
// from the tier-level EscalationTracker in tiered.go — that one tracks global
// local-vs-cloud metrics, while this one tracks per-session consecutive failures
// and quality scores to trigger within-session model upgrades.
//
// Escalation rule: if 2+ consecutive failures OR quality < 0.3 for 3+ turns
// in a session, suggest escalating to a higher-capability model from the
// configured fallback chain.
type SessionEscalationTracker struct {
	mu       sync.RWMutex
	sessions map[string]*sessionSignals
	// fallbacks is the ordered model chain from config (primary → fallback1 → ...).
	// Escalation suggests the next model in the chain after the current one.
	fallbacks []string
}

// sessionSignals tracks quality signals for one session.
type sessionSignals struct {
	consecutiveFailures int
	lowQualityCount     int       // turns with quality < 0.3
	recentQualities     []float64 // last N quality scores
	currentModel        string    // last model used
	totalTurns          int
}

// NewSessionEscalationTracker creates a new per-session escalation tracker.
func NewSessionEscalationTracker(fallbacks []string) *SessionEscalationTracker {
	return &SessionEscalationTracker{
		sessions:  make(map[string]*sessionSignals),
		fallbacks: fallbacks,
	}
}

// RecordOutcome records an inference outcome for a session.
func (t *SessionEscalationTracker) RecordOutcome(sessionID, model string, success bool, quality float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.getOrCreate(sessionID)
	s.currentModel = model
	s.totalTurns++

	if !success {
		s.consecutiveFailures++
	} else {
		s.consecutiveFailures = 0
	}

	// Track quality in a sliding window of the last 10 turns.
	s.recentQualities = append(s.recentQualities, quality)
	if len(s.recentQualities) > 10 {
		s.recentQualities = s.recentQualities[len(s.recentQualities)-10:]
	}

	if quality < 0.3 {
		s.lowQualityCount++
	} else {
		// Reset if we get a good-quality turn.
		s.lowQualityCount = 0
	}
}

// ShouldEscalate checks whether a session should escalate to a higher model.
// Returns (shouldEscalate, suggestedModel).
func (t *SessionEscalationTracker) ShouldEscalate(sessionID string) (bool, string) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	s, ok := t.sessions[sessionID]
	if !ok {
		return false, ""
	}

	// Rule 1: 2+ consecutive failures → escalate.
	if s.consecutiveFailures >= 2 {
		if next := t.nextModel(s.currentModel); next != "" {
			return true, next
		}
	}

	// Rule 2: quality < 0.3 for 3+ consecutive turns → escalate.
	if s.lowQualityCount >= 3 {
		if next := t.nextModel(s.currentModel); next != "" {
			return true, next
		}
	}

	return false, ""
}

// ResetSession clears tracking for a session (e.g., on session close).
func (t *SessionEscalationTracker) ResetSession(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, sessionID)
}

// getOrCreate returns the signals for a session, creating if needed.
// Caller must hold the write lock.
func (t *SessionEscalationTracker) getOrCreate(sessionID string) *sessionSignals {
	s, ok := t.sessions[sessionID]
	if !ok {
		s = &sessionSignals{}
		t.sessions[sessionID] = s
	}
	return s
}

// nextModel returns the next higher-capability model in the fallback chain
// after the given model. Returns "" if already at the highest or unknown.
func (t *SessionEscalationTracker) nextModel(current string) string {
	if len(t.fallbacks) == 0 {
		return ""
	}

	// Find current model's position in the chain.
	for i, m := range t.fallbacks {
		if m == current {
			// The fallback chain is ordered primary → fallback1 → fallback2 etc.
			// "Escalating" means using an earlier (higher-capability) model in
			// the chain? No — the chain is primary (best) → fallbacks (cheaper).
			// Escalation goes UP: from a later fallback to the primary.
			if i > 0 {
				return t.fallbacks[i-1]
			}
			return "" // Already at the top.
		}
	}

	// Current model not in chain — suggest the primary (first in chain).
	return t.fallbacks[0]
}
