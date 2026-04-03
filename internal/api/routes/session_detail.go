package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/core"
	"goboticus/internal/db"
)

// ListSessionTurns returns turns for a session.
func ListSessionTurns(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, role, content, created_at FROM session_messages WHERE session_id = ? ORDER BY created_at`, sessionID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"turns": make([]any, 0)})
			return
		}
		defer func() { _ = rows.Close() }()

		turns := make([]map[string]any, 0)
		for rows.Next() {
			var id, role, content, createdAt string
			if err := rows.Scan(&id, &role, &content, &createdAt); err != nil {
				continue
			}
			turns = append(turns, map[string]any{
				"id": id, "role": role, "content": content, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"turns": turns})
	}
}

// GetSessionFeedback returns feedback for a session by querying turn_feedback.
func GetSessionFeedback(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		rows, err := store.QueryContext(r.Context(),
			`SELECT tf.id, tf.turn_id, tf.grade, tf.source, tf.comment, tf.created_at
			 FROM turn_feedback tf
			 WHERE tf.session_id = ?
			 ORDER BY tf.created_at DESC`, sessionID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"feedback": make([]any, 0)})
			return
		}
		defer func() { _ = rows.Close() }()

		feedback := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, source, createdAt string
			var grade int
			var comment *string
			if err := rows.Scan(&id, &turnID, &grade, &source, &comment, &createdAt); err != nil {
				continue
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
		row := store.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(tokens_in + tokens_out), 0), COALESCE(SUM(cost), 0)
			 FROM turns WHERE session_id = ?`, sessionID)
		if err := row.Scan(&turnCount, &totalTokens, &totalCost); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var msgCount int64
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, sessionID)
		if err := row.Scan(&msgCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var toolCallCount int64
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM tool_calls tc
			 JOIN turns t ON t.id = tc.turn_id
			 WHERE t.session_id = ?`, sessionID)
		if err := row.Scan(&toolCallCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		insights := map[string]any{
			"turn_count":      turnCount,
			"message_count":   msgCount,
			"total_tokens":    totalTokens,
			"total_cost":      totalCost,
			"tool_call_count": toolCallCount,
		}

		if turnCount > 0 {
			insights["avg_tokens_per_turn"] = totalTokens / turnCount
			insights["avg_cost_per_turn"] = totalCost / float64(turnCount)
		}

		writeJSON(w, http.StatusOK, map[string]any{"insights": insights})
	}
}

// DeleteSession deletes a session and its messages.
func DeleteSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		_, err := store.ExecContext(r.Context(), `DELETE FROM session_messages WHERE session_id = ?`, sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err = store.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// GetSemanticCategories returns semantic memory categories with counts.
func GetSemanticCategories(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT category, COUNT(*) as cnt FROM semantic_memory GROUP BY category ORDER BY cnt DESC`)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"categories": make([]any, 0)})
			return
		}
		defer func() { _ = rows.Close() }()

		cats := make([]map[string]any, 0)
		for rows.Next() {
			var cat string
			var cnt int64
			if err := rows.Scan(&cat, &cnt); err != nil {
				continue
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
		result, err := store.ExecContext(r.Context(), `DELETE FROM skills WHERE id = ?`, skillID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
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
		result, err := store.ExecContext(r.Context(),
			`UPDATE skills SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END WHERE id = ?`, skillID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// GetSkillsCatalog returns available skills from the catalog.
func GetSkillsCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"skills": make([]any, 0)})
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
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create skills directory: "+err.Error())
			return
		}

		path := filepath.Join(skillsDir, req.Name+".md")
		if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write skill file: "+err.Error())
			return
		}

		// Also register in the database.
		id := db.NewID()
		_, _ = store.ExecContext(r.Context(),
			`INSERT INTO skills (id, name, kind, source_path, content_hash, enabled, version, risk_level)
			 VALUES (?, ?, 'instruction', ?, '', 1, '1.0.0', 'Safe')
			 ON CONFLICT(name) DO UPDATE SET source_path = excluded.source_path`,
			id, req.Name, path)

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

		result, err := store.ExecContext(r.Context(),
			`UPDATE skills SET enabled = 1 WHERE name = ?`, req.Name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "skill not found: "+req.Name)
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
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create plugin directory: "+err.Error())
			return
		}

		path := filepath.Join(pluginDir, "main.lua")
		if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write plugin file: "+err.Error())
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
		row := store.QueryRowContext(r.Context(),
			`SELECT id, name, kind, description, enabled, version, risk_level, created_at
			 FROM skills WHERE id = ?`, skillID)

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

		// Build dynamic update.
		if req.Description != nil {
			if _, err := store.ExecContext(r.Context(),
				`UPDATE skills SET description = ? WHERE id = ?`, *req.Description, skillID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if req.RiskLevel != nil {
			if _, err := store.ExecContext(r.Context(),
				`UPDATE skills SET risk_level = ? WHERE id = ?`, *req.RiskLevel, skillID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if req.Version != nil {
			if _, err := store.ExecContext(r.Context(),
				`UPDATE skills SET version = ? WHERE id = ?`, *req.Version, skillID); err != nil {
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

		var activeCount, disabledCount, totalCount int64
		row := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE enabled = 1`)
		_ = row.Scan(&activeCount)

		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE enabled = 0`)
		_ = row.Scan(&disabledCount)

		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills`)
		_ = row.Scan(&totalCount)

		var lastReload *string
		row = store.QueryRowContext(ctx, `SELECT MAX(created_at) FROM skills`)
		_ = row.Scan(&lastReload)

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
