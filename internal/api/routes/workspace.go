package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// GetWorkspaceState returns live runtime state for the workspace page.
func GetWorkspaceState(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStats := store.Stats()
		var sessionCount int64
		row := store.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sessions WHERE status = 'active'`)
		if err := row.Scan(&sessionCount); err != nil {
			log.Warn().Err(err).Msg("failed to query active session count")
		}

		// Build agents array from sub_agents table for the fleet activity card.
		agents := make([]map[string]any, 0)
		agentRows, err := store.QueryContext(r.Context(),
			`SELECT name, model, enabled FROM sub_agents ORDER BY name`)
		if err == nil {
			defer func() { _ = agentRows.Close() }()
			for agentRows.Next() {
				var name, model string
				var enabled bool
				if err := agentRows.Scan(&name, &model, &enabled); err != nil {
					break
				}
				state := "stopped"
				if enabled {
					state = "running"
				}
				agents = append(agents, map[string]any{
					"name":     name,
					"model":    model,
					"enabled":  enabled,
					"state":    state,
					"activity": "idle",
					"color":    "",
				})
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"uptime":          time.Since(processStartTime).Seconds(),
			"goroutines":      runtime.NumGoroutine(),
			"connections":     dbStats.OpenConnections,
			"active_sessions": sessionCount,
			"db_in_use":       dbStats.InUse,
			"db_idle":         dbStats.Idle,
			"status":          "running",
			"agents":          agents,
		})
	}
}

// GetRoster returns the agent roster from the sub_agents table.
func GetRoster(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT name, model, enabled, role FROM sub_agents ORDER BY name`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query roster")
			return
		}
		defer func() { _ = rows.Close() }()

		agents := make([]map[string]any, 0)
		for rows.Next() {
			var name, model, role string
			var enabled bool
			if err := rows.Scan(&name, &model, &enabled, &role); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read roster row")
				return
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
		writeJSON(w, http.StatusOK, map[string]any{"roster": agents})
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

// applyConfigPatch loads the config from disk, merges the patch via JSON round-trip,
// validates the result, and writes valid TOML back to disk. It also persists an
// audit trail entry. This is the shared implementation used by both UpdateConfig
// and ConfigApply.
func applyConfigPatch(ctx context.Context, store *db.Store, patch map[string]any) (string, error) {
	path := core.ConfigFilePath()

	// Load the existing config from disk (falls back to defaults if absent).
	merged, err := core.LoadConfigFromFile(path)
	if err != nil {
		return path, fmt.Errorf("failed to load config: %w", err)
	}

	// Apply the patch: marshal the current config to JSON, overlay the patch
	// keys, then unmarshal back into the Config struct. This ensures only
	// known fields are accepted and types are enforced.
	base, err := json.Marshal(merged)
	if err != nil {
		return path, fmt.Errorf("failed to marshal config: %w", err)
	}

	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		return path, fmt.Errorf("failed to parse config: %w", err)
	}

	for k, v := range patch {
		baseMap[k] = v
	}

	patchedJSON, err := json.Marshal(baseMap)
	if err != nil {
		return path, fmt.Errorf("failed to marshal patched config: %w", err)
	}

	if err := json.Unmarshal(patchedJSON, &merged); err != nil {
		return path, fmt.Errorf("patch produced invalid config: %w", err)
	}

	// Validate the merged config.
	if err := merged.Validate(); err != nil {
		return path, fmt.Errorf("validation failed: %w", err)
	}

	// Persist patch to identity table for audit trail.
	auditJSON, _ := json.Marshal(patch)
	if _, err := store.ExecContext(ctx,
		`INSERT INTO identity (key, value) VALUES ('config_patch:latest', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		string(auditJSON)); err != nil {
		return path, fmt.Errorf("failed to persist config audit trail: %w", err)
	}

	// Write the validated config as TOML.
	tomlBytes, err := core.MarshalTOML(&merged)
	if err != nil {
		return path, fmt.Errorf("failed to marshal TOML: %w", err)
	}

	if err := os.MkdirAll(core.ConfigDir(), 0o755); err != nil {
		return path, fmt.Errorf("failed to create config dir: %w", err)
	}
	if err := os.WriteFile(path, tomlBytes, 0o644); err != nil {
		return path, fmt.Errorf("failed to write config file: %w", err)
	}

	return path, nil
}

// UpdateConfig applies a JSON config patch by loading the existing TOML config,
// merging the patch via JSON round-trip into the Config struct, validating the
// result, and writing valid TOML back to disk.
func UpdateConfig(cfg *core.Config, store *db.Store) http.HandlerFunc {
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

		path, err := applyConfigPatch(r.Context(), store, patch)
		if err != nil {
			// Distinguish validation errors (client's fault) from internal errors.
			errMsg := err.Error()
			if strings.HasPrefix(errMsg, "validation failed:") || strings.HasPrefix(errMsg, "patch produced invalid config:") {
				writeError(w, http.StatusBadRequest, errMsg)
			} else {
				writeError(w, http.StatusInternalServerError, errMsg)
			}
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":           "patched",
			"keys":             len(patch),
			"path":             path,
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

// SetProviderKey stores a provider API key in the encrypted keystore.
func SetProviderKey(ks *core.Keystore) http.HandlerFunc {
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
		if ks == nil {
			writeError(w, http.StatusServiceUnavailable, "keystore not initialized")
			return
		}
		if err := ks.Set("provider_key:"+provider, req.Key); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := ks.Save(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist keystore: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "provider": provider})
	}
}

// DeleteProviderKey removes a provider API key from the encrypted keystore.
func DeleteProviderKey(ks *core.Keystore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		if ks == nil {
			writeError(w, http.StatusServiceUnavailable, "keystore not initialized")
			return
		}
		if err := ks.Delete("provider_key:" + provider); err != nil {
			writeError(w, http.StatusNotFound, "provider key not found")
			return
		}
		if err := ks.Save(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist keystore: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "provider": provider})
	}
}
