package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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
		_ = row.Scan(&turnCount, &totalTokens, &totalCost)

		var msgCount int64
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, sessionID)
		_ = row.Scan(&msgCount)

		var toolCallCount int64
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM tool_calls tc
			 JOIN turns t ON t.id = tc.turn_id
			 WHERE t.session_id = ?`, sessionID)
		_ = row.Scan(&toolCallCount)

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
		_, _ = store.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, sessionID)
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
		_, _ = store.ExecContext(r.Context(), `DELETE FROM skills WHERE id = ?`, skillID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// ToggleSkill enables/disables a skill.
func ToggleSkill(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID := chi.URLParam(r, "id")
		_, _ = store.ExecContext(r.Context(),
			`UPDATE skills SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END WHERE id = ?`, skillID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// GetSkillsCatalog returns available skills from the catalog.
func GetSkillsCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"skills": make([]any, 0)})
	}
}

// InstallSkillFromCatalog installs a skill from the catalog.
func InstallSkillFromCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "skill catalog installation not yet implemented")
	}
}

// ActivateSkillFromCatalog activates a catalog skill.
func ActivateSkillFromCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "skill catalog activation not yet implemented")
	}
}

// InstallPlugin installs a plugin.
func InstallPlugin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "plugin installation not yet implemented")
	}
}
