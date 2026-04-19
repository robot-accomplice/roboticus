package tools

import (
	"encoding/json"
	"testing"

	"roboticus/internal/mcp"
)

// TestMcpBridgeTool_ImplementsTool verifies the Tool interface is satisfied.
func TestMcpBridgeTool_ImplementsTool(t *testing.T) {
	var _ Tool = (*McpBridgeTool)(nil)
}

// TestMcpBridgeTool_Accessors verifies Name, Description, Risk, ParameterSchema.
func TestMcpBridgeTool_Accessors(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)
	bt := &McpBridgeTool{
		name:        "search",
		description: "Search the web",
		schema:      schema,
		risk:        RiskCaution,
		serverName:  "brave",
	}

	if bt.Name() != "search" {
		t.Errorf("Name() = %q, want %q", bt.Name(), "search")
	}
	if bt.Description() != "Search the web" {
		t.Errorf("Description() = %q, want %q", bt.Description(), "Search the web")
	}
	if bt.Risk() != RiskCaution {
		t.Errorf("Risk() = %v, want %v", bt.Risk(), RiskCaution)
	}
	if string(bt.ParameterSchema()) != string(schema) {
		t.Errorf("ParameterSchema() mismatch")
	}
}

// TestRegisterMCPTools_EmptyManager verifies 0 tools with no connections.
func TestRegisterMCPTools_EmptyManager(t *testing.T) {
	registry := NewRegistry()
	manager := mcp.NewConnectionManager()

	count := RegisterMCPTools(registry, manager)
	if count != 0 {
		t.Errorf("RegisterMCPTools() = %d, want 0", count)
	}
	if len(registry.List()) != 0 {
		t.Errorf("registry.List() has %d tools, want 0", len(registry.List()))
	}
}

// TestRegisterMCPTools_NilManager verifies 0 tools with nil manager.
func TestRegisterMCPTools_NilManager(t *testing.T) {
	registry := NewRegistry()

	count := RegisterMCPTools(registry, nil)
	if count != 0 {
		t.Errorf("RegisterMCPTools(nil) = %d, want 0", count)
	}
}

func TestSyncMCPTools_RemovesDisconnectedServerTools(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&McpBridgeTool{
		name:        "echo",
		description: "echo",
		schema:      json.RawMessage(`{"type":"object"}`),
		risk:        RiskCaution,
		serverName:  "srv",
	})

	count := SyncMCPTools(registry, mcp.NewConnectionManager())
	if count != 0 {
		t.Fatalf("SyncMCPTools() = %d, want 0", count)
	}
	if got := registry.Get("echo"); got != nil {
		t.Fatalf("registry.Get(echo) = %T, want nil after sync pruning", got)
	}
}
