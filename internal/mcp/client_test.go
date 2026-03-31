package mcp

import (
	"encoding/json"
	"testing"
)

func TestJsonRPCRequest_Marshal(t *testing.T) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]any{"protocolVersion": "2024-11-05"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", parsed["jsonrpc"])
	}
	if parsed["method"] != "initialize" {
		t.Errorf("method = %v", parsed["method"])
	}
}

func TestJsonRPCResponse_Unmarshal(t *testing.T) {
	data := `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"test","version":"0.1.0"}}}`
	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("id = %d", resp.ID)
	}
	if resp.Error != nil {
		t.Error("should not have error")
	}
	if resp.Result == nil {
		t.Error("result should not be nil")
	}
}

func TestJsonRPCResponse_Error(t *testing.T) {
	data := `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`
	var resp jsonRPCResponse
	json.Unmarshal([]byte(data), &resp)
	if resp.Error == nil {
		t.Fatal("should have error")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("code = %d", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid Request" {
		t.Errorf("message = %s", resp.Error.Message)
	}
}

func TestToolDescriptor_JSON(t *testing.T) {
	td := ToolDescriptor{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}
	data, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed ToolDescriptor
	json.Unmarshal(data, &parsed)
	if parsed.Name != "read_file" {
		t.Errorf("name = %s", parsed.Name)
	}
}

func TestServerStatus_JSON(t *testing.T) {
	s := ServerStatus{
		Name:          "test-server",
		Connected:     true,
		ToolCount:     5,
		ServerName:    "test",
		ServerVersion: "1.0",
	}
	data, _ := json.Marshal(s)
	var parsed ServerStatus
	json.Unmarshal(data, &parsed)
	if !parsed.Connected {
		t.Error("should be connected")
	}
	if parsed.ToolCount != 5 {
		t.Errorf("tool_count = %d", parsed.ToolCount)
	}
}
