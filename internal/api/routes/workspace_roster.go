package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// GetRoster returns the agent roster with rich personality data.
// Rust parity: workspace_roster.rs — includes voice, missions, firmware rules,
// skill breakdown, capabilities, and subagent metadata in the response envelope.
func GetRoster(store *db.Store, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rq := db.NewRouteQueries(store)
		ctx := r.Context()

		// --- Load personality files (graceful defaults on missing) ---
		workspaceDir := cfg.Agent.Workspace
		if workspaceDir == "" {
			workspaceDir = core.ConfigDir()
		}

		osConfig, _ := core.LoadOsConfig(workspaceDir, cfg.Personality.OSPath)
		fwConfig, _ := core.LoadFirmwareConfig(workspaceDir, cfg.Personality.FirmwarePath)
		dirConfig, _ := core.LoadDirectivesConfig(workspaceDir, "")

		// --- Build skill lists and breakdown ---
		var allSkillNames []string
		skillBreakdown := map[string][]string{}
		var totalSkills, enabledSkills int

		skillRows, sErr := rq.ListSkillNamesAndKinds(ctx)
		if sErr == nil {
			defer func() { _ = skillRows.Close() }()
			for skillRows.Next() {
				var name, kind string
				var enabled bool
				if skillRows.Scan(&name, &kind, &enabled) == nil {
					allSkillNames = append(allSkillNames, name)
					skillBreakdown[kind] = append(skillBreakdown[kind], name)
					totalSkills++
					if enabled {
						enabledSkills++
					}
				}
			}
		}

		// --- Collect subagent names and counts ---
		var subordinateNames []string
		subTotal, subRunning := 0, 0

		rows, err := rq.ListSubAgentRosterEnriched(ctx)
		var subagentCards []map[string]any
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var name, displayName, model, role, description string
				var fallbackJSON, skillsJSON string
				var enabled bool
				var sessionCount int
				var lastUsedAt, status *string
				if err := rows.Scan(&name, &displayName, &model, &enabled,
					&role, &description, &fallbackJSON, &skillsJSON,
					&sessionCount, &lastUsedAt, &status); err != nil {
					continue
				}
				subTotal++
				statusStr := "stopped"
				if status != nil {
					statusStr = *status
				}
				if enabled {
					subRunning++
				}

				subordinateNames = append(subordinateNames, name)

				// Parse fallback models JSON.
				var fallbackModels []string
				_ = json.Unmarshal([]byte(fallbackJSON), &fallbackModels)
				if fallbackModels == nil {
					fallbackModels = []string{}
				}

				// Parse skills JSON (subagent-specific).
				var fixedSkills []string
				_ = json.Unmarshal([]byte(skillsJSON), &fixedSkills)
				if fixedSkills == nil {
					fixedSkills = []string{}
				}

				entry := map[string]any{
					"id":              name,
					"name":            name,
					"display_name":    displayName,
					"model":           model,
					"resolved_model":  model,
					"model_mode":      "assigned",
					"enabled":         enabled,
					"role":            role,
					"description":     description,
					"color":           "",
					"fallback_models": fallbackModels,
					"fixed_skills":    fixedSkills,
					"shared_skills":   allSkillNames,
					"skills":          fixedSkills,
					"session_count":   sessionCount,
					"supervisor":      strings.ToLower(primaryNameOrDefault(cfg)),
					"status":          statusStr,
				}
				if lastUsedAt != nil {
					entry["last_used_at"] = *lastUsedAt
				}
				subagentCards = append(subagentCards, entry)
			}
		}
		if subagentCards == nil {
			subagentCards = []map[string]any{}
		}
		if subordinateNames == nil {
			subordinateNames = []string{}
		}

		// --- Build voice object ---
		voice := rosterVoice(osConfig)

		// --- Build missions ---
		missions := make([]map[string]string, 0, len(dirConfig.Missions))
		for _, m := range dirConfig.Missions {
			missions = append(missions, map[string]string{
				"name":        m.Name,
				"description": m.Description,
				"priority":    m.Priority,
				"timeframe":   m.Timeframe,
			})
		}

		// --- Build firmware rules ---
		fwRules := make([]string, 0, len(fwConfig.Rules))
		for _, rule := range fwConfig.Rules {
			prefix := ""
			switch rule.RuleType {
			case "must":
				prefix = "MUST: "
			case "must_not":
				prefix = "MUST NOT: "
			}
			fwRules = append(fwRules, prefix+rule.Rule)
		}

		// --- Build description from OS prompt text ---
		description := rosterDescription(osConfig)

		// --- Primary agent capabilities ---
		capabilities := []string{"orchestrate-subagents", "assign-tasks", "select-subagent-model"}
		if cfg.Agent.DelegationEnabled {
			capabilities = append(capabilities, "delegate-tasks")
		}

		// --- Primary agent card ---
		primaryName := primaryNameOrDefault(cfg)
		primaryModel := cfg.Models.Primary
		if primaryModel == "" {
			primaryModel = "auto"
		}

		primaryCard := map[string]any{
			"id":              cfg.Agent.ID,
			"name":            strings.ToLower(primaryName),
			"display_name":    primaryName,
			"role":            "orchestrator",
			"model":           primaryModel,
			"enabled":         true,
			"color":           "#6366f1",
			"description":     description,
			"voice":           voice,
			"missions":        missions,
			"firmware_rules":  fwRules,
			"skills":          allSkillNames,
			"skill_breakdown": skillBreakdown,
			"capabilities":    capabilities,
			"subordinates":    subordinateNames,
			"fallback_models": cfg.Models.Fallback,
			"stats": map[string]int{
				"subordinate_count":    subTotal,
				"running_subordinates": subRunning,
				"total_skills":         totalSkills,
				"enabled_skills":       enabledSkills,
			},
		}

		// --- Combine roster ---
		roster := append([]map[string]any{primaryCard}, subagentCards...)

		// --- Model proxies (providers configured as local proxies) ---
		modelProxies := []string{}
		modelProxyCount := 0
		for name, p := range cfg.Providers {
			if p.IsLocal {
				modelProxies = append(modelProxies, name)
				modelProxyCount++
			}
		}

		// --- Taskable subagent count (enabled subagents) ---
		taskableCount := 0
		for _, sa := range subagentCards {
			if en, ok := sa["enabled"].(bool); ok && en {
				taskableCount++
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"roster":                  roster,
			"count":                   len(roster),
			"taskable_subagent_count": taskableCount,
			"model_proxy_count":       modelProxyCount,
			"model_proxies":           modelProxies,
		})
	}
}

// rosterVoice builds the voice parameters object with defaults for empty fields.
func rosterVoice(osConfig core.OsConfig) map[string]string {
	voice := map[string]string{
		"formality":     osConfig.Voice.Formality,
		"proactiveness": osConfig.Voice.Proactiveness,
		"verbosity":     osConfig.Voice.Verbosity,
		"humor":         osConfig.Voice.Humor,
		"domain":        osConfig.Voice.Domain,
	}
	defaults := map[string]string{
		"formality": "balanced", "proactiveness": "suggest",
		"verbosity": "concise", "humor": "dry", "domain": "general",
	}
	for k, d := range defaults {
		if voice[k] == "" {
			voice[k] = d
		}
	}
	return voice
}

// rosterDescription extracts the first line of the OS personality prompt text.
func rosterDescription(osConfig core.OsConfig) string {
	for _, src := range []string{osConfig.PromptText, osConfig.Voice.PromptText} {
		if src != "" {
			if first := strings.SplitN(src, "\n", 2)[0]; first != "" {
				return first
			}
		}
	}
	return "Primary orchestrator agent"
}

// primaryNameOrDefault returns the configured agent name or "roboticus".
func primaryNameOrDefault(cfg *core.Config) string {
	if cfg.Agent.Name != "" {
		return cfg.Agent.Name
	}
	return "roboticus"
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
