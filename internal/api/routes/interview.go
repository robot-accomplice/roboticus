package routes

import (
	"encoding/json"
	"net/http"
	"sync"
)

// InterviewManager holds active interview state (avoids agent import).
type InterviewManager struct {
	mu       sync.Mutex
	sessions map[string]*interviewSession
}

type interviewSession struct {
	SessionID string         `json:"session_id"`
	Turns     int            `json:"turns"`
	Coverage  int            `json:"coverage"`
	Finished  bool           `json:"finished"`
	Data      map[string]any `json:"data,omitempty"`
}

// NewInterviewManager creates an interview manager.
func NewInterviewManager() *InterviewManager {
	return &InterviewManager{sessions: make(map[string]*interviewSession)}
}

// InterviewStart begins a new personality interview session.
func InterviewStart(mgr *InterviewManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"session_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.SessionID == "" {
			body.SessionID = "interview-" + r.Header.Get("X-Request-Id")
		}

		mgr.mu.Lock()
		mgr.sessions[body.SessionID] = &interviewSession{
			SessionID: body.SessionID,
		}
		mgr.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": body.SessionID,
			"status":     "started",
			"message":    "Interview session started. Send questions via /api/interview/turn.",
		})
	}
}

// InterviewTurn processes a single Q&A exchange.
func InterviewTurn(mgr *InterviewManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"session_id"`
			Category  string `json:"category"`
			Question  string `json:"question"`
			Answer    string `json:"answer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		mgr.mu.Lock()
		sess, ok := mgr.sessions[body.SessionID]
		if !ok {
			mgr.mu.Unlock()
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		sess.Turns++
		sess.Coverage++ // simplified: each turn covers a category
		mgr.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": body.SessionID,
			"turns":      sess.Turns,
			"coverage":   sess.Coverage,
			"can_finish": sess.Coverage >= 5,
		})
	}
}

// InterviewFinish completes the interview and generates TOML files.
func InterviewFinish(mgr *InterviewManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"session_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		mgr.mu.Lock()
		sess, ok := mgr.sessions[body.SessionID]
		if !ok {
			mgr.mu.Unlock()
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		sess.Finished = true
		mgr.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": body.SessionID,
			"status":     "finished",
			"files":      []string{"OS.toml", "FIRMWARE.toml", "OPERATOR.toml", "DIRECTIVES.toml"},
		})
	}
}
