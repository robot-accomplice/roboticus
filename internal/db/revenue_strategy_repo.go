package db

import (
	"context"
)

// StrategyStat summarizes strategy performance.
type StrategyStat struct {
	Strategy string
	Count    int
	AvgScore float64
}

// RevenueStrategyRepository handles revenue strategy analytics.
type RevenueStrategyRepository struct {
	q Querier
}

// NewRevenueStrategyRepository creates a revenue strategy repository.
func NewRevenueStrategyRepository(q Querier) *RevenueStrategyRepository {
	return &RevenueStrategyRepository{q: q}
}

// StrategySummary returns count and average priority score grouped by strategy.
func (r *RevenueStrategyRepository) StrategySummary(ctx context.Context) ([]StrategyStat, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT strategy, COUNT(*) AS cnt, COALESCE(AVG(priority_score), 0.0) AS avg_score
		 FROM revenue_opportunities GROUP BY strategy ORDER BY avg_score DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []StrategyStat
	for rows.Next() {
		var s StrategyStat
		if err := rows.Scan(&s.Strategy, &s.Count, &s.AvgScore); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
