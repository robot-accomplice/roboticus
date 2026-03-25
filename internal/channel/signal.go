package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/core"
)

// SignalConfig holds Signal adapter configuration.
type SignalConfig struct {
	PhoneNumber    string   `mapstructure:"phone_number"`
	DaemonURL      string   `mapstructure:"daemon_url"` // signal-cli JSON-RPC endpoint
	AllowedNumbers []string `mapstructure:"allowed_numbers"`
	DenyOnEmpty    bool     `mapstructure:"deny_on_empty"`
	BufferSize     int      `mapstructure:"buffer_size"` // bounded buffer, default 256
}

// SignalAdapter implements Adapter for Signal via signal-cli JSON-RPC daemon.
// Fixes roboticus bugs: bounded channel (not unbounded VecDeque), rate limiting,
// no std::sync::Mutex in async context.
type SignalAdapter struct {
	cfg     SignalConfig
	client  *http.Client
	mu      sync.Mutex
	inbound chan InboundMessage // bounded buffer
	limiter *core.RateLimiter
}

// NewSignalAdapter creates a Signal channel adapter.
func NewSignalAdapter(cfg SignalConfig) *SignalAdapter {
	if cfg.DaemonURL == "" {
		cfg.DaemonURL = "http://localhost:8080"
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 256
	}
	if len(cfg.AllowedNumbers) == 0 {
		cfg.DenyOnEmpty = true
	}

	return &SignalAdapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		inbound: make(chan InboundMessage, cfg.BufferSize),
		limiter: core.NewRateLimiter(30, time.Minute),
	}
}

func (s *SignalAdapter) PlatformName() string { return "signal" }

// PushMessage adds a message from the webhook handler into the bounded buffer.
// Drops the message if the buffer is full (backpressure).
func (s *SignalAdapter) PushMessage(msg InboundMessage) bool {
	select {
	case s.inbound <- msg:
		return true
	default:
		log.Warn().Msg("signal: inbound buffer full, dropping message")
		return false
	}
}

// Recv returns the next buffered inbound message (non-blocking).
func (s *SignalAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	select {
	case msg := <-s.inbound:
		return &msg, nil
	default:
		return nil, nil
	}
}

// Send sends a message via signal-cli JSON-RPC.
func (s *SignalAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	if !s.limiter.Allow() {
		return fmt.Errorf("signal: rate limit exceeded")
	}

	if !ValidateE164(msg.RecipientID) {
		return fmt.Errorf("signal: invalid phone number: %s", msg.RecipientID)
	}

	// Best-effort typing indicator.
	s.sendTyping(ctx, msg.RecipientID)

	return s.rpcCall(ctx, "send", map[string]any{
		"account":    s.cfg.PhoneNumber,
		"recipients": []string{msg.RecipientID},
		"message":    msg.Content,
	})
}

func (s *SignalAdapter) sendTyping(ctx context.Context, recipient string) {
	_ = s.rpcCall(ctx, "sendTyping", map[string]any{
		"account":   s.cfg.PhoneNumber,
		"recipient": recipient,
	})
}

func (s *SignalAdapter) rpcCall(ctx context.Context, method string, params map[string]any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", s.cfg.DaemonURL+"/api/v1/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("signal rpc %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal rpc %s %d: %s", method, resp.StatusCode, string(respBody))
	}

	var rpcResp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("signal rpc decode: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("signal rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return nil
}

// ProcessWebhook handles an incoming Signal message from the signal-cli daemon.
func (s *SignalAdapter) ProcessWebhook(data []byte) (*InboundMessage, error) {
	var envelope struct {
		Envelope struct {
			Source       string `json:"source"`
			SourceNumber string `json:"sourceNumber"`
			Timestamp    int64  `json:"timestamp"`
			DataMessage  *struct {
				Message   string `json:"message"`
				GroupInfo *struct {
					GroupID string `json:"groupId"`
				} `json:"groupInfo"`
			} `json:"dataMessage"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("signal webhook decode: %w", err)
	}

	env := envelope.Envelope
	if env.DataMessage == nil || env.DataMessage.Message == "" {
		return nil, nil
	}

	sender := env.SourceNumber
	if sender == "" {
		sender = env.Source
	}
	if !s.isSenderAllowed(sender) {
		log.Debug().Str("sender", sender).Msg("signal: sender not in allowlist")
		return nil, nil
	}

	chatID := sender
	if env.DataMessage.GroupInfo != nil && env.DataMessage.GroupInfo.GroupID != "" {
		chatID = "group:" + env.DataMessage.GroupInfo.GroupID
	}

	msg := &InboundMessage{
		ID:        fmt.Sprintf("sig-%d", env.Timestamp),
		Platform:  "signal",
		SenderID:  sender,
		ChatID:    chatID,
		Content:   env.DataMessage.Message,
		Timestamp: time.UnixMilli(env.Timestamp),
	}

	return msg, nil
}

func (s *SignalAdapter) isSenderAllowed(sender string) bool {
	if len(s.cfg.AllowedNumbers) == 0 {
		return !s.cfg.DenyOnEmpty
	}
	for _, num := range s.cfg.AllowedNumbers {
		if num == sender {
			return true
		}
	}
	return false
}
