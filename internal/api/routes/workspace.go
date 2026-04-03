package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/core"
	"goboticus/internal/db"
)

// GetWorkspaceState returns live runtime state for the workspace page.
func GetWorkspaceState(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStats := store.Stats()
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
		result, err := store.ExecContext(r.Context(),
			`UPDATE sub_agents SET model = ? WHERE name = ?`, req.Model, agentName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
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
		result, err := store.ExecContext(r.Context(),
			`UPDATE sub_agents SET model = ?, description = ? WHERE name = ?`,
			req.Model, req.Description, name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// ToggleSubagent enables/disables a subagent by name.
func ToggleSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		result, err := store.ExecContext(r.Context(),
			`UPDATE sub_agents SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END WHERE name = ?`, name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// DeleteSubagent removes a subagent by name.
func DeleteSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		result, err := store.ExecContext(r.Context(), `DELETE FROM sub_agents WHERE name = ?`, name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// UpdateConfig applies a JSON config patch by merging it into the TOML config file.
// The patch is a flat JSON object whose keys map to top-level TOML sections.
// After writing, the caller should restart the daemon to pick up the changes.
func UpdateConfig(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if len(patch) == 0 {
			writeError(w, http.StatusBadRequest, "empty patch")
			return
		}

		path := core.ConfigFilePath()

		// Read existing config file (may not exist yet).
		existing, _ := os.ReadFile(path)

		// Store each patch key-value in the identity table for persistence,
		// and also append to the TOML file for the next restart.
		patchJSON, err := json.Marshal(patch)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal patch")
			return
		}

		// Persist patch to identity table for audit trail.
		_, err = store.ExecContext(r.Context(),
			`INSERT INTO identity (key, value) VALUES ('config_patch:latest', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			string(patchJSON))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Append the patch values as TOML lines to the config file.
		var tomlLines []byte
		if len(existing) > 0 {
			tomlLines = append(existing, '\n')
		}
		tomlLines = append(tomlLines, []byte("# --- patched via API ---\n")...)
		for k, v := range patch {
			line, merr := json.Marshal(v)
			if merr != nil {
				continue
			}
			tomlLines = append(tomlLines, []byte(k+" = ")...)
			tomlLines = append(tomlLines, line...)
			tomlLines = append(tomlLines, '\n')
		}

		// Ensure directory exists.
		if err := os.MkdirAll(core.ConfigDir(), 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.WriteFile(path, tomlLines, 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "patched",
			"keys":            len(patch),
			"path":            path,
			"restart_required": true,
		})
	}
}

// TestChannel validates that a channel platform is configured and enabled.
// It checks the config for the named platform and reports its status.
func TestChannel(cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		platform := chi.URLParam(r, "name")
		if platform == "" {
			writeError(w, http.StatusBadRequest, "platform name is required")
			return
		}

		configured := false
		enabled := false

		switch platform {
		case "telegram":
			configured = cfg.Channels.TelegramTokenEnv != ""
			enabled = configured && os.Getenv(cfg.Channels.TelegramTokenEnv) != ""
		case "whatsapp":
			configured = cfg.Channels.WhatsAppTokenEnv != ""
			enabled = configured && os.Getenv(cfg.Channels.WhatsAppTokenEnv) != ""
		case "discord":
			configured = cfg.Channels.DiscordTokenEnv != ""
			enabled = configured && os.Getenv(cfg.Channels.DiscordTokenEnv) != ""
		case "signal":
			configured = cfg.Channels.SignalDaemonURL != ""
			enabled = configured && cfg.Channels.SignalAccount != ""
		case "email":
			configured = cfg.Channels.EmailFromAddress != ""
			enabled = configured
		case "matrix":
			configured = cfg.Matrix.HomeserverURL != ""
			enabled = cfg.Matrix.Enabled
		default:
			writeError(w, http.StatusNotFound, "unknown platform: "+platform)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"platform":   platform,
			"configured": configured,
			"enabled":    enabled,
		})
	}
}

// SetProviderKey stores a provider API key.
func SetProviderKey(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Key == "" {
			writeError(w, http.StatusBadRequest, "key is required")
			return
		}
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO identity (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			"provider_key:"+provider, req.Key)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "provider": provider})
	}
}

// DeleteProviderKey removes a provider API key.
func DeleteProviderKey(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		result, err := store.ExecContext(r.Context(),
			`DELETE FROM identity WHERE key = ?`, "provider_key:"+provider)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "provider key not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "provider": provider})
	}
}
