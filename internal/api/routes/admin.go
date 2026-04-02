package routes

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"goboticus/internal/core"
	"goboticus/internal/db"
	"goboticus/internal/llm"
	"goboticus/internal/pipeline"
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
				continue
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
		writeJSON(w, http.StatusOK, map[string]any{"feedback": nil})
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
			_ = row.Scan(&req.SessionID)
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
			writeJSON(w, http.StatusOK, map[string]any{"skills": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		skills := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, kind, riskLevel, createdAt, version string
			var description *string
			var enabled bool
			if err := rows.Scan(&id, &name, &kind, &description, &enabled, &version, &riskLevel, &createdAt); err != nil {
				continue
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

// ReloadSkills reloads all skills from disk.
func ReloadSkills() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "skill reload not yet implemented")
	}
}

// --- Stats ---

// GetCosts returns cost statistics.
func GetCosts(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row := store.QueryRowContext(r.Context(),
			`SELECT COALESCE(SUM(cost), 0), COALESCE(SUM(tokens_in), 0), COALESCE(SUM(tokens_out), 0),
			        COUNT(*) FROM inference_costs`)
		var totalCost float64
		var tokensIn, tokensOut, count int64
		if err := row.Scan(&totalCost, &tokensIn, &tokensOut, &count); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"total_cost": 0, "requests": 0})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"total_cost": totalCost,
			"tokens_in":  tokensIn,
			"tokens_out": tokensOut,
			"requests":   count,
		})
	}
}

// GetCacheStats returns semantic cache statistics.
func GetCacheStats(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row := store.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM semantic_cache`)
		var count int64
		_ = row.Scan(&count)
		writeJSON(w, http.StatusOK, map[string]any{"cached_entries": count})
	}
}

// --- Models ---

// GetAvailableModels returns configured LLM providers.
func GetAvailableModels(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := llmSvc.Status()
		writeJSON(w, http.StatusOK, map[string]any{"models": providers})
	}
}

// --- Channels ---

// GetChannelsStatus returns channel adapter health.
func GetChannelsStatus(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "channel health checks not yet implemented")
	}
}

// GetDeadLetters returns dead letter queue entries.
func GetDeadLetters(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, channel, recipient_id, content, last_error, created_at
			 FROM delivery_queue WHERE status = 'dead_letter' ORDER BY created_at DESC LIMIT 50`)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"dead_letters": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]string
		for rows.Next() {
			var id, channel, recipient, content, createdAt string
			var errMsg *string
			if err := rows.Scan(&id, &channel, &recipient, &content, &errMsg, &createdAt); err != nil {
				continue
			}
			e := map[string]string{
				"id": id, "channel": channel, "recipient": recipient,
				"content": content, "created_at": createdAt,
			}
			if errMsg != nil {
				e["error"] = *errMsg
			}
			entries = append(entries, e)
		}
		if entries == nil {
			entries = []map[string]string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"dead_letters": entries})
	}
}

// --- Config ---

// GetConfig returns the current configuration.
func GetConfig(cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfg)
	}
}

// GetCapabilities returns agent capabilities.
func GetCapabilities() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"capabilities": []string{
				"chat", "tool-use", "multi-model", "memory",
				"multi-channel", "scheduling", "multi-agent",
			},
		})
	}
}

// --- Circuit Breaker ---

// BreakerStatus returns circuit breaker status for all providers.
func BreakerStatus(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := llmSvc.Status()
		writeJSON(w, http.StatusOK, map[string]any{"breakers": providers})
	}
}

// BreakerReset resets a provider's circuit breaker.
func BreakerReset(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		if err := llmSvc.ResetBreaker(provider); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "reset", "provider": provider})
	}
}

// --- Subagents ---

// ListSubagents returns registered subagents.
func ListSubagents(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, name, display_name, model, role, description, enabled, created_at
			 FROM sub_agents ORDER BY created_at DESC`)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"subagents": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		var agents []map[string]any
		for rows.Next() {
			var id, name, model, role, createdAt string
			var displayName, description *string
			var enabled bool
			if err := rows.Scan(&id, &name, &displayName, &model, &role, &description, &enabled, &createdAt); err != nil {
				continue
			}
			a := map[string]any{
				"id": id, "name": name, "model": model,
				"role": role, "enabled": enabled, "created_at": createdAt,
			}
			if displayName != nil {
				a["display_name"] = *displayName
			}
			if description != nil {
				a["description"] = *description
			}
			agents = append(agents, a)
		}
		if agents == nil {
			agents = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"subagents": agents})
	}
}

// CreateSubagent registers a new subagent.
func CreateSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name         string   `json:"name"`
			Model        string   `json:"model"`
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		skillsJSON, _ := json.Marshal(req.Capabilities)
		id := db.NewID()
		_, _ = store.ExecContext(r.Context(),
			`INSERT INTO sub_agents (id, name, model, skills_json, enabled)
			 VALUES (?, ?, ?, ?, 1)`,
			id, req.Name, req.Model, string(skillsJSON),
		)
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	}
}

// --- WebSocket Ticket ---

// TicketIssuer creates single-use authentication tickets.
type TicketIssuer interface {
	Issue() string
}

// IssueWSTicket creates a single-use WebSocket auth ticket.
// If no ticket store is wired, returns 501.
func IssueWSTicket(issuer ...TicketIssuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(issuer) == 0 || issuer[0] == nil {
			writeError(w, http.StatusNotImplemented, "WebSocket ticket store not configured")
			return
		}
		ticket := issuer[0].Issue()
		writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
	}
}

// --- Webhooks ---

// WebhookTelegram handles inbound Telegram webhook messages.
// Returns 200 to prevent retry storms from Telegram servers, but logs a warning
// that the adapter is not yet wired. Will be implemented in Phase 2 (channel adapters).
func WebhookTelegram(p pipeline.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Warn().Str("path", r.URL.Path).Msg("Telegram webhook received but adapter not wired — message dropped")
		writeJSON(w, http.StatusOK, map[string]string{"status": "dropped", "reason": "adapter not configured"})
	}
}

// WebhookWhatsAppVerify handles WhatsApp webhook verification challenge.
// verifyToken must match the token configured in the Meta developer console.
func WebhookWhatsAppVerify(verifyToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		if mode != "subscribe" || challenge == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if verifyToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(verifyToken)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(challenge))
	}
}

// WebhookWhatsApp handles inbound WhatsApp messages.
// Returns 200 to prevent retry storms from Meta servers, but logs a warning
// that the adapter is not yet wired. Will be implemented in Phase 2 (channel adapters).
func WebhookWhatsApp(p pipeline.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Warn().Str("path", r.URL.Path).Msg("WhatsApp webhook received but adapter not wired — message dropped")
		writeJSON(w, http.StatusOK, map[string]string{"status": "dropped", "reason": "adapter not configured"})
	}
}
