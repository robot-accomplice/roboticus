package channel

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewDiscordAdapter_Defaults(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{Token: "bot-tok"})
	if a.PlatformName() != "discord" {
		t.Fatalf("expected discord, got %s", a.PlatformName())
	}
	if !a.cfg.DenyOnEmpty {
		t.Fatal("expected DenyOnEmpty=true when AllowedGuildIDs is empty")
	}
}

func TestNewDiscordAdapter_WithGuilds(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{
		Token:           "tok",
		AllowedGuildIDs: []string{"guild1", "guild2"},
	})
	if a.cfg.DenyOnEmpty {
		t.Fatal("DenyOnEmpty should be false when AllowedGuildIDs provided")
	}
}

func TestDiscordPushAndRecv(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{Token: "tok"})
	ctx := context.Background()

	// Empty buffer returns nil.
	msg, err := a.Recv(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != nil {
		t.Fatal("expected nil from empty buffer")
	}

	// Push a message and recv it.
	a.PushMessage(InboundMessage{
		ID:       "msg1",
		Platform: "discord",
		SenderID: "user1",
		ChatID:   "chan1",
		Content:  "hello discord",
	})

	msg, err = a.Recv(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message")
	}
	if msg.Content != "hello discord" {
		t.Fatalf("expected 'hello discord', got %q", msg.Content)
	}

	// Buffer should be empty again.
	msg, err = a.Recv(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != nil {
		t.Fatal("expected nil after draining buffer")
	}
}

func TestDiscordProcessWebhook(t *testing.T) {
	tests := []struct {
		name    string
		guilds  []string
		payload map[string]any
		wantNil bool
		wantID  string
	}{
		{
			name:   "valid message from allowed guild",
			guilds: []string{"g1"},
			payload: map[string]any{
				"t": "MESSAGE_CREATE",
				"d": map[string]any{
					"id": "msg123", "channel_id": "ch1", "guild_id": "g1",
					"content": "hi", "author": map[string]any{"id": "u1", "bot": false},
					"timestamp": "2024-01-15T10:30:00+00:00",
				},
			},
			wantID: "msg123",
		},
		{
			name:   "bot messages are skipped",
			guilds: []string{"g1"},
			payload: map[string]any{
				"t": "MESSAGE_CREATE",
				"d": map[string]any{
					"id": "msg124", "channel_id": "ch1", "guild_id": "g1",
					"content": "bot msg", "author": map[string]any{"id": "bot1", "bot": true},
					"timestamp": "2024-01-15T10:30:00+00:00",
				},
			},
			wantNil: true,
		},
		{
			name:   "guild not in allowlist",
			guilds: []string{"g1"},
			payload: map[string]any{
				"t": "MESSAGE_CREATE",
				"d": map[string]any{
					"id": "msg125", "channel_id": "ch1", "guild_id": "g99",
					"content": "hi", "author": map[string]any{"id": "u1", "bot": false},
					"timestamp": "2024-01-15T10:30:00+00:00",
				},
			},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewDiscordAdapter(DiscordConfig{
				Token:           "tok",
				AllowedGuildIDs: tt.guilds,
			})
			data, _ := json.Marshal(tt.payload)
			msg, err := a.ProcessWebhook(data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if msg != nil {
					t.Fatalf("expected nil, got %+v", msg)
				}
				return
			}
			if msg == nil {
				t.Fatal("expected message, got nil")
			}
			if msg.ID != tt.wantID {
				t.Fatalf("expected ID %q, got %q", tt.wantID, msg.ID)
			}
			if msg.Platform != "discord" {
				t.Fatalf("expected platform discord, got %s", msg.Platform)
			}
		})
	}
}

func TestDiscordProcessWebhook_InvalidJSON(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{Token: "tok"})
	_, err := a.ProcessWebhook([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDiscordConnectGateway_DisabledNoop(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{Token: "tok", GatewayEnabled: false})
	err := a.ConnectGateway(context.Background())
	if err != nil {
		t.Fatalf("expected nil for disabled gateway, got %v", err)
	}
}
