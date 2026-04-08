package routes

import (
	"net/http"
	"strconv"

	"roboticus/internal/db"
)

// GetThrottleStats returns abuse event statistics for the throttle dashboard.
func GetThrottleStats(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		hours := parseIntParam(r, "hours", 24)
		offset := intToNegStr(hours)
		rq := db.NewRouteQueries(store)

		// Total events in window.
		totalEvents, err := rq.AbuseSummary(ctx, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query throttle totals")
			return
		}

		// Events by signal type.
		typeRows, err := rq.AbuseByType(ctx, offset)
		byType := make([]map[string]any, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query throttle signal types")
			return
		}
		defer func() { _ = typeRows.Close() }()
		for typeRows.Next() {
			var sigType string
			var cnt int64
			var avgScore float64
			if err := typeRows.Scan(&sigType, &cnt, &avgScore); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read throttle signal row")
				return
			}
			byType = append(byType, map[string]any{
				"signal_type": sigType, "count": cnt, "avg_score": avgScore,
			})
		}

		// Top offending actors.
		actorRows, err := rq.AbuseByActor(ctx, offset)
		topActors := make([]map[string]any, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query throttle actors")
			return
		}
		defer func() { _ = actorRows.Close() }()
		for actorRows.Next() {
			var actorID, action string
			var cnt int64
			var totalScore float64
			if err := actorRows.Scan(&actorID, &cnt, &totalScore, &action); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read throttle actor row")
				return
			}
			topActors = append(topActors, map[string]any{
				"actor_id": actorID, "event_count": cnt,
				"total_score": totalScore, "last_action": action,
			})
		}

		// Active penalties.
		slowdownCount, quarantineCount, err := rq.RateLimitCurrent(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query active throttle penalties")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"period_hours":       hours,
			"total_events":       totalEvents,
			"by_signal_type":     byType,
			"top_actors":         topActors,
			"active_slowdowns":   slowdownCount,
			"active_quarantines": quarantineCount,
		})
	}
}

func intToNegStr(n int) string {
	return "-" + strconv.Itoa(n)
}
