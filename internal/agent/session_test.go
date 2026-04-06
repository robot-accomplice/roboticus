package agent

import (
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/llm"
)

func TestSession_Basic(t *testing.T) {
	s := NewSession("sess1", "agent1", "Roboticus")
	if s.ID != "sess1" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.AgentID != "agent1" {
		t.Errorf("AgentID = %q", s.AgentID)
	}
	if s.Authority != core.AuthorityExternal {
		t.Errorf("default authority = %v, want External", s.Authority)
	}
}

func TestSession_AddMessages(t *testing.T) {
	s := NewSession("s", "a", "G")

	s.AddUserMessage("Hello")
	s.AddSystemMessage("System prompt")
	s.AddAssistantMessage("Hi there!", nil)

	msgs := s.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
		t.Errorf("msg[0] = %+v", msgs[0])
	}
	if msgs[1].Role != "system" {
		t.Errorf("msg[1].Role = %q", msgs[1].Role)
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "Hi there!" {
		t.Errorf("msg[2] = %+v", msgs[2])
	}
}

func TestSession_ToolCallFlow(t *testing.T) {
	s := NewSession("s", "a", "G")

	// Simulate assistant response with tool calls.
	toolCalls := []llm.ToolCall{{
		ID:   "tc1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "read_file",
			Arguments: `{"path": "test.txt"}`,
		},
	}}
	s.AddAssistantMessage("", toolCalls)

	pending := s.PendingToolCalls()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending tool call, got %d", len(pending))
	}
	if pending[0].Function.Name != "read_file" {
		t.Errorf("pending[0].Name = %q", pending[0].Function.Name)
	}

	// Add tool result.
	s.AddToolResult("tc1", "read_file", "file contents here", false)

	// No more pending calls.
	pending = s.PendingToolCalls()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after result, got %d", len(pending))
	}
}

func TestSession_TurnCount(t *testing.T) {
	s := NewSession("s", "a", "G")
	if s.TurnCount() != 0 {
		t.Errorf("initial turn count = %d", s.TurnCount())
	}

	s.AddUserMessage("Q1")
	s.AddAssistantMessage("A1", nil)
	// TurnCount counts assistant messages.
	if s.TurnCount() != 1 {
		t.Errorf("after 1 exchange: turn count = %d, want 1", s.TurnCount())
	}
}

func TestSession_LastAssistantContent(t *testing.T) {
	s := NewSession("s", "a", "G")
	if got := s.LastAssistantContent(); got != "" {
		t.Errorf("empty session: got %q", got)
	}

	s.AddAssistantMessage("First response", nil)
	s.AddUserMessage("Follow-up")
	s.AddAssistantMessage("Second response", nil)

	if got := s.LastAssistantContent(); got != "Second response" {
		t.Errorf("last assistant = %q, want 'Second response'", got)
	}
}

func TestSession_MessageCount(t *testing.T) {
	s := NewSession("s", "a", "G")
	if s.MessageCount() != 0 {
		t.Errorf("initial count = %d", s.MessageCount())
	}
	s.AddUserMessage("msg")
	if s.MessageCount() != 1 {
		t.Errorf("after add: count = %d", s.MessageCount())
	}
}
