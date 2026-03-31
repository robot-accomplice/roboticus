package agent

import (
	"context"
	"sync"
	"time"

	"goboticus/internal/db"
)

// AbuseSignalType classifies the kind of abuse signal detected.
type AbuseSignalType string

const (
	SignalRateBurst       AbuseSignalType = "rate_burst"
	SignalPolicyViolation AbuseSignalType = "policy_violation"
	SignalRepetitionSpam  AbuseSignalType = "repetition_spam"
	SignalSessionChurn    AbuseSignalType = "session_churn"
	SignalSensitiveProbe  AbuseSignalType = "sensitive_probe"
)

// EnforcementAction is the graduated response to abuse.
type EnforcementAction string

const (
	ActionAllow      EnforcementAction = "allow"
	ActionSlowdown   EnforcementAction = "slowdown"
	ActionQuarantine EnforcementAction = "quarantine"
)

// AbuseSignal represents a detected abuse indicator.
type AbuseSignal struct {
	ActorID    string          `json:"actor_id"`
	Origin     string          `json:"origin"`
	Channel    string          `json:"channel"`
	SignalType AbuseSignalType `json:"signal_type"`
	Severity   float64         `json:"severity"` // 0.0 to 1.0
	Detail     string          `json:"detail"`
}

// AbuseEvent is a stored abuse signal with metadata.
type AbuseEvent struct {
	ID        string            `json:"id"`
	ActorID   string            `json:"actor_id"`
	Signal    AbuseSignalType   `json:"signal_type"`
	Severity  float64           `json:"severity"`
	Detail    string            `json:"detail"`
	Action    EnforcementAction `json:"action_taken"`
	CreatedAt time.Time         `json:"created_at"`
}

// AbuseTrackerConfig controls the abuse detection system.
type AbuseTrackerConfig struct {
	Enabled             bool    `toml:"enabled"`
	WindowMinutes       int     `toml:"window_minutes"`
	SlowdownThreshold   float64 `toml:"slowdown_threshold"`
	QuarantineThreshold float64 `toml:"quarantine_threshold"`
	MaxTrackedActors    int     `toml:"max_tracked_actors"`
}

// DefaultAbuseTrackerConfig returns sensible defaults.
func DefaultAbuseTrackerConfig() AbuseTrackerConfig {
	return AbuseTrackerConfig{
		Enabled:             true,
		WindowMinutes:       5,
		SlowdownThreshold:   0.5,
		QuarantineThreshold: 0.8,
		MaxTrackedActors:    10000,
	}
}

// AbuseTracker aggregates abuse signals and computes enforcement actions.
type AbuseTracker struct {
	config AbuseTrackerConfig
	store  *db.Store
	mu     sync.Mutex
	// In-memory score cache: actorID → aggregate score in current window.
	scores map[string]*actorScore
}

type actorScore struct {
	score       float64
	lastSeen    time.Time
	signalCount int
}

// NewAbuseTracker creates an abuse tracker.
func NewAbuseTracker(cfg AbuseTrackerConfig, store *db.Store) *AbuseTracker {
	return &AbuseTracker{
		config: cfg,
		store:  store,
		scores: make(map[string]*actorScore),
	}
}

// RecordSignal records an abuse signal and returns the enforcement action.
func (t *AbuseTracker) RecordSignal(ctx context.Context, signal AbuseSignal) (EnforcementAction, error) {
	if !t.config.Enabled {
		return ActionAllow, nil
	}

	// Store the event in the database.
	action := t.computeAction(signal.ActorID, signal.Severity)

	severityText := "low"
	if signal.Severity >= 0.5 {
		severityText = "high"
	} else if signal.Severity >= 0.2 {
		severityText = "medium"
	}

	_, err := t.store.ExecContext(ctx,
		`INSERT INTO abuse_events (id, actor_id, origin, channel, signal_type, severity, action_taken, detail, score)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		db.NewID(), signal.ActorID, signal.Origin, signal.Channel,
		string(signal.SignalType), severityText, string(action),
		signal.Detail, signal.Severity)
	if err != nil {
		return ActionAllow, err
	}

	return action, nil
}

// computeAction updates the in-memory score and returns the appropriate action.
func (t *AbuseTracker) computeAction(actorID string, severity float64) EnforcementAction {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	window := time.Duration(t.config.WindowMinutes) * time.Minute

	as, ok := t.scores[actorID]
	if !ok {
		// Evict oldest if at capacity.
		if len(t.scores) >= t.config.MaxTrackedActors {
			t.evictOldest()
		}
		as = &actorScore{}
		t.scores[actorID] = as
	}

	// Decay score if window has passed.
	if now.Sub(as.lastSeen) > window {
		as.score = 0
		as.signalCount = 0
	}

	as.score += severity
	as.signalCount++
	as.lastSeen = now

	// Normalize: average severity across signals in window.
	normalized := as.score / float64(as.signalCount)

	switch {
	case normalized >= t.config.QuarantineThreshold:
		return ActionQuarantine
	case normalized >= t.config.SlowdownThreshold:
		return ActionSlowdown
	default:
		return ActionAllow
	}
}

func (t *AbuseTracker) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, v := range t.scores {
		if first || v.lastSeen.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.lastSeen
			first = false
		}
	}
	if oldestKey != "" {
		delete(t.scores, oldestKey)
	}
}

// GetActorScore returns the current aggregate score for an actor.
func (t *AbuseTracker) GetActorScore(actorID string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	as, ok := t.scores[actorID]
	if !ok {
		return 0
	}

	window := time.Duration(t.config.WindowMinutes) * time.Minute
	if time.Since(as.lastSeen) > window {
		return 0
	}

	if as.signalCount == 0 {
		return 0
	}
	return as.score / float64(as.signalCount)
}

// ListRecentEvents returns recent abuse events from the database.
func (t *AbuseTracker) ListRecentEvents(ctx context.Context, limit int) ([]AbuseEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := t.store.QueryContext(ctx,
		`SELECT id, actor_id, signal_type, score, detail, action_taken, created_at
		 FROM abuse_events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []AbuseEvent
	for rows.Next() {
		var e AbuseEvent
		var createdAt string
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Signal, &e.Severity, &e.Detail, &e.Action, &createdAt); err != nil {
			continue
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		events = append(events, e)
	}
	return events, nil
}
