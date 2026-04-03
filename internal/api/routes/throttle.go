package routes

import (
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
)

// GetThrottleStats returns abuse event statistics for the throttle dashboard.
func GetThrottleStats(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		hours := parseIntParam(r, "hours", 24)

		// Total events in window.
		var totalEvents int64
		row := store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM abuse_events
			 WHERE created_at >= datetime('now', ? || ' hours')`,
			intToNegStr(hours))
		if err := row.Scan(&totalEvents); err != nil {
			log.Warn().Err(err).Str("metric", "total_events").Msg("scan failed")
		}

		// Events by signal type.
		typeRows, err := store.QueryContext(ctx,
			`SELECT signal_type, COUNT(*), COALESCE(AVG(score), 0)
			 FROM abuse_events
			 WHERE created_at >= datetime('now', ? || ' hours')
			 GROUP BY signal_type ORDER BY COUNT(*) DESC`,
			intToNegStr(hours))
		byType := make([]map[string]any, 0)
		if err == nil {
			defer func() { _ = typeRows.Close() }()
			for typeRows.Next() {
				var sigType string
				var cnt int64
				var avgScore float64
				if typeRows.Scan(&sigType, &cnt, &avgScore) == nil {
					byType = append(byType, map[string]any{
						"signal_type": sigType, "count": cnt, "avg_score": avgScore,
					})
				}
			}
		}

		// Top offending actors.
		actorRows, err := store.QueryContext(ctx,
			`SELECT actor_id, COUNT(*), COALESCE(SUM(score), 0), MAX(action_taken)
			 FROM abuse_events
			 WHERE created_at >= datetime('now', ? || ' hours')
			 GROUP BY actor_id ORDER BY SUM(score) DESC LIMIT 10`,
			intToNegStr(hours))
		topActors := make([]map[string]any, 0)
		if err == nil {
			defer func() { _ = actorRows.Close() }()
			for actorRows.Next() {
				var actorID, action string
				var cnt int64
				var totalScore float64
				if actorRows.Scan(&actorID, &cnt, &totalScore, &action) == nil {
					topActors = append(topActors, map[string]any{
						"actor_id": actorID, "event_count": cnt,
						"total_score": totalScore, "last_action": action,
					})
				}
			}
		}

		// Active penalties.
		var slowdownCount, quarantineCount int64
		row = store.QueryRowContext(ctx,
			`SELECT
			   SUM(CASE WHEN action_taken = 'slowdown' THEN 1 ELSE 0 END),
			   SUM(CASE WHEN action_taken = 'quarantine' THEN 1 ELSE 0 END)
			 FROM abuse_events
			 WHERE created_at >= datetime('now', '-5 minutes')`)
		if err := row.Scan(&slowdownCount, &quarantineCount); err != nil {
			log.Warn().Err(err).Str("metric", "active_penalties").Msg("scan failed")
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
