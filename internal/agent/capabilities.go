package agent

import (
	"context"

	"github.com/rs/zerolog/log"

	"roboticus/internal/agent/policy"
	"roboticus/internal/agent/tools"
	"roboticus/internal/llm"
)

// ExecutionRegistry is the compositional seam for tool/runtime execution.
// Matches Rust's separation of concerns: tool catalog, policy enforcement,
// approval tracking, plugin/MCP execution, and browser automation.
//
// Each capability domain is an interface — the agent loop composes them
// without knowing the concrete implementations.
type ExecutionRegistry struct {
	Tools     *ToolRegistry
	Policy    *policy.Engine
	Approvals *ApprovalManager
	Plugins   PluginExecutor
	MCP       MCPExecutor
	Browser   BrowserExecutor
}

// PluginExecutor executes plugin-provided tools.
type PluginExecutor interface {
	Execute(ctx context.Context, pluginName, toolName, args string) (*tools.Result, error)
	Available() []string
}

// MCPExecutor executes MCP server-provided tools.
type MCPExecutor interface {
	Execute(ctx context.Context, serverName, toolName, args string) (*tools.Result, error)
	Available() []string
}

// BrowserExecutor provides browser automation capabilities.
type BrowserExecutor interface {
	Navigate(ctx context.Context, url string) error
	GetContent(ctx context.Context) (string, error)
	Available() bool
}

// ApprovalManager tracks tool approval requests and decisions.
// Matches Rust's approval flow: gated tools require explicit approval,
// blocked tools are forbidden, safe tools execute immediately.
type ApprovalManager struct {
	blockedTools []string
	gatedTools   []string
}

// ToolClassification categorizes a tool for approval purposes.
type ToolClassification int

const (
	ToolSafe    ToolClassification = iota // No approval needed
	ToolGated                             // Approval required before execution
	ToolBlocked                           // Execution forbidden
)

// NewApprovalManager creates an approval manager with blocked/gated tool lists.
func NewApprovalManager(blocked, gated []string) *ApprovalManager {
	return &ApprovalManager{
		blockedTools: blocked,
		gatedTools:   gated,
	}
}

// Classify determines the approval classification for a tool.
func (am *ApprovalManager) Classify(toolName string) ToolClassification {
	for _, t := range am.blockedTools {
		if t == toolName {
			return ToolBlocked
		}
	}
	for _, t := range am.gatedTools {
		if t == toolName {
			return ToolGated
		}
	}
	return ToolSafe
}

// ApprovalRequest tracks a pending tool approval.
type ApprovalRequest struct {
	ID          string
	ToolName    string
	ToolInput   string
	SessionID   string
	TurnID      string
	Authority   string
	Status      string // "pending", "approved", "denied", "timed_out"
	DecidedBy   string
	RequestedAt string
	ExpiresAt   string
}

// ToolSource identifies where a tool comes from (for flight recorder).
type ToolSource string

const (
	SourceBuiltin ToolSource = "builtin"
	SourcePlugin  ToolSource = "plugin"
	SourceMCP     ToolSource = "mcp"
)

// ResolveToolSource determines which execution path to use for a tool call.
// Matches Rust's execution separation: builtin registry → plugin registry → MCP servers.
func (cr *ExecutionRegistry) ResolveToolSource(toolName string) ToolSource {
	if cr.Tools != nil && cr.Tools.Get(toolName) != nil {
		return SourceBuiltin
	}
	if cr.Plugins != nil {
		for _, name := range cr.Plugins.Available() {
			if name == toolName {
				return SourcePlugin
			}
		}
	}
	if cr.MCP != nil {
		for _, name := range cr.MCP.Available() {
			if name == toolName {
				return SourceMCP
			}
		}
	}
	return SourceBuiltin // Default — will fail at execution if not found.
}

// ExecuteTool dispatches a tool call to the correct execution path based on source.
// This is the unified entry point matching Rust's execute_tool_detailed.
func (cr *ExecutionRegistry) ExecuteTool(ctx context.Context, toolName, args string, tctx *tools.Context) (*tools.Result, error) {
	// Check approval classification. ToolBlocked is enforced immediately.
	// ToolGated is NOT yet implemented — when it is, the approval flow
	// must be pipeline-owned (Rule 4.2/4.4), not connector-level.
	if cr.Approvals != nil {
		class := cr.Approvals.Classify(toolName)
		if class == ToolBlocked {
			return &tools.Result{Output: "Tool is blocked by policy", Source: string(SourceBuiltin)}, nil
		}
		if class == ToolGated {
			log.Warn().Str("tool", toolName).Msg("gated tool called but approval flow not yet implemented — executing without gate")
		}
	}

	source := cr.ResolveToolSource(toolName)
	switch source {
	case SourcePlugin:
		if cr.Plugins != nil {
			result, err := cr.Plugins.Execute(ctx, "", toolName, args)
			if err == nil {
				result.Source = string(SourcePlugin)
			}
			return result, err
		}
	case SourceMCP:
		if cr.MCP != nil {
			result, err := cr.MCP.Execute(ctx, "", toolName, args)
			if err == nil {
				result.Source = string(SourceMCP)
			}
			return result, err
		}
	}

	// Default: builtin tool registry.
	if cr.Tools != nil {
		tool := cr.Tools.Get(toolName)
		if tool == nil {
			return &tools.Result{Output: "unknown tool: " + toolName}, nil
		}
		result, err := tool.Execute(ctx, args, tctx)
		if err == nil && result != nil {
			result.Source = string(SourceBuiltin)
		}
		return result, err
	}
	return &tools.Result{Output: "no tool registry configured"}, nil
}

// Completer wraps an LLM service for the ReAct loop.
type Completer = llm.Completer
