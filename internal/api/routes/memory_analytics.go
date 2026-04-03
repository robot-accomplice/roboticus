package routes

import (
	"net/http"
	"strconv"

	"goboticus/internal/db"
)

// GetMemoryAnalytics returns memory system health metrics.
// Accepts ?hours=24 query parameter for the aggregation window.
func GetMemoryAnalytics(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hours := parseIntParam(r, "hours", 24)
		ctx := r.Context()
		offset := strconv.Itoa(-hours)

		// Total turns with context snapshots in the period.
		var totalTurns, turnsWithMemory int64
		row := store.QueryRowContext(ctx,
			`SELECT COUNT(*),
			        SUM(CASE WHEN COALESCE(memory_tokens, 0) > 0 THEN 1 ELSE 0 END)
			 FROM context_snapshots
			 WHERE created_at >= datetime('now', ? || ' hours')`, offset)
		_ = row.Scan(&totalTurns, &turnsWithMemory)

		// Hit rate.
		var hitRate float64
		if totalTurns > 0 {
			hitRate = float64(turnsWithMemory) / float64(totalTurns)
		}

		// Average budget utilization.
		var avgUtilization float64
		row = store.QueryRowContext(ctx,
			`SELECT COALESCE(
			   AVG(CAST(COALESCE(memory_tokens, 0) + COALESCE(system_prompt_tokens, 0) + COALESCE(history_tokens, 0) AS REAL)
			       / NULLIF(token_budget, 0)),
			 0)
			 FROM context_snapshots
			 WHERE created_at >= datetime('now', ? || ' hours')
			   AND token_budget > 0`, offset)
		_ = row.Scan(&avgUtilization)

		// Complexity distribution.
		complexityRows, err := store.QueryContext(ctx,
			`SELECT complexity_level, COUNT(*)
			 FROM context_snapshots
			 WHERE created_at >= datetime('now', ? || ' hours')
			 GROUP BY complexity_level ORDER BY complexity_level`, offset)
		tierDist := make(map[string]int64)
		if err == nil {
			defer func() { _ = complexityRows.Close() }()
			for complexityRows.Next() {
				var level string
				var cnt int64
				if complexityRows.Scan(&level, &cnt) == nil {
					tierDist[level] = cnt
				}
			}
		}

		// Memory ROI: average grade for turns with vs without memory.
		var avgGradeWithMem, avgGradeWithoutMem float64
		row = store.QueryRowContext(ctx,
			`SELECT COALESCE(AVG(tf.grade), 0)
			 FROM turn_feedback tf
			 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
			 WHERE cs.created_at >= datetime('now', ? || ' hours')
			   AND COALESCE(cs.memory_tokens, 0) > 0`, offset)
		_ = row.Scan(&avgGradeWithMem)

		row = store.QueryRowContext(ctx,
			`SELECT COALESCE(AVG(tf.grade), 0)
			 FROM turn_feedback tf
			 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
			 WHERE cs.created_at >= datetime('now', ? || ' hours')
			   AND COALESCE(cs.memory_tokens, 0) = 0`, offset)
		_ = row.Scan(&avgGradeWithoutMem)

		memoryROI := 0.0
		if avgGradeWithoutMem > 0 {
			memoryROI = (avgGradeWithMem - avgGradeWithoutMem) / avgGradeWithoutMem
		}

		// Memory entry counts per tier.
		var workingCount, episodicCount, semanticCount, proceduralCount, relationshipCount int64
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM working_memory`)
		_ = row.Scan(&workingCount)
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_memory`)
		_ = row.Scan(&episodicCount)
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM semantic_memory`)
		_ = row.Scan(&semanticCount)
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM procedural_memory`)
		_ = row.Scan(&proceduralCount)
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM relationship_memory`)
		_ = row.Scan(&relationshipCount)

		writeJSON(w, http.StatusOK, map[string]any{
			"period_hours":           hours,
			"total_turns":            totalTurns,
			"retrieval_hits":         turnsWithMemory,
			"hit_rate":               hitRate,
			"avg_budget_utilization": avgUtilization,
			"memory_roi":             memoryROI,
			"tier_distribution":      tierDist,
			"entry_counts": map[string]int64{
				"working":      workingCount,
				"episodic":     episodicCount,
				"semantic":     semanticCount,
				"procedural":   proceduralCount,
				"relationship": relationshipCount,
			},
		})
	}
}
