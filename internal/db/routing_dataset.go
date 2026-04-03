package db

import (
	"context"
	"database/sql"
)

// QueryFeatures holds the feature vector for a routing decision.
type QueryFeatures struct {
	TokenCount    int     `json:"token_count"`
	Complexity    float64 `json:"complexity"`
	HasToolCalls  bool    `json:"has_tool_calls"`
	SessionDepth  int     `json:"session_depth"`
	Channel       string  `json:"channel"`
}

// RoutingExample is a stored routing prediction for ML training.
type RoutingExample struct {
	ID                   string        `json:"id"`
	TurnID               string        `json:"turn_id"`
	ProductionModel      string        `json:"production_model"`
	ShadowModel          *string       `json:"shadow_model,omitempty"`
	ProductionComplexity *float64      `json:"production_complexity,omitempty"`
	ShadowComplexity     *float64      `json:"shadow_complexity,omitempty"`
	Agreed               bool          `json:"agreed"`
	DetailJSON           *string       `json:"detail_json,omitempty"`
	CreatedAt            string        `json:"created_at"`
}

// RoutingDatasetRepo handles routing prediction data.
type RoutingDatasetRepo struct {
	q Querier
}

// NewRoutingDatasetRepo creates a routing dataset repository.
func NewRoutingDatasetRepo(q Querier) *RoutingDatasetRepo {
	return &RoutingDatasetRepo{q: q}
}

// SaveRoutingExample inserts a routing prediction example into shadow_routing_predictions.
func (r *RoutingDatasetRepo) SaveRoutingExample(ctx context.Context, turnID, productionModel string, productionComplexity, shadowComplexity float64, shadowModel string, agreed bool, detailJSON string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO shadow_routing_predictions
		 (id, turn_id, production_model, shadow_model, production_complexity, shadow_complexity, agreed, detail_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		NewID(), turnID, productionModel, shadowModel, productionComplexity, shadowComplexity, boolToInt(agreed), detailJSON)
	return err
}

// ListRoutingExamples queries recent routing prediction examples.
func (r *RoutingDatasetRepo) ListRoutingExamples(ctx context.Context, limit int) ([]RoutingExample, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.q.QueryContext(ctx,
		`SELECT id, turn_id, production_model, shadow_model, production_complexity, shadow_complexity, agreed, detail_json, created_at
		 FROM shadow_routing_predictions
		 ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []RoutingExample
	for rows.Next() {
		var ex RoutingExample
		var shadowModel, detailJSON sql.NullString
		var prodComplexity, shadowComplexity sql.NullFloat64
		var agreed int
		if err := rows.Scan(&ex.ID, &ex.TurnID, &ex.ProductionModel, &shadowModel,
			&prodComplexity, &shadowComplexity, &agreed, &detailJSON, &ex.CreatedAt); err != nil {
			continue
		}
		if shadowModel.Valid {
			ex.ShadowModel = &shadowModel.String
		}
		if prodComplexity.Valid {
			ex.ProductionComplexity = &prodComplexity.Float64
		}
		if shadowComplexity.Valid {
			ex.ShadowComplexity = &shadowComplexity.Float64
		}
		ex.Agreed = agreed != 0
		if detailJSON.Valid {
			ex.DetailJSON = &detailJSON.String
		}
		results = append(results, ex)
	}
	return results, rows.Err()
}

