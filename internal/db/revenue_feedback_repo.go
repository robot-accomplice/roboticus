package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FeedbackRow represents a simplified row in revenue_feedback.
type FeedbackRow struct {
	ID            string
	OpportunityID string
	Grade         string
	Notes         string
	CreatedAt     string
}

// RevenueFeedbackRepository handles revenue feedback persistence.
type RevenueFeedbackRepository struct {
	q Querier
}

// NewRevenueFeedbackRepository creates a revenue feedback repository.
func NewRevenueFeedbackRepository(q Querier) *RevenueFeedbackRepository {
	return &RevenueFeedbackRepository{q: q}
}

// RecordFeedback inserts a revenue feedback entry.
func (r *RevenueFeedbackRepository) RecordFeedback(ctx context.Context, oppID, grade, notes string) error {
	id := fmt.Sprintf("fb-%d", time.Now().UnixNano())
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO revenue_feedback (id, opportunity_id, strategy, grade, source, comment)
		 VALUES (?, ?, '', ?, 'api', ?)`,
		id, oppID, grade, notes,
	)
	return err
}

// ListByOpportunity returns all feedback for a given opportunity.
func (r *RevenueFeedbackRepository) ListByOpportunity(ctx context.Context, oppID string) ([]FeedbackRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, opportunity_id, CAST(grade AS TEXT), comment, created_at
		 FROM revenue_feedback WHERE opportunity_id = ? ORDER BY created_at DESC`,
		oppID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []FeedbackRow
	for rows.Next() {
		var fb FeedbackRow
		var notes sql.NullString
		if err := rows.Scan(&fb.ID, &fb.OpportunityID, &fb.Grade, &notes, &fb.CreatedAt); err != nil {
			return nil, err
		}
		if notes.Valid {
			fb.Notes = notes.String
		}
		result = append(result, fb)
	}
	return result, rows.Err()
}
