package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
)

// ListTraces returns recent pipeline traces.
func ListTraces(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, turn_id, channel, total_ms, created_at
			 FROM pipeline_traces ORDER BY created_at DESC LIMIT ?`, limit)
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

// GetTrace returns a pipeline trace by turn ID with parsed stages.
func GetTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, turn_id, channel, total_ms, stages_json, created_at
			 FROM pipeline_traces WHERE turn_id = ? LIMIT 1`, turnID)
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
		row := store.QueryRowContext(r.Context(),
			`SELECT rt.id, rt.pipeline_trace_id, rt.react_json, rt.created_at
			 FROM react_traces rt
			 JOIN pipeline_traces pt ON pt.id = rt.pipeline_trace_id
			 WHERE pt.turn_id = ? LIMIT 1`, turnID)
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

		writeJSON(w, http.StatusOK, map[string]any{
			"id":                id,
			"pipeline_trace_id": pipelineTraceID,
			"react_trace":       parsed,
			"created_at":        createdAt,
		})
	}
}

// ExportTrace returns the full trace as downloadable JSON.
func ExportTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, turn_id, channel, total_ms, stages_json, created_at
			 FROM pipeline_traces WHERE turn_id = ? LIMIT 1`, turnID)
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

// GetTraceFlow returns trace stages with timing diagram data.
func GetTraceFlow(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, turn_id, channel, total_ms, stages_json, created_at
			 FROM pipeline_traces WHERE turn_id = ? LIMIT 1`, turnID)
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
