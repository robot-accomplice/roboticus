package pipeline

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/core"
)

func TestBotCommandHandler_Match(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)

	tests := []struct {
		name      string
		input     string
		wantMatch bool
		wantIn    string // substring expected in response (lowercased)
	}{
		{"help command", "/help", true, "commands"},
		{"status command", "/status", true, "online"},
		{"tools command", "/tools", true, "tools"},
		{"skills command", "/skills", true, "skills"},
		{"model command", "/model", true, ""},     // llmSvc nil — just check it matches
		{"models command", "/models", true, ""},   // llmSvc nil — just check it matches
		{"breaker command", "/breaker", true, ""}, // llmSvc nil — just check it matches
		{"retry command", "/retry", true, "no previous"},
		{"whoami command", "/whoami", true, "session"},
		{"clear command", "/clear", true, "clear"},
		{"unknown command", "/nonexistent", false, ""},
		{"not a command", "hello world", false, ""},
		{"command with args", "/memory search weather", true, "memory"},
		{"empty input", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := NewSession("s1", "agent1", "TestBot")
			result, matched := handler.TryHandle(context.Background(), tt.input, session)

			if matched != tt.wantMatch {
				t.Errorf("matched = %v, want %v", matched, tt.wantMatch)
			}
			if matched && result == nil {
				t.Fatal("matched but result is nil")
			}
			if matched && tt.wantIn != "" {
				lower := strings.ToLower(result.Content)
				if !strings.Contains(lower, tt.wantIn) {
					t.Errorf("content %q does not contain %q", result.Content, tt.wantIn)
				}
			}
		})
	}
}

func TestBotCommandHandler_RegisterCustom(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	handler.Register("ping", func(_ context.Context, _ string, s *Session) (*Outcome, error) {
		return &Outcome{SessionID: s.ID, Content: "pong"}, nil
	})

	session := NewSession("s1", "agent1", "TestBot")
	result, matched := handler.TryHandle(context.Background(), "/ping", session)

	if !matched {
		t.Fatal("expected /ping to match")
	}
	if result.Content != "pong" {
		t.Errorf("content = %q, want %q", result.Content, "pong")
	}
}

// ── @bot_name stripping (Rust parity) ────────────────────────────────────────

func TestBotCommand_BotNameStripping(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")

	tests := []struct {
		input     string
		wantMatch bool
	}{
		{"/help@DuncanBot", true},
		{"/status@SomeBot", true},
		{"/model@bot reset", true},
		{"/nonexistent@bot", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, matched := handler.TryHandle(context.Background(), tt.input, session)
			if matched != tt.wantMatch {
				t.Errorf("matched = %v, want %v for %q", matched, tt.wantMatch, tt.input)
			}
		})
	}
}

// ── Authority gating (Rust parity) ───────────────────────────────────────────

func TestBotCommand_ModelSetRequiresCreator(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")
	session.Authority = core.AuthorityPeer // Below Creator

	result, matched := handler.TryHandle(context.Background(), "/model openai/gpt-4o", session)
	if !matched {
		t.Fatal("/model should match")
	}
	if !strings.Contains(result.Content, "creator authority") {
		t.Errorf("should deny Peer authority for /model set, got: %s", result.Content)
	}
}

func TestBotCommand_ModelSetAllowedForCreator(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")
	session.Authority = core.AuthorityCreator

	result, matched := handler.TryHandle(context.Background(), "/model openai/gpt-4o", session)
	if !matched {
		t.Fatal("/model should match")
	}
	if strings.Contains(result.Content, "authority") {
		t.Errorf("Creator should be allowed, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "openai/gpt-4o") {
		t.Errorf("should confirm override, got: %s", result.Content)
	}
}

func TestBotCommand_ModelResetRequiresCreator(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")
	session.Authority = core.AuthorityExternal

	result, matched := handler.TryHandle(context.Background(), "/model reset", session)
	if !matched {
		t.Fatal("/model should match")
	}
	if !strings.Contains(result.Content, "creator authority") {
		t.Errorf("should deny External authority, got: %s", result.Content)
	}
}

func TestBotCommand_BreakerResetRequiresCreator(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")
	session.Authority = core.AuthorityPeer

	result, matched := handler.TryHandle(context.Background(), "/breaker reset", session)
	if !matched {
		t.Fatal("/breaker should match")
	}
	if !strings.Contains(result.Content, "creator authority") {
		t.Errorf("should deny Peer authority for breaker reset, got: %s", result.Content)
	}
}

// ── /retry ───────────────────────────────────────────────────────────────────

func TestBotCommand_RetryWithNoHistory(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")

	result, matched := handler.TryHandle(context.Background(), "/retry", session)
	if !matched {
		t.Fatal("/retry should match")
	}
	if !strings.Contains(result.Content, "No previous response") {
		t.Errorf("should indicate no history, got: %s", result.Content)
	}
}

func TestBotCommand_RetryReplaysLastAssistant(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")
	session.AddAssistantMessage("Here is the answer you asked for.", nil)

	result, matched := handler.TryHandle(context.Background(), "/retry", session)
	if !matched {
		t.Fatal("/retry should match")
	}
	if result.Content != "Here is the answer you asked for." {
		t.Errorf("should replay last assistant message, got: %s", result.Content)
	}
}

// ── /help lists all commands ─────────────────────────────────────────────────

func TestBotCommand_HelpListsAllCommands(t *testing.T) {
	handler := NewBotCommandHandler(nil, nil)
	session := NewSession("s1", "agent1", "TestBot")

	result, matched := handler.TryHandle(context.Background(), "/help", session)
	if !matched {
		t.Fatal("/help should match")
	}

	required := []string{"/help", "/status", "/model", "/models", "/breaker", "/retry",
		"/memory", "/tools", "/skills", "/whoami", "/clear"}
	for _, cmd := range required {
		if !strings.Contains(result.Content, cmd) {
			t.Errorf("/help output missing %q", cmd)
		}
	}
}
