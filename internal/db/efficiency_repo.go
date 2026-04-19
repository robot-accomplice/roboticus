// Efficiency tracking — per-model cost/quality analysis and user profile building.
//
// Ported from Rust: crates/roboticus-db/src/efficiency.rs

package db

import (
	"context"
	"database/sql"
	"math"
)

// ── Structs ─────────────────────────────────────────────────────────────

// AttributionDetail breaks down cost by component.
type AttributionDetail struct {
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"cost"`
	Pct    float64 `json:"pct"`
}

// CostAttribution attributes cost to system prompt, memories, and history.
type CostAttribution struct {
	SystemPrompt AttributionDetail `json:"system_prompt"`
	Memories     AttributionDetail `json:"memories"`
	History      AttributionDetail `json:"history"`
}

// CostMetrics captures cost breakdown for a model.
type CostMetrics struct {
	Total            float64         `json:"total"`
	PerOutputToken   float64         `json:"per_output_token"`
	EffectivePerTurn float64         `json:"effective_per_turn"`
	CacheSavings     float64         `json:"cache_savings"`
	CumulativeTrend  string          `json:"cumulative_trend"`
	Attribution      CostAttribution `json:"attribution"`
	WastedBudgetCost float64         `json:"wasted_budget_cost"`
}

// TrendMetrics tracks trend direction for key metrics.
type TrendMetrics struct {
	OutputDensity string `json:"output_density"`
	CostPerTurn   string `json:"cost_per_turn"`
	CacheHitRate  string `json:"cache_hit_rate"`
}

// MemoryImpact compares quality with and without memory.
type MemoryImpact struct {
	WithMemory    float64 `json:"with_memory"`
	WithoutMemory float64 `json:"without_memory"`
}

// QualityMetrics captures quality assessment for a model.
type QualityMetrics struct {
	AvgGrade            float64            `json:"avg_grade"`
	GradeCount          int64              `json:"grade_count"`
	GradeCoverage       float64            `json:"grade_coverage"`
	CostPerQualityPoint float64            `json:"cost_per_quality_point"`
	ByComplexity        map[string]float64 `json:"by_complexity"`
	MemoryImpact        MemoryImpact       `json:"memory_impact"`
	Trend               string             `json:"trend"`
}

// ModelEfficiency aggregates efficiency metrics for a single model.
type ModelEfficiency struct {
	Model                string          `json:"model"`
	TotalTurns           int64           `json:"total_turns"`
	LastInvokedAt        string          `json:"last_invoked_at,omitempty"`
	SuccessCount         int64           `json:"success_count"`
	ErrorCount           int64           `json:"error_count"`
	AvgOutputDensity     float64         `json:"avg_output_density"`
	AvgBudgetUtilization float64         `json:"avg_budget_utilization"`
	AvgMemoryROI         float64         `json:"avg_memory_roi"`
	AvgSystemPromptWt    float64         `json:"avg_system_prompt_weight"`
	CacheHitRate         float64         `json:"cache_hit_rate"`
	ContextPressureRate  float64         `json:"context_pressure_rate"`
	Cost                 CostMetrics     `json:"cost"`
	Trend                TrendMetrics    `json:"trend"`
	Quality              *QualityMetrics `json:"quality,omitempty"`
}

// TimeSeriesPoint is a single data point in the efficiency time series.
type TimeSeriesPoint struct {
	Bucket            string  `json:"bucket"`
	Model             string  `json:"model"`
	OutputDensity     float64 `json:"output_density"`
	Cost              float64 `json:"cost"`
	Turns             int64   `json:"turns"`
	BudgetUtilization float64 `json:"budget_utilization"`
	CachedCount       int64   `json:"cached_count"`
}

// EfficiencyTotals aggregates across all models.
type EfficiencyTotals struct {
	TotalCost          float64 `json:"total_cost"`
	TotalCacheSavings  float64 `json:"total_cache_savings"`
	TotalTurns         int64   `json:"total_turns"`
	MostExpensiveModel string  `json:"most_expensive_model,omitempty"`
	MostEfficientModel string  `json:"most_efficient_model,omitempty"`
	BiggestCostDriver  string  `json:"biggest_cost_driver"`
}

// EfficiencyReport is the full efficiency analysis result.
type EfficiencyReport struct {
	Period     string                     `json:"period"`
	Models     map[string]ModelEfficiency `json:"models"`
	TimeSeries []TimeSeriesPoint          `json:"time_series"`
	Totals     EfficiencyTotals           `json:"totals"`
}

// RecommendationModelStats captures per-model stats for user profile.
type RecommendationModelStats struct {
	Turns            int64    `json:"turns"`
	AvgCost          float64  `json:"avg_cost"`
	AvgQuality       *float64 `json:"avg_quality,omitempty"`
	CacheHitRate     float64  `json:"cache_hit_rate"`
	AvgOutputDensity float64  `json:"avg_output_density"`
}

// RecommendationUserProfile aggregates user behavior across sessions.
type RecommendationUserProfile struct {
	TotalSessions       int64                               `json:"total_sessions"`
	TotalTurns          int64                               `json:"total_turns"`
	TotalCost           float64                             `json:"total_cost"`
	AvgQuality          *float64                            `json:"avg_quality,omitempty"`
	GradeCoverage       float64                             `json:"grade_coverage"`
	ModelsUsed          []string                            `json:"models_used"`
	ModelStats          map[string]RecommendationModelStats `json:"model_stats"`
	AvgSessionLength    float64                             `json:"avg_session_length"`
	AvgTokensPerTurn    float64                             `json:"avg_tokens_per_turn"`
	ToolSuccessRate     float64                             `json:"tool_success_rate"`
	CacheHitRate        float64                             `json:"cache_hit_rate"`
	MemoryRetrievalRate float64                             `json:"memory_retrieval_rate"`
}

// EfficiencyRepository computes efficiency analytics.
type EfficiencyRepository struct {
	q Querier
}

// NewEfficiencyRepository creates an efficiency repository.
func NewEfficiencyRepository(q Querier) *EfficiencyRepository {
	return &EfficiencyRepository{q: q}
}

// ── Helpers ─────────────────────────────────────────────────────────────

func round6(v float64) float64 {
	return math.Round(v*1_000_000) / 1_000_000
}

func cutoffExpr(period string) string {
	switch period {
	case "1h":
		return "datetime('now', '-1 hour')"
	case "24h":
		return "datetime('now', '-1 day')"
	case "7d":
		return "datetime('now', '-7 days')"
	case "30d":
		return "datetime('now', '-30 days')"
	default:
		return "datetime('1970-01-01')"
	}
}

func trendLabel(firstHalf, secondHalf float64) string {
	if firstHalf == 0 && secondHalf == 0 {
		return "stable"
	}
	base := math.Max(firstHalf, 0.001)
	pct := (secondHalf - firstHalf) / base
	if pct > 0.05 {
		return "increasing"
	}
	if pct < -0.05 {
		return "decreasing"
	}
	return "stable"
}

// ── rawModelRow ─────────────────────────────────────────────────────────

type rawModelRow struct {
	model          string
	totalTurns     int64
	lastInvokedAt  sql.NullString
	avgOutputDens  float64
	totalCost      float64
	totalTokensOut int64
	totalTokensIn  int64
	cachedCount    int64
	avgCostPerTurn float64
}

// ── ComputeEfficiency ───────────────────────────────────────────────────

// ComputeEfficiency computes a full efficiency report for the given period.
func (r *EfficiencyRepository) ComputeEfficiency(
	ctx context.Context,
	period string,
	modelFilter string,
) (*EfficiencyReport, error) {
	cutoff := cutoffExpr(period)

	modelClause := ""
	var modelArgs []any
	if modelFilter != "" {
		modelClause = " AND model = ?"
		modelArgs = append(modelArgs, modelFilter)
	}

	// Per-model aggregates.
	// Normalize model names: combine "qwen3.5:35b" and "ollama/qwen3.5:35b" into one row.
	mainSQL := `SELECT
		CASE WHEN model NOT LIKE '%/%' AND provider != '' THEN provider || '/' || model ELSE model END AS norm_model,
		COUNT(*) AS total_turns,
		MAX(created_at) AS last_invoked_at,
		AVG(CAST(tokens_out AS REAL) / NULLIF(tokens_in, 0)) AS avg_output_density,
		SUM(cost) AS total_cost,
		SUM(tokens_out) AS total_tokens_out,
		SUM(tokens_in) AS total_tokens_in,
		SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END) AS cached_count,
		AVG(cost) AS avg_cost_per_turn
	FROM inference_costs
	WHERE created_at >= ` + cutoff + modelClause + `
	GROUP BY norm_model
	ORDER BY total_cost DESC`

	args := make([]any, len(modelArgs))
	copy(args, modelArgs)

	rows, err := r.q.QueryContext(ctx, mainSQL, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var rawRows []rawModelRow
	for rows.Next() {
		var rm rawModelRow
		var avgOD, totalCost sql.NullFloat64
		var tokOut, tokIn, cached sql.NullInt64
		var avgCPT sql.NullFloat64
		if err := rows.Scan(&rm.model, &rm.totalTurns, &rm.lastInvokedAt,
			&avgOD, &totalCost, &tokOut, &tokIn, &cached, &avgCPT); err != nil {
			return nil, err
		}
		rm.avgOutputDens = avgOD.Float64
		rm.totalCost = totalCost.Float64
		rm.totalTokensOut = tokOut.Int64
		rm.totalTokensIn = tokIn.Int64
		rm.cachedCount = cached.Int64
		rm.avgCostPerTurn = avgCPT.Float64
		rawRows = append(rawRows, rm)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Time-series (daily buckets).
	tsSQL := `SELECT
		strftime('%Y-%m-%d', created_at) AS bucket,
		model,
		AVG(CAST(tokens_out AS REAL) / NULLIF(tokens_in, 0)) AS output_density,
		SUM(cost) AS cost,
		COUNT(*) AS turns,
		SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END) AS cached_count
	FROM inference_costs
	WHERE created_at >= ` + cutoff + modelClause + `
	GROUP BY bucket, model
	ORDER BY bucket`

	tsArgs := make([]any, len(modelArgs))
	copy(tsArgs, modelArgs)

	tsRows, err := r.q.QueryContext(ctx, tsSQL, tsArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tsRows.Close() }()

	var timeSeries []TimeSeriesPoint
	for tsRows.Next() {
		var pt TimeSeriesPoint
		var od, cost sql.NullFloat64
		var cached sql.NullInt64
		if err := tsRows.Scan(&pt.Bucket, &pt.Model, &od, &cost, &pt.Turns, &cached); err != nil {
			return nil, err
		}
		pt.OutputDensity = od.Float64
		pt.Cost = cost.Float64
		pt.CachedCount = cached.Int64
		timeSeries = append(timeSeries, pt)
	}
	if err := tsRows.Err(); err != nil {
		return nil, err
	}

	// Build per-model trend data from time series.
	modelTS := make(map[string][]TimeSeriesPoint)
	for _, pt := range timeSeries {
		modelTS[pt.Model] = append(modelTS[pt.Model], pt)
	}

	// Assemble ModelEfficiency map.
	models := make(map[string]ModelEfficiency)
	var grandTotalCost float64
	var grandTotalTurns int64
	var mostExpensive *struct {
		model string
		cost  float64
	}
	var mostEfficient *struct {
		model   string
		density float64
	}

	for _, rm := range rawRows {
		cacheHitRate := 0.0
		if rm.totalTurns > 0 {
			cacheHitRate = float64(rm.cachedCount) / float64(rm.totalTurns)
		}
		perOutputToken := 0.0
		if rm.totalTokensOut > 0 {
			perOutputToken = rm.totalCost / float64(rm.totalTokensOut)
		}

		inputFraction := 0.5
		if rm.totalTokensIn+rm.totalTokensOut > 0 {
			inputFraction = float64(rm.totalTokensIn) / float64(rm.totalTokensIn+rm.totalTokensOut)
		}
		cacheSavings := float64(rm.cachedCount) * rm.avgCostPerTurn * inputFraction

		// Trends from time-series split.
		pts := modelTS[rm.model]
		trend := TrendMetrics{OutputDensity: "stable", CostPerTurn: "stable", CacheHitRate: "stable"}
		if len(pts) >= 2 {
			mid := len(pts) / 2
			first, second := pts[:mid], pts[mid:]

			avgSlice := func(slice []TimeSeriesPoint, f func(TimeSeriesPoint) float64) float64 {
				if len(slice) == 0 {
					return 0
				}
				sum := 0.0
				for _, p := range slice {
					sum += f(p)
				}
				return sum / float64(len(slice))
			}

			trend.OutputDensity = trendLabel(
				avgSlice(first, func(p TimeSeriesPoint) float64 { return p.OutputDensity }),
				avgSlice(second, func(p TimeSeriesPoint) float64 { return p.OutputDensity }))
			trend.CostPerTurn = trendLabel(
				avgSlice(first, func(p TimeSeriesPoint) float64 {
					if p.Turns > 0 {
						return p.Cost / float64(p.Turns)
					}
					return 0
				}),
				avgSlice(second, func(p TimeSeriesPoint) float64 {
					if p.Turns > 0 {
						return p.Cost / float64(p.Turns)
					}
					return 0
				}))
			trend.CacheHitRate = trendLabel(
				avgSlice(first, func(p TimeSeriesPoint) float64 {
					if p.Turns > 0 {
						return float64(p.CachedCount) / float64(p.Turns)
					}
					return 0
				}),
				avgSlice(second, func(p TimeSeriesPoint) float64 {
					if p.Turns > 0 {
						return float64(p.CachedCount) / float64(p.Turns)
					}
					return 0
				}))
		}

		// Attribution: without context_snapshots, attribute all input to history.
		attribution := CostAttribution{
			History: AttributionDetail{
				Tokens: rm.totalTokensIn,
				Cost:   rm.totalCost * inputFraction,
				Pct:    100.0,
			},
		}

		// Quality metrics from turn_feedback.
		quality := r.computeQualityForModel(ctx, rm.model, cutoff, rm.totalTurns)

		eff := ModelEfficiency{
			Model:            rm.model,
			TotalTurns:       rm.totalTurns,
			LastInvokedAt:    rm.lastInvokedAt.String,
			SuccessCount:     rm.totalTurns,
			AvgOutputDensity: rm.avgOutputDens,
			CacheHitRate:     cacheHitRate,
			Cost: CostMetrics{
				Total:            round6(rm.totalCost),
				PerOutputToken:   round6(perOutputToken),
				EffectivePerTurn: round6(rm.avgCostPerTurn),
				CacheSavings:     round6(cacheSavings),
				CumulativeTrend:  trend.CostPerTurn,
				Attribution:      attribution,
			},
			Trend:   trend,
			Quality: quality,
		}

		grandTotalCost += rm.totalCost
		grandTotalTurns += rm.totalTurns

		if mostExpensive == nil || rm.totalCost > mostExpensive.cost {
			mostExpensive = &struct {
				model string
				cost  float64
			}{rm.model, rm.totalCost}
		}
		if mostEfficient == nil || rm.avgOutputDens > mostEfficient.density {
			mostEfficient = &struct {
				model   string
				density float64
			}{rm.model, rm.avgOutputDens}
		}

		models[rm.model] = eff
	}

	totalCacheSavings := 0.0
	for _, m := range models {
		totalCacheSavings += m.Cost.CacheSavings
	}

	biggestCostDriver := "none"
	expModel := ""
	effModel := ""
	if mostExpensive != nil {
		biggestCostDriver = mostExpensive.model
		expModel = mostExpensive.model
	}
	if mostEfficient != nil {
		effModel = mostEfficient.model
	}

	return &EfficiencyReport{
		Period:     period,
		Models:     models,
		TimeSeries: timeSeries,
		Totals: EfficiencyTotals{
			TotalCost:          round6(grandTotalCost),
			TotalCacheSavings:  round6(totalCacheSavings),
			TotalTurns:         grandTotalTurns,
			MostExpensiveModel: expModel,
			MostEfficientModel: effModel,
			BiggestCostDriver:  biggestCostDriver,
		},
	}, nil
}

// computeQualityForModel queries turn_feedback for quality metrics.
func (r *EfficiencyRepository) computeQualityForModel(
	ctx context.Context,
	model, cutoff string,
	totalTurns int64,
) *QualityMetrics {
	qualitySQL := `SELECT tf.grade, t.cost
		FROM turn_feedback tf
		JOIN turns t ON t.id = tf.turn_id
		WHERE t.model = ? AND tf.created_at >= ` + cutoff

	rows, err := r.q.QueryContext(ctx, qualitySQL, model)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	type gradeRow struct {
		grade int
		cost  float64
	}
	var gradeRows []gradeRow
	for rows.Next() {
		var g gradeRow
		var cost sql.NullFloat64
		if err := rows.Scan(&g.grade, &cost); err != nil {
			continue
		}
		g.cost = cost.Float64
		gradeRows = append(gradeRows, g)
	}

	if len(gradeRows) == 0 {
		return nil
	}

	gradeCount := int64(len(gradeRows))
	sumGrade := int64(0)
	totalCost := 0.0
	totalQuality := 0.0
	for _, g := range gradeRows {
		sumGrade += int64(g.grade)
		totalCost += g.cost
		totalQuality += float64(g.grade)
	}

	avgGrade := float64(sumGrade) / float64(gradeCount)
	gradeCoverage := 0.0
	if totalTurns > 0 {
		gradeCoverage = float64(gradeCount) / float64(totalTurns)
	}
	costPerQP := 0.0
	if totalQuality > 0 {
		costPerQP = totalCost / totalQuality
	}

	trend := "stable"
	if len(gradeRows) >= 4 {
		half := len(gradeRows) / 2
		firstSum := 0.0
		for _, g := range gradeRows[:half] {
			firstSum += float64(g.grade)
		}
		secondSum := 0.0
		for _, g := range gradeRows[half:] {
			secondSum += float64(g.grade)
		}
		trend = trendLabel(firstSum/float64(half), secondSum/float64(len(gradeRows)-half))
	}

	return &QualityMetrics{
		AvgGrade:            avgGrade,
		GradeCount:          gradeCount,
		GradeCoverage:       gradeCoverage,
		CostPerQualityPoint: costPerQP,
		ByComplexity:        make(map[string]float64),
		MemoryImpact:        MemoryImpact{},
		Trend:               trend,
	}
}

// ── BuildUserProfile ────────────────────────────────────────────────────

// BuildUserProfile aggregates user behavior for recommendation engine.
func (r *EfficiencyRepository) BuildUserProfile(
	ctx context.Context,
	period string,
) (*RecommendationUserProfile, error) {
	cutoff := cutoffExpr(period)

	// Session stats.
	var totalSessions int64
	var avgSessionLength float64
	_ = r.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(AVG(msg_count), 0) FROM (
		   SELECT s.id, COUNT(m.id) AS msg_count
		   FROM sessions s
		   LEFT JOIN session_messages m ON m.session_id = s.id
		   WHERE s.created_at >= `+cutoff+`
		   GROUP BY s.id
		 )`).Scan(&totalSessions, &avgSessionLength)

	// Inference cost stats.
	var totalTurns int64
	var totalCost, avgTokensPerTurn, cacheHitRate float64
	_ = r.q.QueryRowContext(ctx,
		`SELECT
		   COUNT(*),
		   COALESCE(SUM(cost), 0),
		   COALESCE(AVG(tokens_in + tokens_out), 0),
		   CASE WHEN COUNT(*) > 0
		     THEN CAST(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END) AS REAL) / COUNT(*)
		     ELSE 0.0 END
		 FROM inference_costs
		 WHERE created_at >= `+cutoff).Scan(&totalTurns, &totalCost, &avgTokensPerTurn, &cacheHitRate)

	// Per-model stats.
	modelRows, err := r.q.QueryContext(ctx,
		`SELECT
		   CASE WHEN model NOT LIKE '%/%' AND provider != '' THEN provider || '/' || model ELSE model END AS model,
		   COUNT(*) AS turns,
		   AVG(cost) AS avg_cost,
		   CASE WHEN COUNT(*) > 0
		     THEN CAST(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END) AS REAL) / COUNT(*)
		     ELSE 0.0 END AS cache_rate,
		   AVG(CAST(tokens_out AS REAL) / NULLIF(tokens_in, 0)) AS avg_density
		 FROM inference_costs
		 WHERE created_at >= `+cutoff+`
		 GROUP BY 1
		 ORDER BY turns DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = modelRows.Close() }()

	var modelsUsed []string
	modelStats := make(map[string]RecommendationModelStats)
	for modelRows.Next() {
		var model string
		var turns int64
		var avgCost, cacheRate float64
		var avgDensity sql.NullFloat64
		if err := modelRows.Scan(&model, &turns, &avgCost, &cacheRate, &avgDensity); err != nil {
			continue
		}
		modelsUsed = append(modelsUsed, model)
		modelStats[model] = RecommendationModelStats{
			Turns:            turns,
			AvgCost:          avgCost,
			CacheHitRate:     cacheRate,
			AvgOutputDensity: avgDensity.Float64,
		}
	}

	// Tool success rate.
	var toolSuccessRate float64
	_ = r.q.QueryRowContext(ctx,
		`SELECT CASE WHEN COUNT(*) > 0
		   THEN CAST(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS REAL) / COUNT(*)
		   ELSE 1.0 END
		 FROM tool_calls WHERE created_at >= `+cutoff).Scan(&toolSuccessRate)

	// Quality from turn_feedback.
	var gradedTurns int64
	var avgQuality sql.NullFloat64
	_ = r.q.QueryRowContext(ctx,
		`SELECT COUNT(*), AVG(CAST(tf.grade AS REAL))
		 FROM turn_feedback tf
		 JOIN turns t ON t.id = tf.turn_id
		 JOIN sessions s ON s.id = t.session_id
		 WHERE s.created_at >= `+cutoff).Scan(&gradedTurns, &avgQuality)

	gradeCoverage := 0.0
	if totalTurns > 0 {
		gradeCoverage = float64(gradedTurns) / float64(totalTurns)
	}

	// Memory retrieval rate.
	memoryRetrievalRate := 0.5
	_ = r.q.QueryRowContext(ctx,
		`SELECT CASE WHEN COUNT(*) > 0
		   THEN CAST(SUM(CASE WHEN memory_tokens > 0 THEN 1 ELSE 0 END) AS REAL) / COUNT(*)
		   ELSE 0.5 END
		 FROM context_snapshots WHERE created_at >= `+cutoff).Scan(&memoryRetrievalRate)

	var qualityPtr *float64
	if avgQuality.Valid {
		qualityPtr = &avgQuality.Float64
	}

	return &RecommendationUserProfile{
		TotalSessions:       totalSessions,
		TotalTurns:          totalTurns,
		TotalCost:           totalCost,
		AvgQuality:          qualityPtr,
		GradeCoverage:       gradeCoverage,
		ModelsUsed:          modelsUsed,
		ModelStats:          modelStats,
		AvgSessionLength:    avgSessionLength,
		AvgTokensPerTurn:    avgTokensPerTurn,
		ToolSuccessRate:     toolSuccessRate,
		CacheHitRate:        cacheHitRate,
		MemoryRetrievalRate: memoryRetrievalRate,
	}, nil
}
