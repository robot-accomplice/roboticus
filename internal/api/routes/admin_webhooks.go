package routes

import (
	"crypto/subtle"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"

	"roboticus/internal/channel"
	"roboticus/internal/pipeline"
)

type webhookBatchParser interface {
	ProcessWebhookBatch(data []byte) ([]channel.InboundMessage, error)
}

type whatsappWebhookVerifier interface {
	VerifyWebhook(mode, token, challenge string) (string, bool)
	ValidateWebhookSignature(body []byte, signature string) bool
}

func inboundToPipelineInput(msg channel.InboundMessage) pipeline.Input {
	channel.SanitizeInbound(&msg)
	return pipeline.Input{
		Content:  msg.Content,
		Platform: msg.Platform,
		SenderID: msg.SenderID,
		ChatID:   msg.ChatID,
		Claim: &pipeline.ChannelClaimContext{
			SenderID:            msg.SenderID,
			ChatID:              msg.ChatID,
			Platform:            msg.Platform,
			SenderInAllowlist:   true,
			AllowlistConfigured: true,
		},
	}
}

// --- Webhooks ---

// WebhookTelegram handles inbound Telegram webhook messages.
// The adapter owns Telegram payload normalization; the route only bridges
// normalized inbound messages into the pipeline.
func WebhookTelegram(p pipeline.Runner, parser webhookBatchParser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if parser == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "note": "telegram adapter not configured"})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Warn().Err(err).Msg("telegram webhook: failed to read body")
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		msgs, err := parser.ProcessWebhookBatch(body)
		if err != nil {
			log.Warn().Err(err).Msg("telegram webhook: invalid JSON body")
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if len(msgs) == 0 {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "note": "non-text update ignored"})
			return
		}

		msg := msgs[0]
		if msg.Content == "" {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "note": "non-text update ignored"})
			return
		}
		outcome, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetChannel(msg.Platform), inboundToPipelineInput(msg))
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
func WebhookWhatsAppVerify(verifier whatsappWebhookVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if verifier == nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		if mode != "subscribe" || challenge == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		expected, ok := verifier.VerifyWebhook(mode, token, challenge)
		if !ok || subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(expected)); err != nil {
			log.Trace().Err(err).Msg("webhook: challenge response write failed")
		}
	}
}

// WebhookWhatsApp handles inbound WhatsApp messages from the Meta webhook API.
// The adapter owns signature verification and payload normalization; the route
// only bridges normalized inbound messages into the pipeline.
func WebhookWhatsApp(p pipeline.Runner, parser webhookBatchParser, verifier whatsappWebhookVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if parser == nil || verifier == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "note": "whatsapp adapter not configured"})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Warn().Err(err).Msg("whatsapp webhook: failed to read body")
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if !verifier.ValidateWebhookSignature(body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		msgs, err := parser.ProcessWebhookBatch(body)
		if err != nil {
			log.Warn().Err(err).Msg("whatsapp webhook: invalid JSON body")
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		processed := 0
		for _, msg := range msgs {
			if msg.Content == "" {
				continue
			}
			_, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetChannel(msg.Platform), inboundToPipelineInput(msg))
			if err != nil {
				log.Error().Err(err).Str("platform", "whatsapp").Str("from", msg.SenderID).Msg("pipeline error on webhook")
			} else {
				processed++
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"processed": processed,
		})
	}
}
