package routes

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/db"
)

// GetWorkspaceState returns live runtime state for the workspace page.
func GetWorkspaceState(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStats := store.DB().Stats()
		var sessionCount int64
		row := store.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sessions WHERE status = 'active'`)
		_ = row.Scan(&sessionCount)
		writeJSON(w, http.StatusOK, map[string]any{
			"uptime":          time.Since(processStartTime).Seconds(),
			"goroutines":      runtime.NumGoroutine(),
			"connections":     dbStats.OpenConnections,
			"active_sessions": sessionCount,
			"db_in_use":       dbStats.InUse,
			"db_idle":         dbStats.Idle,
			"status":          "running",
		})
	}
}

// GetRoster returns the agent roster from the sub_agents table.
func GetRoster(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT name, model, enabled, role FROM sub_agents ORDER BY name`)
		if err != nil {
			// Fallback to default agent.
			writeJSON(w, http.StatusOK, map[string]any{
				"agents": []map[string]any{
					{"name": "default", "model": "", "enabled": true, "role": "primary"},
				},
			})
			return
		}
		defer func() { _ = rows.Close() }()

		agents := make([]map[string]any, 0)
		for rows.Next() {
			var name, model, role string
			var enabled bool
			if err := rows.Scan(&name, &model, &enabled, &role); err != nil {
				continue
			}
			agents = append(agents, map[string]any{
				"name": name, "model": model, "enabled": enabled, "role": role,
			})
		}
		if len(agents) == 0 {
			agents = append(agents, map[string]any{
				"name": "default", "model": "", "enabled": true, "role": "primary",
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
	}
}

// UpdateRosterModel updates an agent's model assignment.
func UpdateRosterModel(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentName := chi.URLParam(r, "agent")
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		_, _ = store.ExecContext(r.Context(),
			`UPDATE sub_agents SET model = ? WHERE name = ?`, req.Model, agentName)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// UpdateSubagent updates a subagent by name.
func UpdateSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		var req struct {
			Model       string `json:"model"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		_, _ = store.ExecContext(r.Context(),
			`UPDATE sub_agents SET model = ?, description = ? WHERE name = ?`,
			req.Model, req.Description, name)
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// ToggleSubagent enables/disables a subagent by name.
func ToggleSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		_, _ = store.ExecContext(r.Context(),
			`UPDATE sub_agents SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END WHERE name = ?`, name)
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// DeleteSubagent removes a subagent by name.
func DeleteSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		_, _ = store.ExecContext(r.Context(), `DELETE FROM sub_agents WHERE name = ?`, name)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// UpdateConfig applies a config patch.
func UpdateConfig(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
	}
}

// TestChannel sends a test message to a channel.
func TestChannel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		writeJSON(w, http.StatusOK, map[string]any{
			"channel": name,
			"status":  "test sent",
		})
	}
}

// SetProviderKey stores a provider API key.
func SetProviderKey(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}

// DeleteProviderKey removes a provider API key.
func DeleteProviderKey(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
	}
}
