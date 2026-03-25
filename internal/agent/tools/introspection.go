package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// IntrospectionTool lets the agent inspect its own capabilities and state.
type IntrospectionTool struct {
	startTime time.Time
	agentName string
	version   string
	toolNames func() []string
}

// NewIntrospectionTool creates an introspection tool.
func NewIntrospectionTool(agentName, version string, toolNames func() []string) *IntrospectionTool {
	return &IntrospectionTool{
		startTime: time.Now(),
		agentName: agentName,
		version:   version,
		toolNames: toolNames,
	}
}

func (t *IntrospectionTool) Name() string { return "introspect" }
func (t *IntrospectionTool) Description() string {
	return "Inspect agent capabilities, available tools, and runtime state."
}
func (t *IntrospectionTool) Risk() RiskLevel { return RiskSafe }
func (t *IntrospectionTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"aspect": {
				"type": "string",
				"enum": ["capabilities", "tools", "runtime", "memory", "all"],
				"description": "Which aspect to introspect (default: all)"
			}
		}
	}`)
}

func (t *IntrospectionTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	var p struct {
		Aspect string `json:"aspect"`
	}
	json.Unmarshal([]byte(params), &p)
	if p.Aspect == "" {
		p.Aspect = "all"
	}

	var sections []string

	if p.Aspect == "capabilities" || p.Aspect == "all" {
		sections = append(sections, t.capabilities())
	}
	if p.Aspect == "tools" || p.Aspect == "all" {
		sections = append(sections, t.tools())
	}
	if p.Aspect == "runtime" || p.Aspect == "all" {
		sections = append(sections, t.runtimeInfo())
	}
	if p.Aspect == "memory" || p.Aspect == "all" {
		sections = append(sections, t.memoryInfo())
	}

	return &Result{Output: strings.Join(sections, "\n\n")}, nil
}

func (t *IntrospectionTool) capabilities() string {
	return fmt.Sprintf(`## Capabilities
- Agent: %s (v%s)
- Multi-model inference with cascade routing
- 5-tier memory system (working, episodic, semantic, procedural, relationship)
- Multi-channel delivery (Telegram, Discord, Signal, WhatsApp, Voice, A2A)
- Tool execution with sandboxed filesystem access
- Cron scheduling with durable execution
- On-chain wallet for autonomous payments
- WebSocket real-time event streaming`, t.agentName, t.version)
}

func (t *IntrospectionTool) tools() string {
	names := t.toolNames()
	var sb strings.Builder
	sb.WriteString("## Available Tools\n")
	for _, name := range names {
		fmt.Fprintf(&sb, "- %s\n", name)
	}
	return sb.String()
}

func (t *IntrospectionTool) runtimeInfo() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return fmt.Sprintf(`## Runtime
- Uptime: %s
- Go version: %s
- OS/Arch: %s/%s
- Goroutines: %d
- Heap alloc: %.1f MB
- Sys memory: %.1f MB
- GC cycles: %d`,
		time.Since(t.startTime).Round(time.Second),
		runtime.Version(),
		runtime.GOOS, runtime.GOARCH,
		runtime.NumGoroutine(),
		float64(m.HeapAlloc)/1024/1024,
		float64(m.Sys)/1024/1024,
		m.NumGC,
	)
}

func (t *IntrospectionTool) memoryInfo() string {
	return `## Memory Tiers
- Working: Active session context (goals, notes, summaries)
- Episodic: Past events with temporal decay re-ranking
- Semantic: Structured knowledge (category/key/value with confidence)
- Procedural: Tool usage statistics (success/failure rates)
- Relationship: Entity interaction tracking (trust scores, frequency)`
}
