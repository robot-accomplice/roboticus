package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/db"
)

// GetPolicyAudit returns policy decisions for a given turn.
func GetPolicyAudit(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, turn_id, tool_name, decision, rule_name, reason, created_at
			 FROM policy_decisions WHERE turn_id = ? ORDER BY created_at`, turnID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"decisions": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		decisions := make([]map[string]any, 0)
		for rows.Next() {
			var id, tid, toolName, decision, createdAt string
			var ruleName, reason *string
			if err := rows.Scan(&id, &tid, &toolName, &decision, &ruleName, &reason, &createdAt); err != nil {
				continue
			}
			d := map[string]any{
				"id": id, "turn_id": tid, "tool_name": toolName,
				"decision": decision, "created_at": createdAt,
			}
			if ruleName != nil {
				d["rule_name"] = *ruleName
			}
			if reason != nil {
				d["reason"] = *reason
			}
			decisions = append(decisions, d)
		}
		writeJSON(w, http.StatusOK, map[string]any{"decisions": decisions})
	}
}

// GetToolAudit returns tool calls for a given turn.
func GetToolAudit(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, tool_name, input, output, status, duration_ms, created_at
			 FROM tool_calls WHERE turn_id = ? ORDER BY created_at`, turnID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"tool_calls": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		calls := make([]map[string]any, 0)
		for rows.Next() {
			var id, toolName, input, status, createdAt string
			var output *string
			var durationMs *int64
			if err := rows.Scan(&id, &toolName, &input, &output, &status, &durationMs, &createdAt); err != nil {
				continue
			}
			c := map[string]any{
				"id": id, "tool_name": toolName, "input": input,
				"status": status, "created_at": createdAt,
			}
			if output != nil {
				c["output"] = *output
			}
			if durationMs != nil {
				c["duration_ms"] = *durationMs
			}
			calls = append(calls, c)
		}
		writeJSON(w, http.StatusOK, map[string]any{"tool_calls": calls})
	}
}
