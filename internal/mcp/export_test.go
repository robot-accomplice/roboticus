package mcp

import (
	"encoding/json"
	"testing"

	"roboticus/internal/plugin"
)

func TestExportToolsAsMCP_Empty(t *testing.T) {
	result := ExportToolsAsMCP(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestExportToolsAsMCP_Converts(t *testing.T) {
	tools := []plugin.ToolDef{
		{
			Name:        "greet",
			Description: "Says hello",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			RiskLevel:   "safe",
		},
		{
			Name:        "calc",
			Description: "Calculator",
		},
	}

	result := ExportToolsAsMCP(tools)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}

	if result[0].Name != "greet" || result[0].Description != "Says hello" {
		t.Fatalf("bad first tool: %+v", result[0])
	}
	if string(result[0].InputSchema) != `{"type":"object","properties":{"name":{"type":"string"}}}` {
		t.Fatalf("bad schema: %s", result[0].InputSchema)
	}

	// Nil parameters should get default schema.
	if string(result[1].InputSchema) != `{"type":"object"}` {
		t.Fatalf("expected default schema, got: %s", result[1].InputSchema)
	}
}
