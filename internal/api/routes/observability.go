package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
)

// ListObservabilityTraces returns pipeline traces with pagination.
func ListObservabilityTraces(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		offset := parseIntParam(r, "offset", 0)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at
			 FROM pipeline_traces ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query observability traces")
			return
		}
		defer func() { _ = rows.Close() }()

		traces := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, sessionID, channel, stagesJSON, createdAt string
			var totalMs int64
			if err := rows.Scan(&id, &turnID, &sessionID, &channel, &totalMs, &stagesJSON, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read observability trace row")
				return
			}
			var stages any
			if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
				stages = []any{}
			}
			traces = append(traces, map[string]any{
				"id": id, "turn_id": turnID, "session_id": sessionID,
				"channel": channel, "total_ms": totalMs, "stages": stages, "created_at": createdAt,
			})
		}

		var total int64
		row := store.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pipeline_traces`)
		if err := row.Scan(&total); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"traces": traces, "total": total, "limit": limit, "offset": offset,
		})
	}
}

// TraceWaterfall returns trace stages as a waterfall JSON structure.
func TraceWaterfall(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, turn_id, channel, total_ms, stages_json, created_at
			 FROM pipeline_traces WHERE id = ? LIMIT 1`, id)
		var traceID, turnID, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&traceID, &turnID, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id": traceID, "turn_id": turnID, "channel": channel,
			"total_ms": totalMs, "stages": stages, "created_at": createdAt,
			"format": "waterfall",
		})
	}
}

// DelegationOutcomes returns delegation outcome records.
func DelegationOutcomes(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, turn_id, session_id, task_description, subtask_count,
			        pattern, assigned_agents_json, duration_ms, success, quality_score, created_at
			 FROM delegation_outcomes ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query delegation outcomes")
			return
		}
		defer func() { _ = rows.Close() }()

		outcomes := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, sessionID, taskDesc, pattern, assignedAgentsJSON, createdAt string
			var subtaskCount, durationMs int64
			var success int
			var qualityScore *float64
			if err := rows.Scan(&id, &turnID, &sessionID, &taskDesc, &subtaskCount,
				&pattern, &assignedAgentsJSON, &durationMs, &success, &qualityScore, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read delegation outcome row")
				return
			}
			o := map[string]any{
				"id": id, "turn_id": turnID, "session_id": sessionID,
				"task_description": taskDesc, "subtask_count": subtaskCount,
				"pattern": pattern, "assigned_agents_json": assignedAgentsJSON,
				"duration_ms": durationMs, "retry_count": 0,
				"success": success == 1, "created_at": createdAt,
			}
			if qualityScore != nil {
				o["quality_score"] = *qualityScore
			}
			outcomes = append(outcomes, o)
		}
		writeJSON(w, http.StatusOK, map[string]any{"outcomes": outcomes})
	}
}

// DelegationStats returns aggregate delegation statistics.
func DelegationStats(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var total, successful int64
		var avgDuration float64
		row := store.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(success), 0), COALESCE(AVG(duration_ms), 0)
			 FROM delegation_outcomes`)
		if err := row.Scan(&total, &successful, &avgDuration); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var avgQuality float64
		row = store.QueryRowContext(ctx,
			`SELECT COALESCE(AVG(quality_score), 0) FROM delegation_outcomes WHERE quality_score IS NOT NULL`)
		if err := row.Scan(&avgQuality); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var successRate float64
		if total > 0 {
			successRate = float64(successful) / float64(total)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"total_delegations": total,
			"successful":        successful,
			"success_rate":      successRate,
			"avg_duration_ms":   avgDuration,
			"avg_quality":       avgQuality,
		})
	}
}
