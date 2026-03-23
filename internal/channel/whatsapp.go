package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// WhatsAppConfig holds WhatsApp Cloud API configuration.
type WhatsAppConfig struct {
	Token          string   `mapstructure:"token"`            // Cloud API access token
	PhoneNumberID  string   `mapstructure:"phone_number_id"`  // Business phone number ID
	VerifyToken    string   `mapstructure:"verify_token"`     // Webhook challenge token
	AppSecret      string   `mapstructure:"app_secret"`       // Webhook HMAC-SHA256 verification
	APIVersion     string   `mapstructure:"api_version"`      // Graph API version, default v21.0
	AllowedNumbers []string `mapstructure:"allowed_numbers"`
	DenyOnEmpty    bool     `mapstructure:"deny_on_empty"`
}

// WhatsAppAdapter implements Adapter for the WhatsApp Cloud API.
type WhatsAppAdapter struct {
	cfg           WhatsAppConfig
	client        *http.Client
	mu            sync.Mutex
	messageBuffer []InboundMessage
}

// NewWhatsAppAdapter creates a WhatsApp channel adapter.
func NewWhatsAppAdapter(cfg WhatsAppConfig) *WhatsAppAdapter {
	if cfg.APIVersion == "" {
		cfg.APIVersion = "v21.0"
	}
	if len(cfg.AllowedNumbers) == 0 {
		cfg.DenyOnEmpty = true
	}
	return &WhatsAppAdapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (w *WhatsAppAdapter) PlatformName() string { return "whatsapp" }

func (w *WhatsAppAdapter) apiURL(path string) string {
	return fmt.Sprintf("https://graph.facebook.com/%s/%s/%s", w.cfg.APIVersion, w.cfg.PhoneNumberID, path)
}

// Recv returns buffered inbound messages (webhook-driven, no polling).
func (w *WhatsAppAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.messageBuffer) == 0 {
		return nil, nil
	}
	msg := w.messageBuffer[0]
	w.messageBuffer = w.messageBuffer[1:]
	return &msg, nil
}

// Send sends a text message via the WhatsApp Cloud API.
func (w *WhatsAppAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	if !ValidateE164(msg.RecipientID) {
		return fmt.Errorf("whatsapp: invalid phone number: %s", msg.RecipientID)
	}

	// Mark as read (typing indicator equivalent).
	w.markRead(ctx, msg.RecipientID)

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                msg.RecipientID,
		"type":              "text",
		"text": map[string]string{
			"body": msg.Content,
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", w.apiURL("messages"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.cfg.Token)

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp send %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (w *WhatsAppAdapter) markRead(ctx context.Context, messageID string) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", w.apiURL("messages"), bytes.NewReader(body))
	if req == nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.cfg.Token)
	resp, err := w.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// VerifyWebhook handles the WhatsApp webhook challenge verification.
func (w *WhatsAppAdapter) VerifyWebhook(mode, token, challenge string) (string, bool) {
	if mode == "subscribe" && token == w.cfg.VerifyToken {
		return challenge, true
	}
	return "", false
}

// ValidateWebhookSignature checks the X-Hub-Signature-256 header.
func (w *WhatsAppAdapter) ValidateWebhookSignature(body []byte, signature string) bool {
	if w.cfg.AppSecret == "" {
		return true // no secret configured, skip validation
	}
	mac := hmac.New(sha256.New, []byte(w.cfg.AppSecret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ProcessWebhook parses an incoming WhatsApp webhook payload.
func (w *WhatsAppAdapter) ProcessWebhook(data []byte) (*InboundMessage, error) {
	var webhook struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Messages []struct {
						ID        string `json:"id"`
						From      string `json:"from"`
						Timestamp string `json:"timestamp"`
						Type      string `json:"type"`
						Text      *struct {
							Body string `json:"body"`
						} `json:"text"`
						Image *struct {
							ID      string `json:"id"`
							Caption string `json:"caption"`
						} `json:"image"`
						Video *struct {
							ID      string `json:"id"`
							Caption string `json:"caption"`
						} `json:"video"`
						Audio *struct {
							ID string `json:"id"`
						} `json:"audio"`
						Document *struct {
							ID       string `json:"id"`
							Filename string `json:"filename"`
						} `json:"document"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(data, &webhook); err != nil {
		return nil, fmt.Errorf("whatsapp webhook decode: %w", err)
	}

	for _, entry := range webhook.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if !w.isSenderAllowed(msg.From) {
					log.Debug().Str("from", msg.From).Msg("whatsapp: sender not in allowlist")
					continue
				}

				inbound := &InboundMessage{
					ID:        msg.ID,
					Platform:  "whatsapp",
					SenderID:  msg.From,
					ChatID:    msg.From,
					Timestamp: time.Now(),
				}

				switch msg.Type {
				case "text":
					if msg.Text != nil {
						inbound.Content = msg.Text.Body
					}
				case "image":
					if msg.Image != nil {
						inbound.Content = msg.Image.Caption
						inbound.Media = append(inbound.Media, MediaAttachment{
							Type:    MediaImage,
							Caption: msg.Image.Caption,
						})
					}
				case "video":
					if msg.Video != nil {
						inbound.Content = msg.Video.Caption
						inbound.Media = append(inbound.Media, MediaAttachment{
							Type:    MediaVideo,
							Caption: msg.Video.Caption,
						})
					}
				case "audio":
					if msg.Audio != nil {
						inbound.Media = append(inbound.Media, MediaAttachment{
							Type: MediaAudio,
						})
					}
				case "document":
					if msg.Document != nil {
						inbound.Media = append(inbound.Media, MediaAttachment{
							Type:     MediaDocument,
							Filename: msg.Document.Filename,
						})
					}
				}

				if inbound.Content == "" && len(inbound.Media) == 0 {
					continue
				}

				w.mu.Lock()
				w.messageBuffer = append(w.messageBuffer, *inbound)
				w.mu.Unlock()
				return inbound, nil
			}
		}
	}
	return nil, nil
}

func (w *WhatsAppAdapter) isSenderAllowed(sender string) bool {
	if len(w.cfg.AllowedNumbers) == 0 {
		return !w.cfg.DenyOnEmpty
	}
	for _, num := range w.cfg.AllowedNumbers {
		if num == sender {
			return true
		}
	}
	return false
}
