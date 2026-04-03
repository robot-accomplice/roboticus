package routes

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
	"goboticus/internal/pipeline"
)

// ListSessions returns all sessions.
func ListSessions(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, agent_id, scope_key, status, nickname, created_at, updated_at
			 FROM sessions ORDER BY created_at DESC LIMIT 100`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var sessions []map[string]any
		for rows.Next() {
			var id, agentID, scopeKey, status, createdAt, updatedAt string
			var nickname *string
			if err := rows.Scan(&id, &agentID, &scopeKey, &status, &nickname, &createdAt, &updatedAt); err != nil {
				continue
			}
			s := map[string]any{
				"id":         id,
				"agent_id":   agentID,
				"scope":      scopeKey,
				"status":     status,
				"created_at": createdAt,
				"updated_at": updatedAt,
			}
			if nickname != nil {
				s["nickname"] = *nickname
			}
			sessions = append(sessions, s)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	}
}

// CreateSession creates a new session.
// The scope_key is made unique per session to avoid UNIQUE constraint violations
// on the (agent_id, scope_key) partial index for active sessions.
func CreateSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AgentID string `json:"agent_id"`
			Scope   string `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.AgentID == "" {
			req.AgentID = "default"
		}
		if req.Scope == "" {
			req.Scope = "api"
		}

		id := db.NewID()
		// Append session ID to scope to make it unique per session.
		// The partial unique index on (agent_id, scope_key) WHERE status='active'
		// prevents duplicate active sessions with the same scope.
		scopeKey := req.Scope + ":" + id
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
			id, req.AgentID, scopeKey,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"id":       id,
			"agent_id": req.AgentID,
			"scope":    req.Scope,
		})
	}
}

// GetSession returns a single session.
func GetSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, agent_id, scope_key, status, nickname, created_at, updated_at FROM sessions WHERE id = ?`, id)

		var agentID, scopeKey, status, createdAt, updatedAt string
		var nickname *string
		if err := row.Scan(&id, &agentID, &scopeKey, &status, &nickname, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		s := map[string]any{
			"id":         id,
			"agent_id":   agentID,
			"scope":      scopeKey,
			"status":     status,
			"created_at": createdAt,
			"updated_at": updatedAt,
		}
		if nickname != nil {
			s["nickname"] = *nickname
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// ListMessages returns messages for a session.
func ListMessages(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, role, content, created_at FROM session_messages
			 WHERE session_id = ? ORDER BY created_at ASC LIMIT 200`, sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var messages []map[string]string
		for rows.Next() {
			var id, role, content, createdAt string
			if err := rows.Scan(&id, &role, &content, &createdAt); err != nil {
				continue
			}
			messages = append(messages, map[string]string{
				"id":         id,
				"role":       role,
				"content":    content,
				"created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	}
}

// PostMessage sends a message to a session via the pipeline.
func PostMessage(p pipeline.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		var req struct {
			Content string `json:"content"`
			AgentID string `json:"agent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Content == "" {
			writeError(w, http.StatusBadRequest, "content is required")
			return
		}
		if req.AgentID == "" {
			req.AgentID = "default"
		}

		input := pipeline.Input{
			Content:   req.Content,
			SessionID: sessionID,
			AgentID:   req.AgentID,
			AgentName: "Goboticus",
			Platform:  "api",
		}

		outcome, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetAPI(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}

// ArchiveSession sets a session's status to "archived".
func ArchiveSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		res, err := store.ExecContext(r.Context(),
			`UPDATE sessions SET status = 'archived' WHERE id = ?`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "archived", "id": id})
	}
}

// BackfillNicknames generates nicknames for sessions that lack them.
// It uses the first user message (truncated to 50 chars) as the nickname.
func BackfillNicknames(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rows, err := store.QueryContext(ctx,
			`SELECT s.id, (
				SELECT content FROM session_messages
				WHERE session_id = s.id AND role = 'user'
				ORDER BY created_at ASC LIMIT 1
			) AS first_msg
			FROM sessions s
			WHERE s.nickname IS NULL OR s.nickname = ''`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var updated int
		for rows.Next() {
			var id string
			var firstMsg *string
			if err := rows.Scan(&id, &firstMsg); err != nil {
				continue
			}
			nick := "Untitled"
			if firstMsg != nil && *firstMsg != "" {
				nick = *firstMsg
				if len(nick) > 50 {
					nick = nick[:50]
				}
			}
			_, err := store.ExecContext(ctx,
				`UPDATE sessions SET nickname = ? WHERE id = ?`, nick, id)
			if err == nil {
				updated++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
	}
}

// AnalyzeSession returns basic analytics for a session.
func AnalyzeSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ctx := r.Context()

		// Verify session exists and get timestamps.
		var createdAt, updatedAt string
		row := store.QueryRowContext(ctx,
			`SELECT created_at, updated_at FROM sessions WHERE id = ?`, id)
		if err := row.Scan(&createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		// Message count.
		var msgCount int64
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, id)
		if err := row.Scan(&msgCount); err != nil {
			log.Warn().Err(err).Str("metric", "msg_count").Msg("scan failed")
		}

		// Turn count (user messages = turns).
		var turnCount int64
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM session_messages WHERE session_id = ? AND role = 'user'`, id)
		if err := row.Scan(&turnCount); err != nil {
			log.Warn().Err(err).Str("metric", "turn_count").Msg("scan failed")
		}

		// Duration in seconds.
		var durationSec float64
		t0, err0 := time.Parse(time.RFC3339, createdAt)
		t1, err1 := time.Parse(time.RFC3339, updatedAt)
		if err0 == nil && err1 == nil {
			durationSec = math.Round(t1.Sub(t0).Seconds())
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id":    id,
			"message_count": msgCount,
			"turn_count":    turnCount,
			"duration_sec":  durationSec,
			"created_at":    createdAt,
			"updated_at":    updatedAt,
		})
	}
}
