package routes

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// --- Turns ---

// GetTurn returns a single turn with its messages.
func GetTurn(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, role, content, created_at FROM session_messages WHERE id = ? OR session_id = ? LIMIT 10`, id, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var messages []map[string]string
		for rows.Next() {
			var mid, role, content, createdAt string
			if err := rows.Scan(&mid, &role, &content, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read turn message row")
				return
			}
			messages = append(messages, map[string]string{
				"id": mid, "role": role, "content": content, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	}
}

// GetTurnFeedback returns feedback for a turn.
func GetTurnFeedback(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, turn_id, session_id, grade, source, comment, created_at
			 FROM turn_feedback
			 WHERE turn_id = ?
			 ORDER BY created_at DESC
			 LIMIT 1`, id)

		var feedbackID, turnID, sessionID, source, createdAt string
		var grade int
		var comment *string
		err := row.Scan(&feedbackID, &turnID, &sessionID, &grade, &source, &comment, &createdAt)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusOK, map[string]any{"feedback": nil})
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to query turn feedback")
			return
		}

		feedback := map[string]any{
			"id":         feedbackID,
			"turn_id":    turnID,
			"session_id": sessionID,
			"grade":      grade,
			"source":     source,
			"created_at": createdAt,
		}
		if comment != nil {
			feedback["comment"] = *comment
		}
		writeJSON(w, http.StatusOK, map[string]any{"feedback": feedback})
	}
}

// PostTurnFeedback creates feedback for a turn.
func PostTurnFeedback(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			Grade     int    `json:"grade"`
			Comment   string `json:"comment"`
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Grade < 1 || req.Grade > 5 {
			writeError(w, http.StatusBadRequest, "grade must be between 1 and 5")
			return
		}
		if req.SessionID == "" {
			// Try to look up the session from the turn.
			row := store.QueryRowContext(r.Context(), `SELECT session_id FROM turns WHERE id = ?`, id)
			if err := row.Scan(&req.SessionID); err != nil {
				log.Warn().Err(err).Str("turn_id", id).Msg("failed to look up session for turn feedback")
			}
		}
		feedbackID := db.NewID()
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO turn_feedback (id, turn_id, session_id, grade, comment)
			 VALUES (?, ?, ?, ?, ?)`,
			feedbackID, id, req.SessionID, req.Grade, req.Comment,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": feedbackID})
	}
}

// --- Skills ---

// ListSkills returns loaded skills from the database.
func ListSkills(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, name, kind, description, enabled, version, risk_level, created_at
			 FROM skills ORDER BY name`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query skills")
			return
		}
		defer func() { _ = rows.Close() }()

		skills := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, kind, riskLevel, createdAt, version string
			var description *string
			var enabled bool
			if err := rows.Scan(&id, &name, &kind, &description, &enabled, &version, &riskLevel, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read skill row")
				return
			}
			s := map[string]any{
				"id": id, "name": name, "kind": kind, "enabled": enabled,
				"version": version, "risk_level": riskLevel, "created_at": createdAt,
			}
			if description != nil {
				s["description"] = *description
			}
			skills = append(skills, s)
		}
		writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
	}
}

// ReloadSkills reloads all skills from disk using the provided reload callback.
func ReloadSkills(reload func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := reload(); err != nil {
			writeError(w, http.StatusInternalServerError, "reload failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
	}
}
