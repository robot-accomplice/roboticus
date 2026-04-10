package routes

import (
	"context"
	"encoding/json"
	"errors"
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
func GetWorkspaceState(store *db.Store, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStats := store.Stats()
		rq := db.NewRouteQueries(store)
		sessionCount, err := rq.CountActiveSessions(r.Context())
		if err != nil {
			log.Warn().Err(err).Msg("failed to query active session count")
		}

		// Primary agent is always first.
		primaryName := cfg.Agent.Name
		if primaryName == "" {
			primaryName = "roboticus"
		}
		primaryModel := cfg.Models.Primary
		if primaryModel == "" {
			primaryModel = "auto"
		}

		// Derive activity from recent pipeline traces (last 30s = working).
		primaryActivity := "idle"
		if active, err := rq.HasRecentActivity(r.Context(), 30); err == nil && active {
			primaryActivity = "inference"
		}

		agents := []map[string]any{
			{
				"name":     strings.ToLower(primaryName),
				"id":       cfg.Agent.ID,
				"model":    primaryModel,
				"enabled":  true,
				"state":    "running",
				"activity": primaryActivity,
				"color":    "#6366f1",
				"role":     "orchestrator",
			},
		}

		// Append subagents from DB.
		agentRows, err := rq.ListSubAgentNamesModels(r.Context())
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
					"role":     "subagent",
				})
			}
		}

		// Systems/workstations for workspace canvas (Rust parity).
		systems := []map[string]any{
			{"id": "llm", "name": "LLM Inference", "kind": "Inference", "x": 0.18, "y": 0.22},
			{"id": "memory", "name": "Memory", "kind": "Storage", "x": 0.82, "y": 0.22},
			{"id": "exec", "name": "Code Execution", "kind": "Execution", "x": 0.18, "y": 0.78},
			{"id": "blockchain", "name": "Blockchain", "kind": "Blockchain", "x": 0.82, "y": 0.78},
			{"id": "web", "name": "Web / APIs", "kind": "Tool", "x": 0.50, "y": 0.12},
			{"id": "files", "name": "File System", "kind": "Tool", "x": 0.50, "y": 0.88},
			{"id": "tools_plugins", "name": "Tools / Plugins", "kind": "Plugin", "x": 0.965, "y": 0.50},
			{"id": "shelter", "name": "Idle Agents", "kind": "Shelter", "x": 0.035, "y": 0.50},
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
			"systems":         systems,
		})
	}
}

// GetRoster returns the agent roster: primary/orchestrator agent first, then subagents.
func GetRoster(store *db.Store, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Primary agent is always first in the roster.
		primaryName := cfg.Agent.Name
		if primaryName == "" {
			primaryName = "roboticus"
		}
		primaryModel := cfg.Models.Primary
		if primaryModel == "" {
			primaryModel = "auto"
		}
		// Count skills for the primary agent.
		rq := db.NewRouteQueries(store)
		var skillNames []string
		skillRows, sErr := rq.ListEnabledSkillNames(r.Context(), 20)
		if sErr == nil {
			defer func() { _ = skillRows.Close() }()
			for skillRows.Next() {
				var sn string
				if skillRows.Scan(&sn) == nil {
					skillNames = append(skillNames, sn)
				}
			}
		}

		agents := []map[string]any{
			{
				"name":            strings.ToLower(primaryName),
				"display_name":    primaryName,
				"model":           primaryModel,
				"enabled":         true,
				"role":            "orchestrator",
				"description":     "Primary orchestrator agent",
				"skills":          skillNames,
				"fallback_models": cfg.Models.Fallback,
			},
		}

		rows, err := rq.ListSubAgentRoster(r.Context())
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var name, displayName, model, role, description string
				var enabled bool
				if err := rows.Scan(&name, &displayName, &model, &enabled, &role, &description); err != nil {
					continue
				}
				if displayName == "" {
					displayName = name
				}
				agents = append(agents, map[string]any{
					"name":         name,
					"display_name": displayName,
					"model":        model,
					"enabled":      enabled,
					"role":         role,
					"description":  description,
					"skills":       []string{},
				})
			}
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
		repo := db.NewAgentsRepository(store)
		if err := repo.UpdateModel(r.Context(), agentName, req.Model, ""); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeError(w, http.StatusNotFound, "agent not found")
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
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
		repo := db.NewAgentsRepository(store)
		if err := repo.UpdateModel(r.Context(), name, req.Model, req.Description); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeError(w, http.StatusNotFound, "subagent not found")
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// ToggleSubagent enables/disables a subagent by name.
func ToggleSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		repo := db.NewAgentsRepository(store)
		if err := repo.ToggleEnabled(r.Context(), name); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeError(w, http.StatusNotFound, "subagent not found")
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// DeleteSubagent removes a subagent by name.
func DeleteSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		repo := db.NewAgentsRepository(store)
		affected, err := repo.DeleteByName(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if affected == 0 {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// applyConfigPatch delegates to core.ApplyConfigPatch — the canonical config
// mutation path. Route handlers call this thin wrapper; they do not implement
// config persistence directly (architecture_rules.md §4.1, §4.2).
func applyConfigPatch(ctx context.Context, store *db.Store, patch map[string]any) (string, error) {
	return core.ApplyConfigPatch(ctx, store, patch)
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
