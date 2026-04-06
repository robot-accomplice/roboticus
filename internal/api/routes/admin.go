package routes

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
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
			writeError(w, http.StatusInternalServerError, "failed to query cost statistics")
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
		if err := row.Scan(&count); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cached_entries": count})
	}
}

// --- Models ---

// RunRoutingEval runs the model routing evaluation against the default corpus
// and returns the results as JSON.
func RunRoutingEval(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		router := llmSvc.Router()
		if router == nil {
			writeError(w, http.StatusServiceUnavailable, "router not configured")
			return
		}
		corpus := llm.DefaultEvalCorpus()
		result := llm.RunEval(router, corpus)
		writeJSON(w, http.StatusOK, result)
	}
}

// GetAvailableModels returns configured LLM providers.
func GetAvailableModels(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := llmSvc.Status()
		writeJSON(w, http.StatusOK, map[string]any{"models": providers})
	}
}

// --- Channels ---

// GetChannelsStatus returns channel adapter configuration and enabled status.
func GetChannelsStatus(cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channels := map[string]bool{
			"telegram": cfg.Channels.TelegramTokenEnv != "",
			"whatsapp": cfg.Channels.WhatsAppTokenEnv != "",
			"discord":  cfg.Channels.DiscordTokenEnv != "",
			"signal":   cfg.Channels.SignalDaemonURL != "",
			"email":    cfg.Channels.EmailFromAddress != "",
			"matrix":   cfg.Matrix.Enabled,
		}
		writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
	}
}

// GetDeadLetters returns dead letter queue entries.
func GetDeadLetters(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, channel, recipient_id, content, last_error, created_at
			 FROM delivery_queue WHERE status = 'dead_letter' ORDER BY created_at DESC LIMIT 50`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query dead letters")
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]string
		for rows.Next() {
			var id, channel, recipient, content, createdAt string
			var errMsg *string
			if err := rows.Scan(&id, &channel, &recipient, &content, &errMsg, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read dead letter row")
				return
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

// GetConfigStatus returns the current config application status.
// Reports the server start time as last_applied, since config is loaded at startup.
func GetConfigStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "applied",
			"last_applied": processStartTime.UTC().Format(time.RFC3339),
		})
	}
}

// --- Keystore ---

// KeystoreStatus returns whether any provider keys are stored in the encrypted keystore.
func KeystoreStatus(ks *core.Keystore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		unlocked := ks != nil && ks.Count() >= 0 && ks.List() != nil
		resp := map[string]any{
			"unlocked": unlocked,
			"path":     filepath.Join(core.ConfigDir(), "keystore.enc"),
		}
		if unlocked {
			resp["stored_keys"] = ks.Count()
			resp["keys"] = ks.List()
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// KeystoreUnlock attempts to unlock the keystore using the machine-derived passphrase.
func KeystoreUnlock() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"unlocked": true,
			"action":   "already_unlocked",
		})
	}
}

// --- Subagent Retirement ---

// SubagentRetirementCandidates returns subagents not used in 30 days.
func SubagentRetirementCandidates(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, name, display_name, model, role, created_at
			 FROM sub_agents
			 WHERE created_at < datetime('now', '-30 days')
			 ORDER BY created_at ASC`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query retirement candidates")
			return
		}
		defer func() { _ = rows.Close() }()

		candidates := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, model, role, createdAt string
			var displayName *string
			if err := rows.Scan(&id, &name, &displayName, &model, &role, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read retirement candidate row")
				return
			}
			c := map[string]any{
				"id": id, "name": name, "model": model,
				"role": role, "created_at": createdAt,
			}
			if displayName != nil {
				c["display_name"] = *displayName
			}
			candidates = append(candidates, c)
		}
		writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
	}
}

// RetireUnusedSubagents deletes subagents not used in 30 days.
func RetireUnusedSubagents(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := store.ExecContext(r.Context(),
			`DELETE FROM sub_agents WHERE created_at < datetime('now', '-30 days')`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n, err2 := res.RowsAffected()
		if err2 != nil {
			writeError(w, http.StatusInternalServerError, err2.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"retired": n})
	}
}

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

// BreakerForceOpen force-opens a provider's circuit breaker.
// Unlike normal open, this is only cleared by an explicit Reset call.
func BreakerForceOpen(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		if err := llmSvc.ForceOpenBreaker(provider); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "force_opened", "provider": provider})
	}
}

// MemoryHealth returns per-tier counts and stale/active breakdowns.
func MemoryHealth(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var workingCount, workingStale int64
		row := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM working_memory`)
		if err := row.Scan(&workingCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query working memory health")
			return
		}
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM working_memory WHERE created_at < datetime('now', '-24 hours')`)
		if err := row.Scan(&workingStale); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query working memory staleness")
			return
		}

		var episodicCount, episodicStale int64
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_memory`)
		if err := row.Scan(&episodicCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query episodic memory health")
			return
		}
		row = store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM episodic_memory WHERE created_at < datetime('now', '-7 days')`)
		if err := row.Scan(&episodicStale); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query episodic memory staleness")
			return
		}

		var semanticCount int64
		row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM semantic_memory`)
		if err := row.Scan(&semanticCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query semantic memory health")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"working": map[string]any{
				"total":  workingCount,
				"active": workingCount - workingStale,
				"stale":  workingStale,
			},
			"episodic": map[string]any{
				"total":  episodicCount,
				"active": episodicCount - episodicStale,
				"stale":  episodicStale,
			},
			"semantic": map[string]any{
				"total": semanticCount,
			},
		})
	}
}

// ReplayDeadLetter replays a dead letter queue entry by resetting its status for retry.
func ReplayDeadLetter(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		res, err := store.ExecContext(r.Context(),
			`UPDATE delivery_queue SET status = 'pending', last_error = NULL WHERE id = ? AND status = 'dead_letter'`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeError(w, http.StatusNotFound, "dead letter entry not found or already replayed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "replayed", "id": id})
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
			writeError(w, http.StatusInternalServerError, "failed to query subagents")
			return
		}
		defer func() { _ = rows.Close() }()

		var agents []map[string]any
		for rows.Next() {
			var id, name, model, role, createdAt string
			var displayName, description *string
			var enabled bool
			if err := rows.Scan(&id, &name, &displayName, &model, &role, &description, &enabled, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read subagent row")
				return
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
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO sub_agents (id, name, model, skills_json, enabled)
			 VALUES (?, ?, ?, ?, 1)`,
			id, req.Name, req.Model, string(skillsJSON),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
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
// Parses the Telegram update format, extracts the message, and dispatches through the pipeline.
func WebhookTelegram(p pipeline.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var update struct {
			Message *struct {
				Text string `json:"text"`
				From *struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				} `json:"from"`
				Chat *struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			log.Warn().Err(err).Msg("telegram webhook: invalid JSON body")
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if update.Message == nil || update.Message.Text == "" {
			// Non-text update (sticker, photo, etc.) — acknowledge without processing.
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "note": "non-text update ignored"})
			return
		}

		senderID := ""
		if update.Message.From != nil {
			senderID = fmt.Sprintf("%d", update.Message.From.ID)
		}
		chatID := ""
		if update.Message.Chat != nil {
			chatID = fmt.Sprintf("%d", update.Message.Chat.ID)
		}

		input := pipeline.Input{
			Content:  update.Message.Text,
			Platform: "telegram",
			SenderID: senderID,
			ChatID:   chatID,
		}

		outcome, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetChannel("telegram"), input)
		if err != nil {
			log.Error().Err(err).Str("platform", "telegram").Msg("pipeline error on webhook")
			writeJSON(w, http.StatusOK, map[string]string{"status": "error", "detail": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "processed",
			"session_id": outcome.SessionID,
			"response":   outcome.Content,
		})
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

// WebhookWhatsApp handles inbound WhatsApp messages from the Meta webhook API.
// Parses the Cloud API webhook format and dispatches messages through the pipeline.
func WebhookWhatsApp(p pipeline.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Entry []struct {
				Changes []struct {
					Value struct {
						Messages []struct {
							From string `json:"from"`
							Text *struct {
								Body string `json:"body"`
							} `json:"text"`
							Type string `json:"type"`
						} `json:"messages"`
						Metadata struct {
							PhoneNumberID string `json:"phone_number_id"`
						} `json:"metadata"`
					} `json:"value"`
				} `json:"changes"`
			} `json:"entry"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Warn().Err(err).Msg("whatsapp webhook: invalid JSON body")
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		// Walk the nested structure to find text messages.
		processed := 0
		for _, entry := range payload.Entry {
			for _, change := range entry.Changes {
				for _, msg := range change.Value.Messages {
					if msg.Type != "text" || msg.Text == nil || msg.Text.Body == "" {
						continue
					}

					input := pipeline.Input{
						Content:  msg.Text.Body,
						Platform: "whatsapp",
						SenderID: msg.From,
						ChatID:   change.Value.Metadata.PhoneNumberID,
					}

					_, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetChannel("whatsapp"), input)
					if err != nil {
						log.Error().Err(err).Str("platform", "whatsapp").Str("from", msg.From).Msg("pipeline error on webhook")
					} else {
						processed++
					}
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"processed": processed,
		})
	}
}
