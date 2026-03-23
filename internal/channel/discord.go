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
)

const (
	discordAPIBase  = "https://discord.com/api/v10"
	discordMaxChunk = 2000
)

// DiscordConfig holds Discord bot connection parameters.
type DiscordConfig struct {
	Token           string   `mapstructure:"token"`
	AllowedGuildIDs []string `mapstructure:"allowed_guild_ids"`
	DenyOnEmpty     bool     `mapstructure:"deny_on_empty"`
}

// DiscordAdapter implements Adapter for Discord.
// Uses REST API for sending. Inbound messages arrive via gateway or webhook.
type DiscordAdapter struct {
	cfg           DiscordConfig
	client        *http.Client
	mu            sync.Mutex
	messageBuffer []InboundMessage
}

// NewDiscordAdapter creates a Discord channel adapter.
func NewDiscordAdapter(cfg DiscordConfig) *DiscordAdapter {
	if len(cfg.AllowedGuildIDs) == 0 {
		cfg.DenyOnEmpty = true
	}
	return &DiscordAdapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (d *DiscordAdapter) PlatformName() string { return "discord" }

// PushMessage adds an inbound message from the gateway or webhook handler.
func (d *DiscordAdapter) PushMessage(msg InboundMessage) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.messageBuffer = append(d.messageBuffer, msg)
}

// Recv returns the next buffered inbound message.
func (d *DiscordAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.messageBuffer) == 0 {
		return nil, nil
	}
	msg := d.messageBuffer[0]
	d.messageBuffer = d.messageBuffer[1:]
	return &msg, nil
}

// Send posts a message to a Discord channel. Chunks at 2000 chars.
func (d *DiscordAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	chunks := chunkText(msg.Content, discordMaxChunk)
	for _, chunk := range chunks {
		if err := d.postChannelMessage(ctx, msg.RecipientID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (d *DiscordAdapter) postChannelMessage(ctx context.Context, channelID, content string) error {
	url := fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, channelID)
	payload := map[string]string{"content": content}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+d.cfg.Token)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord send %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ProcessWebhook handles a Discord interaction or MESSAGE_CREATE event.
func (d *DiscordAdapter) ProcessWebhook(data []byte) (*InboundMessage, error) {
	var event struct {
		Type    string `json:"t"`
		Data    json.RawMessage `json:"d"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("discord webhook decode: %w", err)
	}

	var msg struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Content   string `json:"content"`
		Author    struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(event.Data, &msg); err != nil {
		return nil, fmt.Errorf("discord message decode: %w", err)
	}

	// Skip bot messages.
	if msg.Author.Bot {
		return nil, nil
	}

	if !d.isGuildAllowed(msg.GuildID) {
		log.Debug().Str("guild_id", msg.GuildID).Msg("discord: guild not in allowlist")
		return nil, nil
	}

	ts, _ := time.Parse(time.RFC3339, msg.Timestamp)

	return &InboundMessage{
		ID:        msg.ID,
		Platform:  "discord",
		SenderID:  msg.Author.ID,
		ChatID:    msg.ChannelID,
		Content:   msg.Content,
		Timestamp: ts,
		Metadata:  map[string]any{"guild_id": msg.GuildID},
	}, nil
}

func (d *DiscordAdapter) isGuildAllowed(guildID string) bool {
	if len(d.cfg.AllowedGuildIDs) == 0 {
		return !d.cfg.DenyOnEmpty
	}
	for _, id := range d.cfg.AllowedGuildIDs {
		if id == guildID {
			return true
		}
	}
	return false
}
