package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestConnectionManager_Statuses_Empty(t *testing.T) {
	mgr := NewConnectionManager()
	statuses := mgr.Statuses()
	if len(statuses) != 0 {
		t.Errorf("empty manager should have 0 statuses, got %d", len(statuses))
	}
}

func TestConnectionManager_AllTools_Empty(t *testing.T) {
	mgr := NewConnectionManager()
	tools := mgr.AllTools()
	if len(tools) != 0 {
		t.Errorf("empty manager should have 0 tools, got %d", len(tools))
	}
}

func TestConnectionManager_Disconnect_NotConnected(t *testing.T) {
	mgr := NewConnectionManager()
	err := mgr.Disconnect("nonexistent")
	if err == nil {
		t.Error("should return error for nonexistent server")
	}
}

func TestConnectionManager_CloseAll_Empty(t *testing.T) {
	mgr := NewConnectionManager()
	mgr.CloseAll() // should not panic
}

func TestConnectionManager_Connect_UnsupportedTransport(t *testing.T) {
	mgr := NewConnectionManager()
	err := mgr.Connect(context.TODO(), McpServerConfig{
		Name:      "test",
		Transport: "invalid",
	})
	if err == nil {
		t.Error("should return error for unsupported transport")
	}
}

func TestConnectionManager_Connect_SSE_MissingURL(t *testing.T) {
	mgr := NewConnectionManager()
	err := mgr.Connect(context.TODO(), McpServerConfig{
		Name:      "test-sse",
		Transport: "sse",
		URL:       "",
	})
	if err == nil {
		t.Fatal("should return error for SSE transport with missing URL")
	}
}

// injectConnection is a test helper that directly injects a Connection into the manager.
func injectConnection(mgr *ConnectionManager, conn *Connection) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	mgr.connections[conn.Name] = conn
}

func TestConnectionManager_Statuses_WithConnections(t *testing.T) {
	mgr := NewConnectionManager()

	conn1 := &Connection{
		Name:          "server-a",
		ServerName:    "Alpha",
		ServerVersion: "1.0",
		Tools: []ToolDescriptor{
			{Name: "tool1"},
			{Name: "tool2"},
		},
	}
	conn2 := &Connection{
		Name:          "server-b",
		ServerName:    "Beta",
		ServerVersion: "2.0",
		Tools: []ToolDescriptor{
			{Name: "tool3"},
		},
	}
	injectConnection(mgr, conn1)
	injectConnection(mgr, conn2)

	statuses := mgr.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("statuses count = %d, want 2", len(statuses))
	}

	// Build a map for easier assertions (order is non-deterministic).
	statusMap := make(map[string]ServerStatus)
	for _, s := range statuses {
		statusMap[s.Name] = s
	}

	sa := statusMap["server-a"]
	if !sa.Connected {
		t.Error("server-a should be connected")
	}
	if sa.ToolCount != 2 {
		t.Errorf("server-a tool_count = %d", sa.ToolCount)
	}
	if sa.ServerName != "Alpha" {
		t.Errorf("server-a server_name = %s", sa.ServerName)
	}

	sb := statusMap["server-b"]
	if sb.ToolCount != 1 {
		t.Errorf("server-b tool_count = %d", sb.ToolCount)
	}
}

func TestConnectionManager_Statuses_AreDeterministicByServerName(t *testing.T) {
	mgr := NewConnectionManager()

	injectConnection(mgr, &Connection{Name: "server-c"})
	injectConnection(mgr, &Connection{Name: "server-a"})
	injectConnection(mgr, &Connection{Name: "server-b"})

	statuses := mgr.Statuses()
	if len(statuses) != 3 {
		t.Fatalf("statuses count = %d, want 3", len(statuses))
	}
	got := []string{statuses[0].Name, statuses[1].Name, statuses[2].Name}
	want := []string{"server-a", "server-b", "server-c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statuses order = %v, want %v", got, want)
		}
	}
}

func TestConnectionManager_AllTools_WithConnections(t *testing.T) {
	mgr := NewConnectionManager()

	injectConnection(mgr, &Connection{
		Name: "s1",
		Tools: []ToolDescriptor{
			{Name: "t1", Description: "first"},
			{Name: "t2", Description: "second"},
		},
	})
	injectConnection(mgr, &Connection{
		Name: "s2",
		Tools: []ToolDescriptor{
			{Name: "t3", Description: "third"},
		},
	})

	tools := mgr.AllTools()
	if len(tools) != 3 {
		t.Fatalf("tool count = %d, want 3", len(tools))
	}

	nameSet := make(map[string]bool)
	for _, tool := range tools {
		nameSet[tool.Name] = true
	}
	for _, expected := range []string{"t1", "t2", "t3"} {
		if !nameSet[expected] {
			t.Errorf("missing tool %s", expected)
		}
	}
}

func TestConnectionManager_AllTools_AreDeterministicByServerName(t *testing.T) {
	mgr := NewConnectionManager()

	injectConnection(mgr, &Connection{
		Name:  "server-b",
		Tools: []ToolDescriptor{{Name: "b1"}, {Name: "b2"}},
	})
	injectConnection(mgr, &Connection{
		Name:  "server-a",
		Tools: []ToolDescriptor{{Name: "a1"}},
	})

	tools := mgr.AllTools()
	if len(tools) != 3 {
		t.Fatalf("tool count = %d, want 3", len(tools))
	}
	got := []string{tools[0].Name, tools[1].Name, tools[2].Name}
	want := []string{"a1", "b1", "b2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool order = %v, want %v", got, want)
		}
	}
}

func TestConnectionManager_CallTool_Success(t *testing.T) {
	mgr := NewConnectionManager()

	expectedID := nextID.Load() + 1
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, map[string]any{
				"content": []map[string]string{{"type": "text", "text": "result"}},
				"isError": false,
			}),
		},
	}

	injectConnection(mgr, &Connection{
		Name:      "my-server",
		transport: mt,
	})

	result, err := mgr.CallTool(context.Background(), "my-server", "test_tool", json.RawMessage(`{"key":"val"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.Content != "result" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestConnectionManager_CallTool_NoServer(t *testing.T) {
	mgr := NewConnectionManager()
	_, err := mgr.CallTool(context.TODO(), "nonexistent", "tool", nil)
	if err == nil {
		t.Error("should error for missing server")
	}
}

func TestConnectionManager_RefreshTools_UpdatesLiveConnection(t *testing.T) {
	mgr := NewConnectionManager()

	expectedID := nextID.Load() + 1
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, map[string]any{
				"tools": []map[string]any{
					{"name": "fresh_tool", "description": "fresh"},
				},
			}),
		},
	}

	injectConnection(mgr, &Connection{
		Name: "refreshable",
		Tools: []ToolDescriptor{
			{Name: "stale_tool", Description: "stale"},
		},
		transport: mt,
	})

	tools, err := mgr.RefreshTools(context.Background(), "refreshable")
	if err != nil {
		t.Fatalf("RefreshTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "fresh_tool" {
		t.Fatalf("returned tools = %#v", tools)
	}

	all := mgr.AllTools()
	if len(all) != 1 || all[0].Name != "fresh_tool" {
		t.Fatalf("live manager tools = %#v, want refreshed tool", all)
	}

	conn, ok := mgr.Connection("refreshable")
	if !ok {
		t.Fatal("expected refreshed connection snapshot")
	}
	if len(conn.Tools) != 1 || conn.Tools[0].Name != "fresh_tool" {
		t.Fatalf("connection snapshot tools = %#v, want refreshed tool", conn.Tools)
	}
}

func TestConnectionManager_RefreshTools_NoServer(t *testing.T) {
	mgr := NewConnectionManager()
	_, err := mgr.RefreshTools(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing server")
	}
}

func TestConnectionManager_Disconnect_Success(t *testing.T) {
	mgr := NewConnectionManager()

	mt := &mockTransport{}
	injectConnection(mgr, &Connection{Name: "to-disconnect", transport: mt})

	err := mgr.Disconnect("to-disconnect")
	if err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	if !mt.closed {
		t.Error("transport should be closed after disconnect")
	}

	// Should no longer be in the connections map.
	statuses := mgr.Statuses()
	if len(statuses) != 0 {
		t.Errorf("should have 0 connections after disconnect, got %d", len(statuses))
	}
}

func TestConnectionManager_CloseAll_WithConnections(t *testing.T) {
	mgr := NewConnectionManager()

	mt1 := &mockTransport{}
	mt2 := &mockTransport{}
	injectConnection(mgr, &Connection{Name: "s1", transport: mt1})
	injectConnection(mgr, &Connection{Name: "s2", transport: mt2})

	mgr.CloseAll()

	if !mt1.closed {
		t.Error("s1 transport should be closed")
	}
	if !mt2.closed {
		t.Error("s2 transport should be closed")
	}

	statuses := mgr.Statuses()
	if len(statuses) != 0 {
		t.Errorf("should have 0 connections after CloseAll, got %d", len(statuses))
	}
}

func TestConnectionManager_CloseAll_WithError(t *testing.T) {
	mgr := NewConnectionManager()

	mt := &mockTransport{closeErr: fmt.Errorf("close failed")}
	injectConnection(mgr, &Connection{Name: "s1", transport: mt})

	// Should not panic even with close error.
	mgr.CloseAll()

	if !mt.closed {
		t.Error("transport should still be marked closed")
	}
}

func TestConnectionManager_Connect_ReplacesExisting(t *testing.T) {
	mgr := NewConnectionManager()

	// Inject a connection that will be replaced.
	oldMt := &mockTransport{}
	injectConnection(mgr, &Connection{Name: "replaceable", transport: oldMt})

	// Attempt to connect with an unsupported transport (will fail after closing old).
	err := mgr.Connect(context.TODO(), McpServerConfig{
		Name:      "replaceable",
		Transport: "invalid",
	})
	if err == nil {
		t.Fatal("should fail with invalid transport")
	}

	// The old connection should have been closed.
	if !oldMt.closed {
		t.Error("old transport should be closed when replaced")
	}
}
