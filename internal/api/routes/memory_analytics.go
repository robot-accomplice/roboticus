package routes

import (
	"net/http"
	"strconv"

	"roboticus/internal/db"
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
			        COALESCE(SUM(CASE WHEN COALESCE(memory_tokens, 0) > 0 THEN 1 ELSE 0 END), 0)
			 FROM context_snapshots
			 WHERE created_at >= datetime('now', ? || ' hours')`, offset)
		if err := row.Scan(&totalTurns, &turnsWithMemory); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory analytics totals")
			return
		}

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
		if err := row.Scan(&avgUtilization); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory utilization")
			return
		}

		// Complexity distribution.
		complexityRows, err := store.QueryContext(ctx,
			`SELECT complexity_level, COUNT(*)
			 FROM context_snapshots
			 WHERE created_at >= datetime('now', ? || ' hours')
			 GROUP BY complexity_level ORDER BY complexity_level`, offset)
		tierDist := make(map[string]int64)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory complexity distribution")
			return
		}
		defer func() { _ = complexityRows.Close() }()
		for complexityRows.Next() {
			var level string
			var cnt int64
			if err := complexityRows.Scan(&level, &cnt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read memory complexity row")
				return
			}
			tierDist[level] = cnt
		}

		// Memory ROI: average grade for turns with vs without memory.
		var avgGradeWithMem, avgGradeWithoutMem float64
		row = store.QueryRowContext(ctx,
			`SELECT COALESCE(AVG(tf.grade), 0)
			 FROM turn_feedback tf
			 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
			 WHERE cs.created_at >= datetime('now', ? || ' hours')
			   AND COALESCE(cs.memory_tokens, 0) > 0`, offset)
		if err := row.Scan(&avgGradeWithMem); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory ROI with memory")
			return
		}

		row = store.QueryRowContext(ctx,
			`SELECT COALESCE(AVG(tf.grade), 0)
			 FROM turn_feedback tf
			 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
			 WHERE cs.created_at >= datetime('now', ? || ' hours')
			   AND COALESCE(cs.memory_tokens, 0) = 0`, offset)
		if err := row.Scan(&avgGradeWithoutMem); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory ROI without memory")
			return
		}

		memoryROI := 0.0
		if avgGradeWithoutMem > 0 {
			memoryROI = (avgGradeWithMem - avgGradeWithoutMem) / avgGradeWithoutMem
		}

		// Memory entry counts per tier.
		var workingCount, episodicCount, semanticCount, proceduralCount, relationshipCount int64
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM working_memory`)
		if err := row.Scan(&workingCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query working memory count")
			return
		}
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_memory`)
		if err := row.Scan(&episodicCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query episodic memory count")
			return
		}
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM semantic_memory`)
		if err := row.Scan(&semanticCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query semantic memory count")
			return
		}
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM procedural_memory`)
		if err := row.Scan(&proceduralCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query procedural memory count")
			return
		}
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM relationship_memory`)
		if err := row.Scan(&relationshipCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query relationship memory count")
			return
		}

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
