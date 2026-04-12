package routes

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"

	"roboticus/internal/pipeline"
)

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
		if _, err := w.Write([]byte(challenge)); err != nil {
			log.Trace().Err(err).Msg("webhook: challenge response write failed")
		}
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
