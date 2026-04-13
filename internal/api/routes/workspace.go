package routes

import (
	"context"
	"encoding/json"
	"net/http"
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

		// Append subagents from DB (enriched query for workspace canvas).
		agentRows, err := rq.ListSubAgentWorkspace(r.Context())
		if err == nil {
			defer func() { _ = agentRows.Close() }()
			for agentRows.Next() {
				var name, displayName, model, role, description string
				var enabled bool
				var sessionCount int
				var lastUsedAt, updatedAt *string
				if err := agentRows.Scan(&name, &displayName, &model, &enabled,
					&role, &description, &sessionCount, &lastUsedAt, &updatedAt); err != nil {
					break
				}
				state := "stopped"
				if enabled {
					state = "running"
				}
				entry := map[string]any{
					"name":          name,
					"display_name":  displayName,
					"model":         model,
					"enabled":       enabled,
					"state":         state,
					"activity":      "idle",
					"color":         "",
					"role":          role,
					"description":   description,
					"session_count": sessionCount,
				}
				if lastUsedAt != nil {
					entry["last_used_at"] = *lastUsedAt
				}
				if updatedAt != nil {
					entry["updated_at"] = *updatedAt
				}
				agents = append(agents, entry)
			}
		}

		// Fetch last pipeline trace timestamp for last_event_at.
		var lastEventAt *string
		if traceTS, err := rq.LatestPipelineTraceTime(r.Context()); err == nil && traceTS.Valid {
			lastEventAt = &traceTS.String
		}

		// Fetch active task summary if any task is in-progress.
		var activeTaskSummary *string
		var activeTaskPercentage *int
		if goal, pct, err := rq.ActiveTaskSummary(r.Context()); err == nil && goal != "" {
			activeTaskSummary = &goal
			activeTaskPercentage = &pct
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

		resp := map[string]any{
			"uptime":          time.Since(processStartTime).Seconds(),
			"goroutines":      runtime.NumGoroutine(),
			"connections":     dbStats.OpenConnections,
			"active_sessions": sessionCount,
			"db_in_use":       dbStats.InUse,
			"db_idle":         dbStats.Idle,
			"status":          "running",
			"agents":          agents,
			"systems":         systems,
			"updated_at":      time.Now().UTC().Format(time.RFC3339),
		}
		if lastEventAt != nil {
			resp["last_event_at"] = *lastEventAt
		}
		if activeTaskSummary != nil {
			resp["active_task_summary"] = *activeTaskSummary
			resp["active_task_percentage"] = *activeTaskPercentage
		}
		writeJSON(w, http.StatusOK, resp)
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
			configured = cfg.Channels.Telegram != nil
			enabled = configured && cfg.Channels.Telegram.Enabled
		case "whatsapp":
			configured = cfg.Channels.WhatsApp != nil
			enabled = configured && cfg.Channels.WhatsApp.Enabled
		case "discord":
			configured = cfg.Channels.Discord != nil
			enabled = configured && cfg.Channels.Discord.Enabled
		case "signal":
			configured = cfg.Channels.Signal != nil
			enabled = configured && cfg.Channels.Signal.Enabled
		case "email":
			configured = cfg.Channels.Email != nil
			enabled = configured && cfg.Channels.Email.Enabled
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
