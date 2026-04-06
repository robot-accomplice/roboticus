package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// QueryFeatures holds the feature vector for a routing decision.
type QueryFeatures struct {
	TokenCount   int     `json:"token_count"`
	Complexity   float64 `json:"complexity"`
	HasToolCalls bool    `json:"has_tool_calls"`
	SessionDepth int     `json:"session_depth"`
	Channel      string  `json:"channel"`
}

// RoutingExample is a stored routing prediction for ML training.
type RoutingExample struct {
	ID                   string   `json:"id"`
	TurnID               string   `json:"turn_id"`
	ProductionModel      string   `json:"production_model"`
	ShadowModel          *string  `json:"shadow_model,omitempty"`
	ProductionComplexity *float64 `json:"production_complexity,omitempty"`
	ShadowComplexity     *float64 `json:"shadow_complexity,omitempty"`
	Agreed               bool     `json:"agreed"`
	DetailJSON           *string  `json:"detail_json,omitempty"`
	CreatedAt            string   `json:"created_at"`
}

// DatasetFilter scopes routing dataset extraction.
type DatasetFilter struct {
	Since         string
	Until         string
	SchemaVersion *int
	Limit         int
}

// RoutingDatasetRow joins routing decisions with their observed inference outcomes.
type RoutingDatasetRow struct {
	EventID         string   `json:"event_id"`
	TurnID          string   `json:"turn_id"`
	SessionID       string   `json:"session_id"`
	AgentID         string   `json:"agent_id"`
	Channel         string   `json:"channel"`
	SelectedModel   string   `json:"selected_model"`
	Strategy        string   `json:"strategy"`
	PrimaryModel    string   `json:"primary_model"`
	OverrideModel   *string  `json:"override_model,omitempty"`
	Complexity      *string  `json:"complexity,omitempty"`
	UserExcerpt     string   `json:"user_excerpt"`
	CandidatesJSON  string   `json:"candidates_json"`
	Attribution     *string  `json:"attribution,omitempty"`
	MetascoreJSON   *string  `json:"metascore_json,omitempty"`
	FeaturesJSON    *string  `json:"features_json,omitempty"`
	SchemaVersion   int      `json:"schema_version"`
	DecisionAt      string   `json:"decision_at"`
	TotalTokensIn   int64    `json:"total_tokens_in"`
	TotalTokensOut  int64    `json:"total_tokens_out"`
	TotalCost       float64  `json:"total_cost"`
	InferenceCount  int64    `json:"inference_count"`
	AnyCached       bool     `json:"any_cached"`
	AvgLatencyMS    *float64 `json:"avg_latency_ms,omitempty"`
	AvgQualityScore *float64 `json:"avg_quality_score,omitempty"`
	AnyEscalation   bool     `json:"any_escalation"`
}

// DatasetSummary describes a routing dataset extract.
type DatasetSummary struct {
	TotalRows          int     `json:"total_rows"`
	DistinctModels     int     `json:"distinct_models"`
	DistinctStrategies int     `json:"distinct_strategies"`
	TotalCost          float64 `json:"total_cost"`
	AvgCostPerDecision float64 `json:"avg_cost_per_decision"`
	SchemaVersions     []int   `json:"schema_versions"`
}

// RoutingDatasetRepo handles routing prediction data.
type RoutingDatasetRepo struct {
	q Querier
}

// NewRoutingDatasetRepo creates a routing dataset repository.
func NewRoutingDatasetRepo(q Querier) *RoutingDatasetRepo {
	return &RoutingDatasetRepo{q: q}
}

// ExtractRoutingDataset joins model selection events with inference costs into
// a flat, exportable dataset for offline replay and operator inspection.
func (r *RoutingDatasetRepo) ExtractRoutingDataset(ctx context.Context, filter DatasetFilter) ([]RoutingDatasetRow, error) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if filter.Since != "" {
		clauses = append(clauses, "mse.created_at >= ?")
		args = append(args, filter.Since)
	}
	if filter.Until != "" {
		clauses = append(clauses, "mse.created_at < ?")
		args = append(args, filter.Until)
	}
	if filter.SchemaVersion != nil {
		clauses = append(clauses, "mse.schema_version = ?")
		args = append(args, *filter.SchemaVersion)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 10000
	}
	if limit > 50000 {
		limit = 50000
	}

	var where string
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT
			mse.id,
			mse.turn_id,
			mse.session_id,
			mse.agent_id,
			mse.channel,
			mse.selected_model,
			mse.strategy,
			mse.primary_model,
			mse.override_model,
			mse.complexity,
			mse.user_excerpt,
			mse.candidates_json,
			mse.attribution,
			mse.metascore_json,
			mse.features_json,
			mse.schema_version,
			mse.created_at,
			COALESCE(SUM(ic.tokens_in), 0) AS total_tokens_in,
			COALESCE(SUM(ic.tokens_out), 0) AS total_tokens_out,
			COALESCE(SUM(ic.cost), 0.0) AS total_cost,
			COUNT(ic.id) AS inference_count,
			COALESCE(MAX(ic.cached), 0) AS any_cached,
			AVG(ic.latency_ms) AS avg_latency_ms,
			AVG(ic.quality_score) AS avg_quality_score,
			COALESCE(MAX(ic.escalation), 0) AS any_escalation
		FROM model_selection_events mse
		INNER JOIN inference_costs ic ON ic.turn_id = mse.turn_id
		%s
		GROUP BY mse.id
		ORDER BY mse.created_at ASC
		LIMIT ?`, where)

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]RoutingDatasetRow, 0)
	for rows.Next() {
		var row RoutingDatasetRow
		var overrideModel, complexity, attribution, metascoreJSON, featuresJSON sql.NullString
		var avgLatencyMS, avgQualityScore sql.NullFloat64
		var anyCached, anyEscalation int
		if err := rows.Scan(
			&row.EventID,
			&row.TurnID,
			&row.SessionID,
			&row.AgentID,
			&row.Channel,
			&row.SelectedModel,
			&row.Strategy,
			&row.PrimaryModel,
			&overrideModel,
			&complexity,
			&row.UserExcerpt,
			&row.CandidatesJSON,
			&attribution,
			&metascoreJSON,
			&featuresJSON,
			&row.SchemaVersion,
			&row.DecisionAt,
			&row.TotalTokensIn,
			&row.TotalTokensOut,
			&row.TotalCost,
			&row.InferenceCount,
			&anyCached,
			&avgLatencyMS,
			&avgQualityScore,
			&anyEscalation,
		); err != nil {
			return nil, err
		}
		if overrideModel.Valid {
			row.OverrideModel = &overrideModel.String
		}
		if complexity.Valid {
			row.Complexity = &complexity.String
		}
		if attribution.Valid {
			row.Attribution = &attribution.String
		}
		if metascoreJSON.Valid {
			row.MetascoreJSON = &metascoreJSON.String
		}
		if featuresJSON.Valid {
			row.FeaturesJSON = &featuresJSON.String
		}
		if avgLatencyMS.Valid {
			row.AvgLatencyMS = &avgLatencyMS.Float64
		}
		if avgQualityScore.Valid {
			row.AvgQualityScore = &avgQualityScore.Float64
		}
		row.AnyCached = anyCached != 0
		row.AnyEscalation = anyEscalation != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// SummarizeRoutingDataset computes summary statistics for the extracted dataset.
func (r *RoutingDatasetRepo) SummarizeRoutingDataset(ctx context.Context, filter DatasetFilter) (DatasetSummary, error) {
	filter.Limit = 0
	rows, err := r.ExtractRoutingDataset(ctx, filter)
	if err != nil {
		return DatasetSummary{}, err
	}
	if len(rows) == 0 {
		return DatasetSummary{SchemaVersions: []int{}}, nil
	}

	models := make(map[string]struct{})
	strategies := make(map[string]struct{})
	schemaVersions := make(map[int]struct{})
	totalCost := 0.0
	for _, row := range rows {
		models[row.SelectedModel] = struct{}{}
		strategies[row.Strategy] = struct{}{}
		schemaVersions[row.SchemaVersion] = struct{}{}
		totalCost += row.TotalCost
	}
	versions := make([]int, 0, len(schemaVersions))
	for version := range schemaVersions {
		versions = append(versions, version)
	}
	slicesSortInts(versions)

	return DatasetSummary{
		TotalRows:          len(rows),
		DistinctModels:     len(models),
		DistinctStrategies: len(strategies),
		TotalCost:          totalCost,
		AvgCostPerDecision: totalCost / float64(len(rows)),
		SchemaVersions:     versions,
	}, nil
}

func slicesSortInts(values []int) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
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
