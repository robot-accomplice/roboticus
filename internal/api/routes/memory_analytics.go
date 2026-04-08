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
		rq := db.NewRouteQueries(store)

		// Total turns with context snapshots in the period.
		totalTurns, turnsWithMemory, err := rq.RetrievalQualityAvg(ctx, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory analytics totals")
			return
		}

		// Hit rate.
		var hitRate float64
		if totalTurns > 0 {
			hitRate = float64(turnsWithMemory) / float64(totalTurns)
		}

		// Average budget utilization.
		avgUtilization, err := rq.CachePerformance(ctx, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory utilization")
			return
		}

		// Complexity distribution.
		complexityRows, err := rq.ComplexityDistribution(ctx, offset)
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
		avgGradeWithMem, err := rq.MemoryROIWithMemory(ctx, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory ROI with memory")
			return
		}

		avgGradeWithoutMem, err := rq.MemoryROIWithoutMemory(ctx, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query memory ROI without memory")
			return
		}

		memoryROI := 0.0
		if avgGradeWithoutMem > 0 {
			memoryROI = (avgGradeWithMem - avgGradeWithoutMem) / avgGradeWithoutMem
		}

		// Memory entry counts per tier.
		workingCount, err := rq.CountWorkingMemory(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query working memory count")
			return
		}
		episodicCount, err := rq.CountEpisodicMemory(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query episodic memory count")
			return
		}
		semanticCount, err := rq.CountSemanticMemory(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query semantic memory count")
			return
		}
		proceduralCount, err := rq.CountProceduralMemory(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query procedural memory count")
			return
		}
		relationshipCount, err := rq.CountRelationshipMemory(ctx)
		if err != nil {
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
