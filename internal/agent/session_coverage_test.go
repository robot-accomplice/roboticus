package agent

import "testing"

func TestSession_Lifecycle(t *testing.T) {
	s := NewSession("s1", "a1", "TestBot")
	if s.ID != "s1" {
		t.Errorf("id = %s", s.ID)
	}

	s.AddUserMessage("hello")
	s.AddAssistantMessage("hi there", nil)
	s.AddSystemMessage("you are helpful")

	if s.TurnCount() < 1 {
		t.Error("turn count should be >= 1 after messages")
	}

	msgs := s.Messages()
	if len(msgs) < 3 {
		t.Errorf("messages = %d, want >= 3", len(msgs))
	}

	last := s.LastAssistantContent()
	if last != "hi there" {
		t.Errorf("last assistant = %q", last)
	}
}

func TestSession_Channel(t *testing.T) {
	s := NewSession("s1", "a1", "bot")
	s.Channel = "telegram"
	if s.Channel != "telegram" {
		t.Errorf("channel = %s", s.Channel)
	}
}

func TestSession_EmptyLastAssistant(t *testing.T) {
	s := NewSession("s1", "a1", "bot")
	last := s.LastAssistantContent()
	if last != "" {
		t.Errorf("empty session should have empty last assistant, got %q", last)
	}
}
