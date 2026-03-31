package routes

import (
	"encoding/json"
	"net/http"

	"goboticus/internal/db"
)

// ListDelegations returns recent delegation outcomes for observability.
func ListDelegations(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, session_id, turn_id, task_description, assigned_agents_json,
			        pattern, duration_ms, success, quality_score, created_at
			 FROM delegation_outcomes ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"delegations": make([]any, 0)})
			return
		}
		defer func() { _ = rows.Close() }()

		delegations := make([]map[string]any, 0)
		for rows.Next() {
			var id, sessionID, turnID, taskDesc, agentsJSON, pattern, createdAt string
			var durationMs int64
			var success int
			var qualityScore *float64
			if err := rows.Scan(&id, &sessionID, &turnID, &taskDesc, &agentsJSON,
				&pattern, &durationMs, &success, &qualityScore, &createdAt); err != nil {
				continue
			}
			d := map[string]any{
				"id": id, "session_id": sessionID, "turn_id": turnID,
				"task_description": taskDesc, "pattern": pattern,
				"duration_ms": durationMs, "success": success == 1,
				"created_at": createdAt,
			}
			if qualityScore != nil {
				d["quality_score"] = *qualityScore
			}
			var agents any
			if json.Unmarshal([]byte(agentsJSON), &agents) == nil {
				d["assigned_agents"] = agents
			}
			delegations = append(delegations, d)
		}
		writeJSON(w, http.StatusOK, map[string]any{"delegations": delegations})
	}
}
