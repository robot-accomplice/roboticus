package pipeline

import (
	"context"
	"strings"
	"testing"
)

func TestBotCommandHandler_Match(t *testing.T) {
	handler := NewBotCommandHandler()

	tests := []struct {
		name      string
		input     string
		wantMatch bool
		wantIn    string // substring expected in response
	}{
		{"help command", "/help", true, "can help with"},
		{"status command", "/status", true, "online"},
		{"tools command", "/tools", true, "tools"},
		{"skills command", "/skills", true, "skills"},
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
	handler := NewBotCommandHandler()
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
