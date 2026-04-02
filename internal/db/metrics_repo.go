package db

import (
	"context"
)

// InferenceCostRow represents a row in the inference_costs table.
type InferenceCostRow struct {
	ID           string
	Model        string
	Provider     string
	TokensIn     int
	TokensOut    int
	Cost         float64
	Tier         string
	Cached       bool
	LatencyMs    int64
	QualityScore float64
	Escalation   bool
	TurnID       string
	CreatedAt    string
}

// MetricSnapshotRow represents a row in the metric_snapshots table.
type MetricSnapshotRow struct {
	ID          string
	MetricsJSON string
	AlertsJSON  string
	CreatedAt   string
}

// MetricsRepository handles inference cost and metric snapshot persistence.
type MetricsRepository struct {
	q Querier
}

// NewMetricsRepository creates a metrics repository.
func NewMetricsRepository(q Querier) *MetricsRepository {
	return &MetricsRepository{q: q}
}

// RecordCost inserts an inference cost record.
func (r *MetricsRepository) RecordCost(ctx context.Context, row InferenceCostRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO inference_costs
		 (id, model, provider, tokens_in, tokens_out, cost, tier, cached, latency_ms, quality_score, escalation, turn_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Model, row.Provider, row.TokensIn, row.TokensOut,
		row.Cost, nullIfEmpty(row.Tier), boolToInt(row.Cached),
		row.LatencyMs, row.QualityScore, boolToInt(row.Escalation), nullIfEmpty(row.TurnID))
	return err
}

// ListRecentCosts returns the N most recent inference cost records.
func (r *MetricsRepository) ListRecentCosts(ctx context.Context, limit int) ([]InferenceCostRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, model, provider, tokens_in, tokens_out, cost,
		        COALESCE(tier,''), cached, COALESCE(latency_ms,0), COALESCE(quality_score,0),
		        escalation, COALESCE(turn_id,''), created_at
		 FROM inference_costs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []InferenceCostRow
	for rows.Next() {
		var rc InferenceCostRow
		var cached, escalation int
		if err := rows.Scan(&rc.ID, &rc.Model, &rc.Provider, &rc.TokensIn, &rc.TokensOut, &rc.Cost,
			&rc.Tier, &cached, &rc.LatencyMs, &rc.QualityScore, &escalation, &rc.TurnID, &rc.CreatedAt); err != nil {
			return nil, err
		}
		rc.Cached = cached == 1
		rc.Escalation = escalation == 1
		result = append(result, rc)
	}
	return result, rows.Err()
}

// TotalCostByModel sums cost per model.
func (r *MetricsRepository) TotalCostByModel(ctx context.Context) (map[string]float64, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT model, SUM(cost) FROM inference_costs GROUP BY model`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make(map[string]float64)
	for rows.Next() {
		var model string
		var total float64
		if err := rows.Scan(&model, &total); err != nil {
			return nil, err
		}
		result[model] = total
	}
	return result, rows.Err()
}

// SaveSnapshot inserts a metric snapshot.
func (r *MetricsRepository) SaveSnapshot(ctx context.Context, row MetricSnapshotRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO metric_snapshots (id, metrics_json, alerts_json) VALUES (?, ?, ?)`,
		row.ID, row.MetricsJSON, nullIfEmpty(row.AlertsJSON))
	return err
}

// LatestSnapshot returns the most recent metric snapshot or nil.
func (r *MetricsRepository) LatestSnapshot(ctx context.Context) (*MetricSnapshotRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, metrics_json, COALESCE(alerts_json,''), created_at
		 FROM metric_snapshots ORDER BY created_at DESC LIMIT 1`)
	var snap MetricSnapshotRow
	err := row.Scan(&snap.ID, &snap.MetricsJSON, &snap.AlertsJSON, &snap.CreatedAt)
	if err != nil {
		return nil, nil // no rows or scan error — return nil gracefully
	}
	return &snap, nil
}

// boolToInt converts bool to SQLite integer.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
