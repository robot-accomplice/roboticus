package pipeline

import (
	"roboticus/internal/core"
	"roboticus/internal/session"
)

// Session is an alias for the shared session type.
type Session = session.Session

// NewSession creates a pipeline session with the given identity.
func NewSession(id, agentID, agentName string) *Session {
	return session.New(id, agentID, agentName)
}

// LoopConfig controls the ReAct loop behavior.
// Defined here so pipeline can pass it without importing agent.
type LoopConfig struct {
	MaxTurns      int
	IdleThreshold int
	LoopWindow    int
}

// DefaultLoopConfig returns sensible defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{MaxTurns: 10, IdleThreshold: 3, LoopWindow: 3} // Rust parity: 10 turns
}

// ToolDef describes a tool for token budgeting in tool pruning.
type ToolDef struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	ParametersJSON string         `json:"parameters_json"`
	RiskLevel      core.RiskLevel `json:"risk_level"`
	Embedding      []float64      `json:"-"` // Pre-computed embedding for relevance pruning (Wave 8, #84)
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
