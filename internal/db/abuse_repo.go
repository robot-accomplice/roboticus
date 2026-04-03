package db

import (
	"context"
	"fmt"
	"time"
)

// AbuseEvent represents a row in abuse_events.
type AbuseEvent struct {
	ID          string
	ActorID     string
	Origin      string
	Channel     string
	SignalType  string
	Severity    string
	ActionTaken string
	Detail      string
	Score       float64
	CreatedAt   string
}

// AbuseRepository handles abuse event persistence.
type AbuseRepository struct {
	q Querier
}

// NewAbuseRepository creates an abuse repository.
func NewAbuseRepository(q Querier) *AbuseRepository {
	return &AbuseRepository{q: q}
}

// RecordAbuse inserts an abuse event.
func (r *AbuseRepository) RecordAbuse(ctx context.Context, actorID, origin, signalType, score, action string) error {
	id := fmt.Sprintf("abuse-%d", time.Now().UnixNano())
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO abuse_events (id, actor_id, origin, channel, signal_type, severity, action_taken, score)
		 VALUES (?, ?, ?, ?, ?, 'medium', ?, ?)`,
		id, actorID, origin, origin, signalType, action, score,
	)
	return err
}

// ListRecentAbuse returns the most recent abuse events.
func (r *AbuseRepository) ListRecentAbuse(ctx context.Context, limit int) ([]AbuseEvent, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, actor_id, origin, channel, signal_type, severity, action_taken, COALESCE(detail,''), score, created_at
		 FROM abuse_events ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []AbuseEvent
	for rows.Next() {
		var e AbuseEvent
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Origin, &e.Channel, &e.SignalType, &e.Severity, &e.ActionTaken, &e.Detail, &e.Score, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// CountByActor returns the number of abuse events for a given actor since a duration ago.
func (r *AbuseRepository) CountByActor(ctx context.Context, actorID string, since time.Duration) (int, error) {
	cutoff := time.Now().Add(-since).UTC().Format(time.RFC3339)
	row := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM abuse_events WHERE actor_id = ? AND created_at > ?`,
		actorID, cutoff,
	)
	var count int
	err := row.Scan(&count)
	return count, err
}
