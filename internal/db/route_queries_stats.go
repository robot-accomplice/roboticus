package db

import (
	"context"
	"database/sql"
)

// --- Inference Costs ---

// ListInferenceCosts returns recent inference costs.
func (rq *RouteQueries) ListInferenceCosts(ctx context.Context, hours, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, model, provider, tokens_in, tokens_out, cost, latency_ms, quality_score, escalation, turn_id, cached, created_at
		 FROM inference_costs WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 ORDER BY created_at DESC LIMIT ?`, hours, limit)
}

// TotalCostSince returns the total inference cost since the given hours ago.
func (rq *RouteQueries) TotalCostSince(ctx context.Context, hours int) (float64, error) {
	var total float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0) FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')`, hours).Scan(&total)
	return total, err
}

// --- Stats ---

// CostsByHour returns cost aggregation by hour.
func (rq *RouteQueries) CostsByHour(ctx context.Context, hours int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%d %H:00', created_at) as hour,
		        SUM(cost) as total_cost, SUM(tokens_in) as total_in, SUM(tokens_out) as total_out, COUNT(*) as calls
		 FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 GROUP BY hour ORDER BY hour`, hours)
}

// CostsByModel returns cost aggregation by model.
func (rq *RouteQueries) CostsByModel(ctx context.Context, hours int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT model, SUM(cost) as total_cost, SUM(tokens_in) as total_in, SUM(tokens_out) as total_out, COUNT(*) as calls
		 FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 GROUP BY model ORDER BY total_cost DESC`, hours)
}

// CountRow returns a single integer count for a query.
func (rq *RouteQueries) CountRow(ctx context.Context, query string, args ...any) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// --- Stats / Costs ---

// ListCostRows returns recent inference cost rows for the dashboard.
func (rq *RouteQueries) ListCostRows(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, model, provider, cost, tokens_in, tokens_out, created_at, cached, COALESCE(latency_ms, 0)
		 FROM inference_costs ORDER BY created_at DESC LIMIT ?`, limit)
}

// CacheStats returns cache entry count, total inference count, and cached inference count.
func (rq *RouteQueries) CacheStats(ctx context.Context) (cacheCount, totalInferences, cachedInferences int64, err error) {
	err = rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM semantic_cache`).Scan(&cacheCount)
	if err != nil {
		return
	}
	err = rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0) FROM inference_costs`).
		Scan(&totalInferences, &cachedInferences)
	return
}

// ListFinancialTransactions returns recent financial transactions.
func (rq *RouteQueries) ListFinancialTransactions(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, tx_type, amount, currency, counterparty, tx_hash, created_at
		 FROM transactions ORDER BY created_at DESC LIMIT ?`, limit)
}

// EfficiencyMetrics returns aggregate inference metrics for a time window.
func (rq *RouteQueries) EfficiencyMetrics(ctx context.Context, offset string) (totalTokens, count, cachedCount int64, totalCost, avgLatency float64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(tokens_in + tokens_out), 0),
		        COALESCE(SUM(cost), 0),
		        COALESCE(AVG(latency_ms), 0),
		        COUNT(*),
		        COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0)
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' hours')`, offset).
		Scan(&totalTokens, &totalCost, &avgLatency, &count, &cachedCount)
	return
}

// ModelCostBreakdown returns per-model cost breakdown for a time window.
func (rq *RouteQueries) ModelCostBreakdown(ctx context.Context, offset string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT model, COUNT(*), COALESCE(SUM(cost), 0), COALESCE(SUM(tokens_in), 0), COALESCE(SUM(tokens_out), 0)
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' hours')
		 GROUP BY model ORDER BY COUNT(*) DESC`, offset)
}

// ModelQualityBreakdown returns per-model quality/efficacy aggregates.
func (rq *RouteQueries) ModelQualityBreakdown(ctx context.Context, offset string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT model,
		        COALESCE(AVG(quality_score), 0),
		        SUM(CASE WHEN quality_score IS NOT NULL THEN 1 ELSE 0 END),
		        SUM(CASE WHEN escalation = 1 THEN 1 ELSE 0 END),
		        COALESCE(AVG(latency_ms), 0)
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' hours')
		 GROUP BY model`, offset)
}

// ListModelSelectionEvents returns recent model selection events with candidates.
func (rq *RouteQueries) ListModelSelectionEvents(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, session_id, selected_model, strategy, complexity, candidates_json, created_at
		 FROM model_selection_events ORDER BY created_at DESC LIMIT ?`, limit)
}

// RecommendationMetrics returns aggregate metrics for generating recommendations.
func (rq *RouteQueries) RecommendationMetrics(ctx context.Context, offset string) (requests, cachedCount, totalTokens int64, totalCost, avgLatency float64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(cost), 0),
		        COALESCE(AVG(latency_ms), 0),
		        COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(tokens_in + tokens_out), 0)
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' hours')`, offset).
		Scan(&requests, &totalCost, &avgLatency, &cachedCount, &totalTokens)
	return
}

// CostTimeseries returns hourly cost/token/request buckets.
func (rq *RouteQueries) CostTimeseries(ctx context.Context, days int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%dT%H:00:00', created_at) as bucket,
		        COUNT(*) as requests, COALESCE(SUM(cost), 0) as cost,
		        COALESCE(SUM(tokens_in + tokens_out), 0) as tokens
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' days')
		 GROUP BY bucket ORDER BY bucket`, -days)
}
