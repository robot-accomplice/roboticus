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
	_ = json.Unmarshal([]byte(params), &p)
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

// --- MemoryStatsTool ---

// MemoryStatsTool returns counts from each memory tier table.
type MemoryStatsTool struct{}

func (t *MemoryStatsTool) Name() string        { return "get_memory_stats" }
func (t *MemoryStatsTool) Description() string { return "Get row counts for each memory tier." }
func (t *MemoryStatsTool) Risk() RiskLevel     { return RiskSafe }
func (t *MemoryStatsTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"session_id": {"type": "string", "description": "Optional session ID to scope working memory counts"}
		}
	}`)
}

func (t *MemoryStatsTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal([]byte(params), &args)

	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}

	type tierCount struct {
		Name  string `json:"tier"`
		Count int    `json:"count"`
	}

	tiers := []struct {
		name  string
		query string
	}{
		{"working_memory", "SELECT COUNT(*) FROM working_memory"},
		{"episodic_memory", "SELECT COUNT(*) FROM episodic_memory"},
		{"semantic_memory", "SELECT COUNT(*) FROM semantic_memory"},
		{"procedural_memory", "SELECT COUNT(*) FROM procedural_memory"},
		{"relationship_memory", "SELECT COUNT(*) FROM relationship_memory"},
	}

	// If a session_id is provided, scope working_memory to that session.
	sessionID := args.SessionID
	if sessionID == "" {
		sessionID = tctx.SessionID
	}

	var results []tierCount
	for _, tier := range tiers {
		var count int
		query := tier.query
		var scanArgs []any

		if tier.name == "working_memory" && sessionID != "" {
			query = "SELECT COUNT(*) FROM working_memory WHERE session_id = ?"
			scanArgs = append(scanArgs, sessionID)
		}

		var err error
		if len(scanArgs) > 0 {
			err = tctx.Store.QueryRowContext(ctx, query, scanArgs...).Scan(&count)
		} else {
			err = tctx.Store.QueryRowContext(ctx, query).Scan(&count)
		}
		if err != nil {
			// Table may not exist yet; treat as zero.
			count = 0
		}
		results = append(results, tierCount{Name: tier.name, Count: count})
	}

	data, err := json.Marshal(results)
	if err != nil {
		return nil, fmt.Errorf("marshal stats: %w", err)
	}
	return &Result{Output: string(data)}, nil
}

// --- ChannelHealthTool ---

// ChannelHealthTool returns health status for all registered channel adapters.
// Matches Rust's GetChannelHealthTool.
type ChannelHealthTool struct{}

func (t *ChannelHealthTool) Name() string { return "get_channel_health" }
func (t *ChannelHealthTool) Description() string {
	return "Get health status of all registered channel adapters (Telegram, Discord, Signal, Email, etc.)."
}
func (t *ChannelHealthTool) Risk() RiskLevel { return RiskSafe }
func (t *ChannelHealthTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ChannelHealthTool) Execute(ctx context.Context, _ string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return &Result{Output: "no channel data available"}, nil
	}

	// Query channel status from the channels status table if it exists,
	// or return basic counts from sessions scoped to each platform.
	rows, err := tctx.Store.QueryContext(ctx,
		`SELECT
			CASE
				WHEN scope_key LIKE 'telegram:%' OR scope_key LIKE 'peer:telegram:%' THEN 'telegram'
				WHEN scope_key LIKE 'discord:%' THEN 'discord'
				WHEN scope_key LIKE 'signal:%' OR scope_key LIKE 'peer:signal:%' THEN 'signal'
				WHEN scope_key LIKE 'email:%' THEN 'email'
				WHEN scope_key LIKE 'api:%' THEN 'api'
				ELSE 'other'
			END as channel,
			COUNT(*) as sessions,
			MAX(updated_at) as last_activity
		FROM sessions
		WHERE status = 'active'
		GROUP BY channel
		ORDER BY sessions DESC`)
	if err != nil {
		return &Result{Output: "failed to query channel health"}, nil
	}
	defer func() { _ = rows.Close() }()

	type channelStat struct {
		Channel      string `json:"channel"`
		Sessions     int    `json:"active_sessions"`
		LastActivity string `json:"last_activity"`
	}
	var stats []channelStat
	for rows.Next() {
		var s channelStat
		if rows.Scan(&s.Channel, &s.Sessions, &s.LastActivity) == nil {
			stats = append(stats, s)
		}
	}
	data, _ := json.Marshal(stats)
	return &Result{Output: string(data)}, nil
}

// --- SubagentStatusTool ---

// SubagentStatusTool returns the status of all registered subagents.
// Matches Rust's GetSubagentStatusTool.
type SubagentStatusTool struct{}

func (t *SubagentStatusTool) Name() string { return "get_subagent_status" }
func (t *SubagentStatusTool) Description() string {
	return "Get status of all registered subagents including their model, role, and activity."
}
func (t *SubagentStatusTool) Risk() RiskLevel { return RiskSafe }
func (t *SubagentStatusTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *SubagentStatusTool) Execute(ctx context.Context, _ string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return &Result{Output: "no subagent data available"}, nil
	}

	rows, err := tctx.Store.QueryContext(ctx,
		`SELECT name, COALESCE(display_name, name), model, role, enabled,
			session_count, COALESCE(last_used_at, 'never')
		 FROM sub_agents ORDER BY name`)
	if err != nil {
		return &Result{Output: "failed to query subagents"}, nil
	}
	defer func() { _ = rows.Close() }()

	type agentStatus struct {
		Name         string `json:"name"`
		DisplayName  string `json:"display_name"`
		Model        string `json:"model"`
		Role         string `json:"role"`
		Enabled      bool   `json:"enabled"`
		SessionCount int    `json:"session_count"`
		LastUsed     string `json:"last_used"`
	}
	var agents []agentStatus
	for rows.Next() {
		var a agentStatus
		if rows.Scan(&a.Name, &a.DisplayName, &a.Model, &a.Role, &a.Enabled, &a.SessionCount, &a.LastUsed) == nil {
			agents = append(agents, a)
		}
	}
	data, _ := json.Marshal(agents)
	return &Result{Output: string(data)}, nil
}
