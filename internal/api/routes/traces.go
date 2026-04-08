package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
)

// ListTraces returns recent pipeline traces.
func ListTraces(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := db.NewRouteQueries(store).ListTracesSimple(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query traces")
			return
		}
		defer func() { _ = rows.Close() }()

		traces := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, channel, createdAt string
			var totalMs int64
			if err := rows.Scan(&id, &turnID, &channel, &totalMs, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read trace row")
				return
			}
			traces = append(traces, map[string]any{
				"id": id, "turn_id": turnID, "channel": channel,
				"total_ms": totalMs, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"traces": traces})
	}
}

// SearchTraces searches traces by optional tool, guard, duration, and timestamp filters.
func SearchTraces(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		if limit > 200 {
			limit = 200
		}
		toolName := strings.TrimSpace(r.URL.Query().Get("tool_name"))
		guardName := strings.TrimSpace(r.URL.Query().Get("guard_name"))
		minDuration := parseIntParam(r, "min_duration_ms", 0)
		since := strings.TrimSpace(r.URL.Query().Get("since"))

		query := `SELECT turn_id, session_id, channel, total_ms, created_at, stages_json
			FROM pipeline_traces WHERE 1=1`
		args := make([]any, 0, 5)
		if toolName != "" {
			query += ` AND stages_json LIKE ?`
			args = append(args, "%"+toolName+"%")
		}
		if guardName != "" {
			query += ` AND (react_trace_json LIKE ? OR stages_json LIKE ?)`
			args = append(args, "%"+guardName+"%", "%"+guardName+"%")
		}
		if minDuration > 0 {
			query += ` AND total_ms >= ?`
			args = append(args, minDuration)
		}
		if since != "" {
			query += ` AND created_at >= ?`
			args = append(args, since)
		}
		query += ` ORDER BY created_at DESC LIMIT ?`
		args = append(args, limit)

		rows, err := db.NewRouteQueries(store).Query(r.Context(), query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query traces")
			return
		}
		defer func() { _ = rows.Close() }()

		results := make([]map[string]any, 0)
		for rows.Next() {
			var turnID, sessionID, channel, createdAt, stagesJSON string
			var totalMs int64
			if err := rows.Scan(&turnID, &sessionID, &channel, &totalMs, &createdAt, &stagesJSON); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read trace search row")
				return
			}
			results = append(results, map[string]any{
				"turn_id":     turnID,
				"session_id":  sessionID,
				"channel":     channel,
				"total_ms":    totalMs,
				"created_at":  createdAt,
				"stages_json": stagesJSON,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"results": results,
			"count":   len(results),
		})
	}
}

// GetTrace returns a pipeline trace by turn ID with parsed stages.
func GetTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace not found"})
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "turn_id": tid, "channel": channel,
			"total_ms": totalMs, "stages": stages, "created_at": createdAt,
		})
	}
}

// GetReactTrace returns the ReAct trace for a given turn.
func GetReactTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		// First find the pipeline trace for this turn, then get the react trace.
		row := db.NewRouteQueries(store).GetReactTraceByTurnID(r.Context(), turnID)
		var id, pipelineTraceID, reactJSON, createdAt string
		err := row.Scan(&id, &pipelineTraceID, &reactJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "react trace not found")
			return
		}

		var parsed any
		if json.Unmarshal([]byte(reactJSON), &parsed) != nil {
			parsed = map[string]any{}
		}

		// Ensure steps is an array for JS (data.steps || []).
		steps := parsed
		if _, ok := parsed.([]any); !ok {
			steps = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":                id,
			"pipeline_trace_id": pipelineTraceID,
			"react_trace":       parsed,
			"steps":             steps,
			"created_at":        createdAt,
		})
	}
}

// ExportTrace returns the full trace as downloadable JSON.
func ExportTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=trace-"+turnID+".json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": id, "turn_id": tid, "channel": channel,
			"total_ms": totalMs, "stages": stages, "created_at": createdAt,
		})
	}
}

// ReplayTrace returns a replay preview for a given trace turn.
func ReplayTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"turn_id":  tid,
			"replayed": true,
			"trace": map[string]any{
				"id": id, "turn_id": tid, "channel": channel,
				"total_ms": totalMs, "stages": stages, "created_at": createdAt,
			},
		})
	}
}

// GetTraceFlow returns trace stages with timing diagram data.
func GetTraceFlow(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "turn_id": tid, "channel": channel,
			"total_ms": totalMs, "stages": stages, "created_at": createdAt,
			"format": "flow",
		})
	}
}
