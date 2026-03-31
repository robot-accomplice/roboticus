package mcp

import "testing"

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
	err := mgr.Connect(nil, McpServerConfig{
		Name:      "test",
		Transport: "invalid",
	})
	if err == nil {
		t.Error("should return error for unsupported transport")
	}
}
