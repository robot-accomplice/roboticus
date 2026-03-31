package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/db"
)

// ListTraces returns recent pipeline traces.
func ListTraces(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, turn_id, channel, total_ms, created_at
			 FROM pipeline_traces ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"traces": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		traces := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, channel, createdAt string
			var totalMs int64
			if err := rows.Scan(&id, &turnID, &channel, &totalMs, &createdAt); err != nil {
				continue
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
