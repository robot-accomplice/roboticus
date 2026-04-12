package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog/log"

	"roboticus/internal/mcp"
)

// McpBridgeTool wraps an MCP server's tool as a local Tool interface
// so it appears in the agent's ToolRegistry alongside builtins.
type McpBridgeTool struct {
	name        string
	description string
	schema      json.RawMessage
	risk        RiskLevel
	serverName  string
	manager     *mcp.ConnectionManager
}

// Name returns the namespaced tool name (mcp__server__toolname).
func (t *McpBridgeTool) Name() string { return t.name }

// Description returns the tool's description from the MCP server.
func (t *McpBridgeTool) Description() string { return t.description }

// Risk returns the risk level (default: RiskCaution for external tools).
func (t *McpBridgeTool) Risk() RiskLevel { return t.risk }

// ParameterSchema returns the JSON schema for the tool's input parameters.
func (t *McpBridgeTool) ParameterSchema() json.RawMessage { return t.schema }

// Execute delegates the tool call to the MCP connection manager.
func (t *McpBridgeTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	result, err := t.manager.CallTool(ctx, t.serverName, t.name, []byte(params))
	if err != nil {
		return nil, fmt.Errorf("mcp tool %s: %w", t.name, err)
	}

	if result.IsError {
		return &Result{
			Output: result.Content,
			Source: "mcp:" + t.serverName,
		}, fmt.Errorf("mcp tool %s returned error: %s", t.name, result.Content)
	}

	return &Result{
		Output: result.Content,
		Source: "mcp:" + t.serverName,
	}, nil
}

// RegisterMCPTools iterates all connected MCP servers and registers each
// discovered tool as an McpBridgeTool in the agent's tool registry.
// Returns the number of tools registered.
func RegisterMCPTools(registry *Registry, manager *mcp.ConnectionManager) int {
	if manager == nil {
		return 0
	}

	registered := 0
	for _, status := range manager.Statuses() {
		if !status.Connected {
			continue
		}

		conn, ok := manager.Connection(status.Name)
		if !ok {
			continue
		}

		for _, td := range conn.Tools {
			bridge := &McpBridgeTool{
				name:        td.Name,
				description: td.Description,
				schema:      td.InputSchema,
				risk:        RiskCaution,
				serverName:  status.Name,
				manager:     manager,
			}
			registry.Register(bridge)
			registered++

			log.Debug().
				Str("tool", td.Name).
				Str("server", status.Name).
				Msg("registered MCP tool in agent registry")
		}
	}

	return registered
}
