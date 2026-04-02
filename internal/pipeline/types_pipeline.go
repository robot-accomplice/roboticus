package pipeline

import (
	"goboticus/internal/core"
	"goboticus/internal/llm"
)

// Session holds the in-flight conversation state for the pipeline.
// This is the pipeline's own type — agent.Session adapts to it at the
// daemon wiring boundary.
type Session struct {
	ID           string
	AgentID      string
	AgentName    string
	Authority    core.AuthorityLevel
	Channel      string
	Workspace    string
	AllowedPaths []string

	messages     []llm.Message
	pendingCalls []llm.ToolCall
}

// NewSession creates a pipeline session with the given identity.
func NewSession(id, agentID, agentName string) *Session {
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

// TurnCount returns the number of user messages.
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

// LoopConfig controls the ReAct loop behavior.
// Defined here so pipeline can pass it without importing agent.
type LoopConfig struct {
	MaxTurns      int
	IdleThreshold int
	LoopWindow    int
}

// DefaultLoopConfig returns sensible defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{MaxTurns: 25, IdleThreshold: 3, LoopWindow: 3}
}

// ToolDef describes a tool for token budgeting in tool pruning.
type ToolDef struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	ParametersJSON string         `json:"parameters_json"`
	RiskLevel      core.RiskLevel `json:"risk_level"`
}

// EstimateTokens returns a rough token count for this tool definition.
func (td ToolDef) EstimateTokens() int {
	// ~4 chars per token heuristic
	return (len(td.Name) + len(td.Description) + len(td.ParametersJSON)) / 4
}

// MemoryFragment represents a single retrieved memory chunk.
type MemoryFragment struct {
	Tier      core.MemoryTier
	Content   string
	Score     float64
	Timestamp int64
}
