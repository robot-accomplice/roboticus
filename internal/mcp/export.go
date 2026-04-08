package mcp

import (
	"encoding/json"

	"roboticus/internal/plugin"
)

// MCPToolDef is an alias for ToolDescriptor used in MCP protocol contexts.
// It uses the MCP-standard field names.
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ExportToolsAsMCP converts plugin ToolDef definitions to MCP-compatible
// MCPToolDef format. This enables exposing plugin tools through the MCP gateway.
func ExportToolsAsMCP(tools []plugin.ToolDef) []MCPToolDef {
	if len(tools) == 0 {
		return nil
	}

	result := make([]MCPToolDef, len(tools))
	for i, t := range tools {
		schema := t.Parameters
		if schema == nil {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		result[i] = MCPToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		}
	}
	return result
}
