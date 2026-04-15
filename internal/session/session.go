// Package session provides the unified conversation session type shared by
// pipeline and agent layers. It is a leaf package with no internal imports
// beyond core and llm, enabling both layers to use it without circular deps.
package session

import (
	"roboticus/internal/core"
	"roboticus/internal/llm"
)

// Session holds the state of an ongoing conversation.
type Session struct {
	ID            string
	AgentID       string
	AgentName     string
	Authority     core.AuthorityLevel
	SecurityClaim *core.SecurityClaim // Full claim for audit — set by pipeline stage 8.
	Workspace     string
	AllowedPaths  []string
	Channel       string
	ScopeKey      string // "platform:chatid" — used for cross-channel consent

	messages          []llm.Message
	pendingCalls      []llm.ToolCall
	memoryContext     string // Pre-retrieved memory block for cognitive scaffold (ARCHITECTURE.md §4).
	memoryIndex       string // Pre-built memory index block for recall/search tool guidance.
	taskIntent        string
	taskComplexity    string
	taskPlannedAction string
	taskSubgoals      []string

	// Perception artifact (Milestone 2): unified decision record produced
	// after task synthesis. Later stages read these fields instead of
	// re-classifying intent / risk / source-of-truth independently.
	taskRisk          string
	taskSourceOfTruth string
	taskRequiredTiers []string
	taskFreshness     bool
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

// SetMemoryContext stores pre-retrieved memory for cognitive scaffold injection.
// Called by the pipeline before delegation/skill-first so early-exit paths
// still have full cognitive context (ARCHITECTURE.md §4).
func (s *Session) SetMemoryContext(block string) { s.memoryContext = block }

// MemoryContext returns the pre-retrieved memory block, if any.
func (s *Session) MemoryContext() string { return s.memoryContext }

// SetMemoryIndex stores the pre-built memory index for prompt injection.
func (s *Session) SetMemoryIndex(block string) { s.memoryIndex = block }

// MemoryIndex returns the pre-built memory index block, if any.
func (s *Session) MemoryIndex() string { return s.memoryIndex }

// SetTaskVerificationHints stores pipeline-computed task state so later stages
// can verify responses against structured intent/subgoals instead of re-deriving
// everything from raw prompt text.
func (s *Session) SetTaskVerificationHints(intent, complexity, plannedAction string, subgoals []string) {
	s.taskIntent = intent
	s.taskComplexity = complexity
	s.taskPlannedAction = plannedAction
	s.taskSubgoals = append([]string(nil), subgoals...)
}

// TaskIntent returns the pipeline-computed intent label, if any.
func (s *Session) TaskIntent() string { return s.taskIntent }

// TaskComplexity returns the pipeline-computed complexity label, if any.
func (s *Session) TaskComplexity() string { return s.taskComplexity }

// TaskPlannedAction returns the pipeline-computed planned action, if any.
func (s *Session) TaskPlannedAction() string { return s.taskPlannedAction }

// TaskSubgoals returns verifier-oriented subgoals computed by the pipeline.
func (s *Session) TaskSubgoals() []string { return append([]string(nil), s.taskSubgoals...) }

// SetPerception stores the pipeline-computed perception artifact so later
// stages can consume risk, source-of-truth, required tiers, and freshness
// without re-classifying.
func (s *Session) SetPerception(risk, sourceOfTruth string, requiredTiers []string, freshness bool) {
	s.taskRisk = risk
	s.taskSourceOfTruth = sourceOfTruth
	s.taskRequiredTiers = append([]string(nil), requiredTiers...)
	s.taskFreshness = freshness
}

// TaskRisk returns the perception risk label (low / medium / high).
func (s *Session) TaskRisk() string { return s.taskRisk }

// TaskSourceOfTruth returns the perception source-of-truth label.
func (s *Session) TaskSourceOfTruth() string { return s.taskSourceOfTruth }

// TaskRequiredTiers returns the memory tiers retrieval must consult.
func (s *Session) TaskRequiredTiers() []string {
	return append([]string(nil), s.taskRequiredTiers...)
}

// TaskFreshness returns whether the answer depends on current state.
func (s *Session) TaskFreshness() bool { return s.taskFreshness }

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
