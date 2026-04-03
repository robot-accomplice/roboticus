// Package session provides the unified conversation session type shared by
// pipeline and agent layers. It is a leaf package with no internal imports
// beyond core and llm, enabling both layers to use it without circular deps.
package session

import (
	"goboticus/internal/core"
	"goboticus/internal/llm"
)

// Session holds the state of an ongoing conversation.
type Session struct {
	ID           string
	AgentID      string
	AgentName    string
	Authority    core.AuthorityLevel
	Workspace    string
	AllowedPaths []string
	Channel      string

	messages     []llm.Message
	pendingCalls []llm.ToolCall
}

// New creates a session with the given identity.
func New(id, agentID, agentName string) *Session {
	return &Session{
		ID:        id,
		AgentID:   agentID,
		AgentName: agentName,
		Authority: core.AuthorityExternal,
	}
}

// Messages returns the full message history.
func (s *Session) Messages() []llm.Message { return s.messages }

// AddUserMessage appends a user message.
func (s *Session) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}

// AddSystemMessage appends a system message.
func (s *Session) AddSystemMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "system", Content: content})
}

// AddAssistantMessage appends an assistant message with optional tool calls.
func (s *Session) AddAssistantMessage(content string, toolCalls []llm.ToolCall) {
	s.messages = append(s.messages, llm.Message{
		Role: "assistant", Content: content, ToolCalls: toolCalls,
	})
	s.pendingCalls = toolCalls
}

// AddToolResult appends a tool result message.
func (s *Session) AddToolResult(callID, toolName, output string, isError bool) {
	content := output
	if isError {
		content = "Error: " + output
	}
	s.messages = append(s.messages, llm.Message{
		Role: "tool", Content: content, ToolCallID: callID, Name: toolName,
	})
	remaining := s.pendingCalls[:0]
	for _, tc := range s.pendingCalls {
		if tc.ID != callID {
			remaining = append(remaining, tc)
		}
	}
	s.pendingCalls = remaining
}

// PendingToolCalls returns tool calls not yet resolved.
func (s *Session) PendingToolCalls() []llm.ToolCall { return s.pendingCalls }

// LastAssistantContent returns the most recent assistant message content.
func (s *Session) LastAssistantContent() string {
	for i := len(s.messages) - 1; i >= 0; i-- {
		if s.messages[i].Role == "assistant" {
			return s.messages[i].Content
		}
	}
	return ""
}

// TurnCount returns the number of user messages (conversation turns).
func (s *Session) TurnCount() int {
	count := 0
	for _, m := range s.messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// MessageCount returns total messages in history.
func (s *Session) MessageCount() int { return len(s.messages) }
