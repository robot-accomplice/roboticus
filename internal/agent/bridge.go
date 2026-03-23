package agent

import (
	"goboticus/internal/agent/tools"
)

// ToolRegistry is an alias for tools.Registry, used throughout the agent package.
type ToolRegistry = tools.Registry

// ToolContext is an alias for tools.Context.
type ToolContext = tools.Context

// ToolResult is an alias for tools.Result.
type ToolResult = tools.Result

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return tools.NewRegistry()
}
