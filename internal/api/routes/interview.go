package routes

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// InterviewManager holds active interview state and LLM service for conducting
// interviews. This is an approved off-pipeline LLM caller — interviews are
// configuration workflows, not agent-turn behavior.
type InterviewManager struct {
	mu           sync.Mutex
	sessions     map[string]*interviewSession
	llmSvc       *llm.Service
	workspaceDir string
}

type interviewSession struct {
	SessionID string             `json:"session_id"`
	StartedAt time.Time          `json:"started_at"`
	History   []interviewMessage `json:"history"`
	Coverage  int                `json:"coverage"`
	Finished  bool               `json:"finished"`
}

type interviewMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const (
	maxInterviewSessions = 100
	interviewTTL         = 3600 * time.Second
	maxInterviewTurns    = 200
)

// NewInterviewManager creates an interview manager with LLM and workspace deps.
func NewInterviewManager(llmSvc *llm.Service, workspaceDir string) *InterviewManager {
	return &InterviewManager{
		sessions:     make(map[string]*interviewSession),
		llmSvc:       llmSvc,
		workspaceDir: workspaceDir,
	}
}

// evictExpired removes sessions older than the TTL.
func (mgr *InterviewManager) evictExpired() {
	now := time.Now()
	for id, sess := range mgr.sessions {
		if now.Sub(sess.StartedAt) > interviewTTL {
			delete(mgr.sessions, id)
		}
	}
}

// InterviewStart begins a new personality interview session.
func InterviewStart(mgr *InterviewManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"session_id"`
			AgentName string `json:"agent_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.SessionID == "" {
			body.SessionID = "interview-" + db.NewID()[:12]
		}

		mgr.mu.Lock()
		mgr.evictExpired()
		if len(mgr.sessions) >= maxInterviewSessions {
			var oldestID string
			var oldestTime time.Time
			for id, sess := range mgr.sessions {
				if oldestID == "" || sess.StartedAt.Before(oldestTime) {
					oldestID = id
					oldestTime = sess.StartedAt
				}
			}
			delete(mgr.sessions, oldestID)
		}

		sess := &interviewSession{
			SessionID: body.SessionID,
			StartedAt: time.Now(),
		}

		// Generate the opening message from the LLM.
		var openingMessage string
		if mgr.llmSvc != nil {
			sysPrompt := core.InterviewSystemPrompt()
			userMsg := "Begin the personality interview. Introduce yourself and ask the first 2-3 questions."
			if body.AgentName != "" {
				userMsg = "Begin the personality interview for an agent named " + body.AgentName + ". Introduce yourself and ask the first 2-3 questions."
			}

			resp, err := mgr.llmSvc.Complete(r.Context(), &llm.Request{
				Messages: []llm.Message{
					{Role: "system", Content: sysPrompt},
					{Role: "user", Content: userMsg},
				},
				MaxTokens: 1024,
			})
			if err != nil {
				log.Warn().Err(err).Msg("interview: LLM failed to generate opening")
				openingMessage = "Let's get started with your personality interview. What would you like to name your AI agent?"
			} else {
				openingMessage = resp.Content
			}
		} else {
			openingMessage = "Let's get started with your personality interview. What would you like to name your AI agent?"
		}

		sess.History = append(sess.History, interviewMessage{Role: "assistant", Content: openingMessage})
		mgr.sessions[body.SessionID] = sess
		mgr.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": body.SessionID,
			"status":     "started",
			"message":    openingMessage,
		})
	}
}

// InterviewTurn processes a user answer and generates the next questions via LLM.
func InterviewTurn(mgr *InterviewManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"session_id"`
			Answer    string `json:"answer"`
			Category  string `json:"category"` // optional — for backward compat
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Answer == "" {
			writeError(w, http.StatusBadRequest, "answer is required")
			return
		}

		mgr.mu.Lock()
		sess, ok := mgr.sessions[body.SessionID]
		if !ok {
			mgr.mu.Unlock()
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		if sess.Finished {
			mgr.mu.Unlock()
			writeError(w, http.StatusConflict, "interview already finished")
			return
		}
		if len(sess.History) >= maxInterviewTurns {
			mgr.mu.Unlock()
			writeError(w, http.StatusConflict, "interview turn limit reached")
			return
		}

		// Add user answer to history.
		sess.History = append(sess.History, interviewMessage{Role: "user", Content: body.Answer})

		// Heuristic coverage tracking (LLM covers ~1 category per exchange).
		sess.Coverage = len(sess.History) / 2
		if sess.Coverage > 8 {
			sess.Coverage = 8
		}

		// Generate next questions via LLM.
		var nextMessage string
		if mgr.llmSvc != nil {
			sysPrompt := core.InterviewSystemPrompt()
			messages := make([]llm.Message, 0, len(sess.History)+1)
			messages = append(messages, llm.Message{Role: "system", Content: sysPrompt})
			for _, m := range sess.History {
				messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
			}

			resp, err := mgr.llmSvc.Complete(r.Context(), &llm.Request{
				Messages:  messages,
				MaxTokens: 1024,
			})
			if err != nil {
				log.Warn().Err(err).Msg("interview: LLM failed to generate next questions")
				nextMessage = "I'm having trouble generating the next questions. Could you tell me more about your preferences?"
			} else {
				nextMessage = resp.Content
			}
		} else {
			nextMessage = "Thank you. What else would you like to configure?"
		}

		sess.History = append(sess.History, interviewMessage{Role: "assistant", Content: nextMessage})
		canFinish := sess.Coverage >= 5
		mgr.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": body.SessionID,
			"turns":      len(sess.History) / 2,
			"coverage":   sess.Coverage,
			"can_finish": canFinish,
			"message":    nextMessage,
		})
	}
}

// InterviewFinish completes the interview, generates TOML files via LLM,
// and writes them to the workspace directory via core.InterviewOutput.
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
		history := make([]interviewMessage, len(sess.History))
		copy(history, sess.History)
		mgr.mu.Unlock()

		// Generate TOML files via LLM or fallback to templates.
		var files map[string]string

		if mgr.llmSvc != nil {
			messages := make([]llm.Message, 0, len(history)+2)
			messages = append(messages, llm.Message{Role: "system", Content: core.InterviewSystemPrompt()})
			for _, m := range history {
				messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
			}
			messages = append(messages, llm.Message{
				Role: "user",
				Content: "Based on our conversation, generate the 4 TOML configuration files. " +
					"Output each file as a fenced code block with the filename as the info string " +
					"(e.g., ```OS.toml).",
			})

			resp, err := mgr.llmSvc.Complete(r.Context(), &llm.Request{
				Messages:  messages,
				MaxTokens: 4096,
			})
			if err != nil {
				log.Warn().Err(err).Msg("interview: LLM failed to generate TOML files")
				files = defaultInterviewTOML()
			} else {
				files = core.ParseTOMLBlocks(resp.Content)
				if len(files) < 2 {
					log.Warn().Int("blocks", len(files)).Msg("interview: LLM output had too few TOML blocks, using defaults")
					files = defaultInterviewTOML()
				}
			}
		} else {
			files = defaultInterviewTOML()
		}

		// Write files via core service (no os.WriteFile in route layer).
		output := &core.InterviewOutput{Files: files}
		written, err := output.WriteToWorkspace(mgr.workspaceDir)
		if err != nil {
			log.Warn().Err(err).Msg("interview: failed to write personality files")
		}

		log.Info().
			Strs("files", written).
			Int("turns", len(history)/2).
			Msg("interview: personality files generated")

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": body.SessionID,
			"status":     "finished",
			"files":      written,
			"contents":   files,
		})
	}
}

// defaultInterviewTOML returns template TOML files when LLM generation fails.
func defaultInterviewTOML() map[string]string {
	return map[string]string{
		"OS.toml":         core.GenerateOsTOML("Roboticus", "balanced", "suggest", "general"),
		"FIRMWARE.toml":   core.GenerateFirmwareTOML(""),
		"OPERATOR.toml":   "[identity]\n\n[preferences]\n",
		"DIRECTIVES.toml": "[goals]\n\n[integrations]\n",
	}
}
