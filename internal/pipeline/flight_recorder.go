package pipeline

import (
	"encoding/json"
	"time"
)

const maxStepFieldLen = 500

// StepKind categorizes a ReactTrace step.
type StepKind int

const (
	StepToolCall   StepKind = iota // Tool execution
	StepLLMCall                    // LLM inference call
	StepGuardCheck                 // Guard evaluation
	StepRetry                      // Guard-triggered retry

	// Extended step kinds (Wave 8, #89).
	StepGuardPrecompute // Guard score pre-computation before chain
	StepCacheHit        // Cache hit — response served from cache
	StepDecomposition   // Decomposition gate evaluation
	StepSpeculation     // Speculative execution path
)

// ToolSource identifies where a tool came from.
type ToolSource struct {
	Kind   string `json:"kind"`   // "builtin", "mcp", "plugin", "skill"
	Server string `json:"server"` // MCP server or plugin name (empty for builtin)
}

// ReactStep records a single step in the ReAct loop.
type ReactStep struct {
	Kind       StepKind   `json:"kind"`
	Name       string     `json:"name"`
	DurationMs int64      `json:"duration_ms"`
	Success    bool       `json:"success"`
	Source     ToolSource `json:"source"`
	Input      string     `json:"input"`
	Output     string     `json:"output"`
}

// ReactTrace records all steps of a ReAct tool-calling loop.
type ReactTrace struct {
	Steps     []ReactStep `json:"steps"`
	StartedAt time.Time   `json:"started_at"`
	TotalMs   int64       `json:"total_ms"`
}

// NewReactTrace starts a new trace.
func NewReactTrace() *ReactTrace {
	return &ReactTrace{StartedAt: time.Now()}
}

// RecordStep adds a step, truncating Input/Output to maxStepFieldLen.
func (rt *ReactTrace) RecordStep(s ReactStep) {
	s.Input = truncate(s.Input, maxStepFieldLen)
	s.Output = truncate(s.Output, maxStepFieldLen)
	rt.Steps = append(rt.Steps, s)
}

// Finish marks the trace complete and records total duration.
func (rt *ReactTrace) Finish() {
	rt.TotalMs = time.Since(rt.StartedAt).Milliseconds()
}

// JSON returns the ReactTrace as a JSON string for DB persistence.
func (rt *ReactTrace) JSON() string {
	if rt == nil {
		return ""
	}
	b, _ := json.Marshal(rt)
	return string(b)
}

// truncate is defined in guards_truthfulness.go (shared utility).
