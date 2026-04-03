package db

import (
	"context"
)

// RevenueIntrospectionRepository handles revenue analytics queries.
type RevenueIntrospectionRepository struct {
	q Querier
}

// NewRevenueIntrospectionRepository creates a revenue introspection repository.
func NewRevenueIntrospectionRepository(q Querier) *RevenueIntrospectionRepository {
	return &RevenueIntrospectionRepository{q: q}
}

// OpportunitySummary returns the count of opportunities grouped by status.
func (r *RevenueIntrospectionRepository) OpportunitySummary(ctx context.Context) (map[string]int, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM revenue_opportunities GROUP BY status`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[status] = count
	}
	return result, rows.Err()
}

// RevenueByStrategy returns the sum of estimated value (expected_revenue_usdc) grouped by strategy.
func (r *RevenueIntrospectionRepository) RevenueByStrategy(ctx context.Context) (map[string]float64, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT strategy, COALESCE(SUM(expected_revenue_usdc), 0.0)
		 FROM revenue_opportunities GROUP BY strategy`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]float64)
	for rows.Next() {
		var strategy string
		var total float64
		if err := rows.Scan(&strategy, &total); err != nil {
			return nil, err
		}
		result[strategy] = total
	}
	return result, rows.Err()
}
