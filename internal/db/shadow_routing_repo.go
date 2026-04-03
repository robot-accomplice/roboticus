package db

import (
	"context"
	"fmt"
	"time"
)

// ShadowRoutingRepository handles shadow routing prediction persistence.
type ShadowRoutingRepository struct {
	q Querier
}

// NewShadowRoutingRepository creates a shadow routing repository.
func NewShadowRoutingRepository(q Querier) *ShadowRoutingRepository {
	return &ShadowRoutingRepository{q: q}
}

// SavePrediction inserts a shadow routing prediction.
func (r *ShadowRoutingRepository) SavePrediction(ctx context.Context, turnID, prodModel, shadowModel string, prodComplexity, shadowComplexity float64, agreed bool) error {
	id := fmt.Sprintf("sr-%d", time.Now().UnixNano())
	agreedInt := 0
	if agreed {
		agreedInt = 1
	}
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO shadow_routing_predictions (id, turn_id, production_model, shadow_model, production_complexity, shadow_complexity, agreed)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, turnID, prodModel, shadowModel, prodComplexity, shadowComplexity, agreedInt,
	)
	return err
}

// AgreementRate returns the percentage of predictions that agreed, over the last N rows.
func (r *ShadowRoutingRepository) AgreementRate(ctx context.Context, limit int) (float64, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT COALESCE(AVG(agreed) * 100.0, 0.0)
		 FROM (SELECT agreed FROM shadow_routing_predictions ORDER BY created_at DESC LIMIT ?)`,
		limit,
	)
	var rate float64
	err := row.Scan(&rate)
	return rate, err
}
