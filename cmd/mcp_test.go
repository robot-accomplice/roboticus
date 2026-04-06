package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMCPConnectCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/mcp/connect" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "filesystem" {
			t.Errorf("unexpected name: %v", body["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"connected": true, "tools": 3})
	}))
	defer cleanup()

	err := mcpConnectCmd.RunE(mcpConnectCmd, []string{"filesystem"})
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
}

func TestMCPDisconnectCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/mcp/disconnect/filesystem" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"disconnected": true})
	}))
	defer cleanup()

	err := mcpDisconnectCmd.RunE(mcpDisconnectCmd, []string{"filesystem"})
	if err != nil {
		t.Fatalf("mcp disconnect: %v", err)
	}
}

func TestMCPConnectCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": "invalid server name"})
	}))
	defer cleanup()

	err := mcpConnectCmd.RunE(mcpConnectCmd, []string{"bad-server"})
	if err == nil {
		t.Fatal("expected error for bad server name")
	}
}

func TestMCPListCmd_NonArrayConnections(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"status": "not available",
	}))
	defer cleanup()

	err := mcpListCmd.RunE(mcpListCmd, nil)
	if err != nil {
		t.Fatalf("mcp list non-array: %v", err)
	}
}
