package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/core"
)

// TelegramConfig holds Telegram Bot API connection parameters.
type TelegramConfig struct {
	Token          string  `mapstructure:"token"`
	PollTimeout    int     `mapstructure:"poll_timeout"` // seconds, default 30
	AllowedChatIDs []int64 `mapstructure:"allowed_chat_ids"`
	DenyOnEmpty    bool    `mapstructure:"deny_on_empty"` // secure default: true
	WebhookSecret  string  `mapstructure:"webhook_secret"`
}

// TelegramAdapter implements Adapter for the Telegram Bot API.
type TelegramAdapter struct {
	cfg           TelegramConfig
	client        core.HTTPDoer
	lastUpdateID  int64
	mu            sync.Mutex
	messageBuffer []InboundMessage
}

// NewTelegramAdapter creates a Telegram channel adapter.
func NewTelegramAdapter(cfg TelegramConfig) *TelegramAdapter {
	return NewTelegramAdapterWithHTTP(cfg, nil)
}

// NewTelegramAdapterWithHTTP creates a Telegram adapter with an injected HTTP client.
func NewTelegramAdapterWithHTTP(cfg TelegramConfig, httpClient core.HTTPDoer) *TelegramAdapter {
	if cfg.PollTimeout == 0 {
		cfg.PollTimeout = 30
	}
	if len(cfg.AllowedChatIDs) == 0 {
		cfg.DenyOnEmpty = true
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: time.Duration(cfg.PollTimeout+10) * time.Second,
		}
	}
	return &TelegramAdapter{
		cfg:    cfg,
		client: httpClient,
	}
}

func (t *TelegramAdapter) PlatformName() string { return "telegram" }

func (t *TelegramAdapter) apiURL(method string) string {
	return fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.cfg.Token, method)
}

// Recv polls getUpdates and returns the next buffered message.
func (t *TelegramAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	t.mu.Lock()
	if len(t.messageBuffer) > 0 {
		msg := t.messageBuffer[0]
		t.messageBuffer = t.messageBuffer[1:]
		t.mu.Unlock()
		return &msg, nil
	}
	lastID := t.lastUpdateID
	t.mu.Unlock()

	params := map[string]any{
		"timeout":         t.cfg.PollTimeout,
		"allowed_updates": []string{"message"},
	}
	if lastID > 0 {
		params["offset"] = lastID + 1
	}

	body, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, "POST", t.apiURL("getUpdates"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		if secs, err := strconv.Atoi(retryAfter); err == nil {
			time.Sleep(time.Duration(secs) * time.Second)
		}
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram getUpdates: %d", resp.StatusCode)
	}

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				MessageID int64 `json:"message_id"`
				From      *struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				} `json:"from"`
				Chat *struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				Date int64  `json:"date"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram decode: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram API returned ok=false")
	}

	var messages []InboundMessage
	for _, update := range result.Result {
		t.mu.Lock()
		if update.UpdateID > t.lastUpdateID {
			t.lastUpdateID = update.UpdateID
		}
		t.mu.Unlock()

		msg := update.Message
		if msg == nil || msg.Text == "" {
			continue
		}

		chatID := strconv.FormatInt(msg.Chat.ID, 10)
		if !t.isChatAllowed(msg.Chat.ID) {
			log.Debug().Str("chat_id", chatID).Msg("telegram: chat not in allowlist")
			continue
		}

		senderID := ""
		if msg.From != nil {
			senderID = strconv.FormatInt(msg.From.ID, 10)
		}

		messages = append(messages, InboundMessage{
			ID:        strconv.FormatInt(msg.MessageID, 10),
			Platform:  "telegram",
			SenderID:  senderID,
			ChatID:    chatID,
			Content:   msg.Text,
			Timestamp: time.Unix(msg.Date, 0),
		})
	}

	if len(messages) == 0 {
		return nil, nil
	}

	t.mu.Lock()
	t.messageBuffer = append(t.messageBuffer, messages[1:]...)
	t.mu.Unlock()
	return &messages[0], nil
}

func (t *TelegramAdapter) isChatAllowed(chatID int64) bool {
	if len(t.cfg.AllowedChatIDs) == 0 {
		return !t.cfg.DenyOnEmpty
	}
	for _, id := range t.cfg.AllowedChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

// Send sends a message through the Telegram Bot API.
// Sends typing indicator first, chunks at 4096 chars,
// tries MarkdownV2 with fallback to plain text.
func (t *TelegramAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	// Best-effort typing indicator.
	t.sendChatAction(ctx, msg.RecipientID, "typing")

	chunks := chunkText(msg.Content, 4096)
	for _, chunk := range chunks {
		if err := t.sendMessage(ctx, msg.RecipientID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (t *TelegramAdapter) sendMessage(ctx context.Context, chatID, text string) error {
	// Try MarkdownV2 first.
	err := t.postMessage(ctx, chatID, text, "MarkdownV2")
	if err != nil {
		// Fallback to plain text.
		return t.postMessage(ctx, chatID, text, "")
	}
	return nil
}

func (t *TelegramAdapter) postMessage(ctx context.Context, chatID, text, parseMode string) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", t.apiURL("sendMessage"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram sendMessage %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (t *TelegramAdapter) sendChatAction(ctx context.Context, chatID, action string) {
	payload := map[string]any{
		"chat_id": chatID,
		"action":  action,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", t.apiURL("sendChatAction"), bytes.NewReader(body))
	if req == nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

// ProcessWebhook handles an incoming Telegram webhook payload.
func (t *TelegramAdapter) ProcessWebhook(data []byte) (*InboundMessage, error) {
	var update struct {
		UpdateID int64 `json:"update_id"`
		Message  *struct {
			MessageID int64 `json:"message_id"`
			From      *struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"from"`
			Chat *struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			Date int64  `json:"date"`
			Text string `json:"text"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &update); err != nil {
		return nil, fmt.Errorf("telegram webhook decode: %w", err)
	}
	msg := update.Message
	if msg == nil || msg.Text == "" {
		return nil, nil
	}

	chatID := msg.Chat.ID
	if !t.isChatAllowed(chatID) {
		return nil, nil
	}

	senderID := ""
	if msg.From != nil {
		senderID = strconv.FormatInt(msg.From.ID, 10)
	}

	return &InboundMessage{
		ID:        strconv.FormatInt(msg.MessageID, 10),
		Platform:  "telegram",
		SenderID:  senderID,
		ChatID:    strconv.FormatInt(chatID, 10),
		Content:   msg.Text,
		Timestamp: time.Unix(msg.Date, 0),
	}, nil
}

// chunkText splits text into chunks of at most maxLen bytes.
func chunkText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	for len(text) > 0 {
		end := maxLen
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[:end])
		text = text[end:]
	}
	return chunks
}
