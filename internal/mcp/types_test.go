package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMcpServerConfig_JSON(t *testing.T) {
	cfg := McpServerConfig{
		Name:      "test-server",
		Transport: "stdio",
		Command:   "python",
		Args:      []string{"-m", "mcp_server"},
		Enabled:   true,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed McpServerConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Name != "test-server" {
		t.Errorf("name = %s", parsed.Name)
	}
	if parsed.Transport != "stdio" {
		t.Errorf("transport = %s", parsed.Transport)
	}
}

func TestToolDescriptor_Roundtrip(t *testing.T) {
	td := ToolDescriptor{
		Name:        "read_file",
		Description: "Read a file from disk",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}
	data, _ := json.Marshal(td)
	var parsed ToolDescriptor
	_ = json.Unmarshal(data, &parsed)
	if parsed.Name != "read_file" {
		t.Errorf("name = %s", parsed.Name)
	}
}

func TestServerStatus_Fields(t *testing.T) {
	s := ServerStatus{
		Name:          "test",
		Connected:     true,
		ToolCount:     5,
		ServerName:    "test-srv",
		ServerVersion: "1.0",
	}
	data, _ := json.Marshal(s)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	if parsed["connected"] != true {
		t.Error("should be connected")
	}
	if parsed["tool_count"].(float64) != 5 {
		t.Errorf("tool_count = %v", parsed["tool_count"])
	}
}

func TestToolCallResult_Fields(t *testing.T) {
	r := ToolCallResult{Content: "hello", IsError: false}
	if r.IsError {
		t.Error("should not be error")
	}
	if r.Content != "hello" {
		t.Errorf("content = %s", r.Content)
	}
}

func TestConnectionManager_CallTool_NoServer(t *testing.T) {
	mgr := NewConnectionManager()
	_, err := mgr.CallTool(context.TODO(), "nonexistent", "tool", nil)
	if err == nil {
		t.Error("should error for missing server")
	}
}
