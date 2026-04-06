package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
)

// ListAgents returns all registered agents from the sub_agents table.
func ListAgents(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, name, display_name, model, role, description, enabled, created_at
			 FROM sub_agents ORDER BY created_at DESC`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query agents")
			return
		}
		defer func() { _ = rows.Close() }()

		agents := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, model, role, createdAt string
			var displayName, description *string
			var enabled bool
			if err := rows.Scan(&id, &name, &displayName, &model, &role, &description, &enabled, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read agent row")
				return
			}
			a := map[string]any{
				"id": id, "name": name, "model": model,
				"role": role, "enabled": enabled, "created_at": createdAt,
			}
			if displayName != nil {
				a["display_name"] = *displayName
			}
			if description != nil {
				a["description"] = *description
			}
			agents = append(agents, a)
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
	}
}

// StartAgent sets an agent's status to "running" by enabling it.
func StartAgent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		res, err := store.ExecContext(r.Context(),
			`UPDATE sub_agents SET enabled = 1 WHERE id = ? OR name = ?`, id, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "running"})
	}
}

// StopAgent sets an agent's status to "stopped" by disabling it.
func StopAgent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		res, err := store.ExecContext(r.Context(),
			`UPDATE sub_agents SET enabled = 0 WHERE id = ? OR name = ?`, id, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "stopped"})
	}
}

// A2AHello returns the agent card for A2A discovery handshake.
func A2AHello() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		card := map[string]any{
			"name":        "roboticus",
			"description": "Autonomous AI agent runtime",
			"version":     "0.1.0",
			"protocol":    "a2a/1.0",
			"capabilities": []string{
				"chat", "tool-use", "multi-model", "memory",
				"multi-channel", "scheduling", "delegation",
			},
		}
		writeJSON(w, http.StatusOK, card)
	}
}
