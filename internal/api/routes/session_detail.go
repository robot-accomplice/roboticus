package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/plugin"
)

// ListSessionTurns returns turns for a session.
func ListSessionTurns(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		rows, err := db.NewRouteQueries(store).SessionTurnsWithMessages(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query session turns")
			return
		}
		defer func() { _ = rows.Close() }()

		turns := make([]map[string]any, 0)
		for rows.Next() {
			var id, role, content, createdAt, model string
			var cost float64
			var tokensIn, tokensOut int64
			if err := rows.Scan(&id, &role, &content, &createdAt, &model, &cost, &tokensIn, &tokensOut); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read session turn row")
				return
			}
			turns = append(turns, map[string]any{
				"id": id, "role": role, "content": content, "created_at": createdAt,
				"model": model, "cost": cost, "tokens_in": tokensIn, "tokens_out": tokensOut,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"turns": turns})
	}
}

// GetSessionFeedback returns feedback for a session by querying turn_feedback.
func GetSessionFeedback(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		rows, err := db.NewRouteQueries(store).SessionFeedback(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query session feedback")
			return
		}
		defer func() { _ = rows.Close() }()

		feedback := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, source, createdAt string
			var grade int
			var comment *string
			if err := rows.Scan(&id, &turnID, &grade, &source, &comment, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read session feedback row")
				return
			}
			f := map[string]any{
				"id": id, "turn_id": turnID, "grade": grade,
				"source": source, "created_at": createdAt,
			}
			if comment != nil {
				f["comment"] = *comment
			}
			feedback = append(feedback, f)
		}
		writeJSON(w, http.StatusOK, map[string]any{"feedback": feedback})
	}
}

// GetSessionInsights returns session insights based on turn data.
func GetSessionInsights(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		ctx := r.Context()

		// Gather session metrics.
		var turnCount, totalTokens int64
		var totalCost float64
		row := db.NewRouteQueries(store).SessionTurnStats(ctx, sessionID)
		if err := row.Scan(&turnCount, &totalTokens, &totalCost); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		msgCountInt, err := db.NewRouteQueries(store).SessionMessageCount(ctx, sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		msgCount := int64(msgCountInt)

		var toolCallCount int64
		row = db.NewRouteQueries(store).SessionToolCallCount(ctx, sessionID)
		if err := row.Scan(&toolCallCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// JS expects insights as an array of { severity, message, suggestion } objects.
		insightsArr := []map[string]any{
			{"severity": "info", "message": fmt.Sprintf("turn_count: %d", turnCount), "suggestion": ""},
			{"severity": "info", "message": fmt.Sprintf("message_count: %d", msgCount), "suggestion": ""},
			{"severity": "info", "message": fmt.Sprintf("total_tokens: %d", totalTokens), "suggestion": ""},
			{"severity": "info", "message": fmt.Sprintf("total_cost: %.6f", totalCost), "suggestion": ""},
			{"severity": "info", "message": fmt.Sprintf("tool_call_count: %d", toolCallCount), "suggestion": ""},
		}

		if turnCount > 0 {
			avgTokens := totalTokens / turnCount
			avgCost := totalCost / float64(turnCount)
			insightsArr = append(insightsArr,
				map[string]any{"severity": "info", "message": fmt.Sprintf("avg_tokens_per_turn: %d", avgTokens), "suggestion": ""},
				map[string]any{"severity": "info", "message": fmt.Sprintf("avg_cost_per_turn: %.6f", avgCost), "suggestion": ""},
			)
		}

		writeJSON(w, http.StatusOK, map[string]any{"insights": insightsArr})
	}
}

// DeleteSession deletes a session and its messages.
func DeleteSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		repo := db.NewSessionRepository(store)
		if err := repo.DeleteSession(r.Context(), sessionID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// GetSemanticCategories returns semantic memory categories with counts.
func GetSemanticCategories(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).SemanticCategories(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query semantic categories")
			return
		}
		defer func() { _ = rows.Close() }()

		cats := make([]map[string]any, 0)
		for rows.Next() {
			var cat string
			var cnt int64
			if err := rows.Scan(&cat, &cnt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read semantic category row")
				return
			}
			cats = append(cats, map[string]any{"category": cat, "count": cnt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"categories": cats})
	}
}

// DeleteSkill removes a skill by ID.
func DeleteSkill(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID := chi.URLParam(r, "id")
		repo := db.NewSkillsRepository(store)
		affected, err := repo.DeleteByID(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if affected == 0 {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// ToggleSkill enables/disables a skill.
func ToggleSkill(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID := chi.URLParam(r, "id")
		repo := db.NewSkillsRepository(store)
		affected, err := repo.ToggleByID(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if affected == 0 {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// GetSkillsCatalog returns the unified catalog with three distinct sections:
// skills (from DB), plugins (from plugin registry), and themes (builtin + catalog with install status).
func GetSkillsCatalog(store *db.Store, reg *plugin.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// --- Skills section ---
		skills := make([]map[string]any, 0)
		rows, err := db.NewRouteQueries(store).ListSkillsAll(r.Context())
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id, name, kind, description, version, riskLevel, createdAt string
				var enabled bool
				if err := rows.Scan(&id, &name, &kind, &description, &version, &riskLevel, &enabled, &createdAt); err != nil {
					continue
				}
				skills = append(skills, map[string]any{
					"id":          id,
					"name":        name,
					"kind":        kind,
					"description": description,
					"version":     version,
					"risk_level":  riskLevel,
					"enabled":     enabled,
					"installed":   true,
					"source":      "registry",
					"created_at":  createdAt,
				})
			}
		}

		// --- Plugins section ---
		plugins := make([]map[string]any, 0)
		if reg != nil {
			for _, p := range reg.List() {
				plugins = append(plugins, map[string]any{
					"name":    p.Name,
					"version": p.Version,
					"status":  p.Status,
					"tools":   p.Tools,
				})
			}
		}

		// --- Themes section (same enrichment as GetThemeCatalog) ---
		catalogMu.RLock()
		installed := installedThemeIDs(store)
		themes := make([]map[string]any, 0, len(builtinThemes)+len(catalogThemes))
		for _, t := range builtinThemes {
			themes = append(themes, map[string]any{
				"id": t.ID, "name": t.Name, "description": t.Description,
				"author": t.Author, "swatch": t.Swatch, "source": t.Source,
				"installed": installedThemes[t.ID] || t.Source == "builtin" || installed[t.ID],
			})
		}
		for _, t := range catalogThemes {
			themes = append(themes, map[string]any{
				"id": t.ID, "name": t.Name, "description": t.Description,
				"author": t.Author, "swatch": t.Swatch, "source": t.Source,
				"installed": installedThemes[t.ID] || installed[t.ID],
			})
		}
		catalogMu.RUnlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"skills":  skills,
			"plugins": plugins,
			"themes":  themes,
		})
	}
}

// InstallSkillFromCatalog installs a skill by writing its content to the skills directory.
// Accepts {"name": "skill_name", "content": "skill body markdown"}.
func InstallSkillFromCatalog(cfg *core.Config, store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Name == "" || req.Content == "" {
			writeError(w, http.StatusBadRequest, "name and content are required")
			return
		}

		skillsDir := cfg.Skills.Directory
		if skillsDir == "" {
			skillsDir = filepath.Join(core.ConfigDir(), "skills")
		}
		installer := db.NewInstaller(store, skillsDir)
		path, err := installer.Install(r.Context(), req.Name, req.Content)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"status": "installed",
			"name":   req.Name,
			"path":   path,
		})
	}
}

// ActivateSkillFromCatalog activates a skill by enabling it in the database.
// Accepts {"name": "skill_name"}.
func ActivateSkillFromCatalog(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		repo := db.NewSkillsRepository(store)
		if err := repo.SetEnabled(r.Context(), req.Name, true); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeError(w, http.StatusNotFound, "skill not found: "+req.Name)
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "activated", "name": req.Name})
	}
}

// InstallPlugin installs a plugin by writing its content to the plugins directory.
// Accepts {"name": "plugin_name", "content": "plugin script content"}.
func InstallPlugin(cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Name == "" || req.Content == "" {
			writeError(w, http.StatusBadRequest, "name and content are required")
			return
		}

		pluginsDir := cfg.Plugins.Dir
		if pluginsDir == "" {
			pluginsDir = filepath.Join(core.ConfigDir(), "plugins")
		}
		pluginDir := filepath.Join(pluginsDir, req.Name)
		path, err := core.WritePluginFile(pluginDir, "main", req.Content)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"status": "installed",
			"name":   req.Name,
			"path":   path,
		})
	}
}

// GetSkill returns a single skill by ID.
func GetSkill(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID := chi.URLParam(r, "id")
		row := db.NewRouteQueries(store).GetSkillByID(r.Context(), skillID)

		var id, name, kind, riskLevel, createdAt, version string
		var description *string
		var enabled bool
		if err := row.Scan(&id, &name, &kind, &description, &enabled, &version, &riskLevel, &createdAt); err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
		s := map[string]any{
			"id": id, "name": name, "kind": kind, "enabled": enabled,
			"version": version, "risk_level": riskLevel, "created_at": createdAt,
		}
		if description != nil {
			s["description"] = *description
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// UpdateSkill updates a skill's content and/or priority.
func UpdateSkill(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID := chi.URLParam(r, "id")
		var req struct {
			Description *string `json:"description"`
			RiskLevel   *string `json:"risk_level"`
			Version     *string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Build dynamic update via repo.
		repo := db.NewSkillsRepository(store)
		if req.Description != nil {
			if err := repo.UpdateField(r.Context(), skillID, "description", *req.Description); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if req.RiskLevel != nil {
			if err := repo.UpdateField(r.Context(), skillID, "risk_level", *req.RiskLevel); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if req.Version != nil {
			if err := repo.UpdateField(r.Context(), skillID, "version", *req.Version); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "id": skillID})
	}
}

// AuditSkills returns skill health summary.
func AuditSkills(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		rq := db.NewRouteQueries(store)
		activeCount, err := rq.CountEnabledSkills(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query active skills")
			return
		}

		disabledCount, err := rq.CountDisabledSkills(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query disabled skills")
			return
		}

		totalCount, err := rq.CountAllSkills(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query total skills")
			return
		}

		lastReload, err := rq.LatestSkillTimestamp(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query skill audit reload time")
			return
		}

		result := map[string]any{
			"total_count":    totalCount,
			"active_count":   activeCount,
			"disabled_count": disabledCount,
			"learned_count":  totalCount - activeCount - disabledCount,
		}
		if lastReload != nil {
			result["last_reload"] = *lastReload
		}
		writeJSON(w, http.StatusOK, result)
	}
}
