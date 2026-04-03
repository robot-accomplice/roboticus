package db

import (
	"context"
)

// ScoredOpportunity represents a revenue opportunity with its priority score.
type ScoredOpportunity struct {
	ID            string
	Strategy      string
	Status        string
	PriorityScore float64
}

// RevenueScoringRepository handles revenue opportunity scoring.
type RevenueScoringRepository struct {
	q Querier
}

// NewRevenueScoringRepository creates a revenue scoring repository.
func NewRevenueScoringRepository(q Querier) *RevenueScoringRepository {
	return &RevenueScoringRepository{q: q}
}

// UpdateScore updates the priority score for an opportunity.
func (r *RevenueScoringRepository) UpdateScore(ctx context.Context, oppID string, score float64) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE revenue_opportunities SET priority_score = ?, updated_at = datetime('now') WHERE id = ?`,
		score, oppID,
	)
	return err
}

// TopScored returns the highest-scored opportunities.
func (r *RevenueScoringRepository) TopScored(ctx context.Context, limit int) ([]ScoredOpportunity, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, strategy, status, priority_score
		 FROM revenue_opportunities ORDER BY priority_score DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []ScoredOpportunity
	for rows.Next() {
		var s ScoredOpportunity
		if err := rows.Scan(&s.ID, &s.Strategy, &s.Status, &s.PriorityScore); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
