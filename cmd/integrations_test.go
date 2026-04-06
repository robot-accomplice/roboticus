package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestIntegrationsTestCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/channels/telegram/test" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": true})
	}))
	defer cleanup()

	err := integrationsTestCmd.RunE(integrationsTestCmd, []string{"telegram"})
	if err != nil {
		t.Fatalf("integrations test: %v", err)
	}
}

func TestIntegrationsTestCmd_Failure(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "channel not configured"})
	}))
	defer cleanup()

	err := integrationsTestCmd.RunE(integrationsTestCmd, []string{"slack"})
	if err == nil {
		t.Fatal("expected error for failed integration test")
	}
}

func TestIntegrationsHealthCmd_NonArrayChannels(t *testing.T) {
	// When channels key is not an array, should fall back to printJSON.
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"status": "degraded",
	}))
	defer cleanup()

	err := integrationsHealthCmd.RunE(integrationsHealthCmd, nil)
	if err != nil {
		t.Fatalf("integrations health non-array: %v", err)
	}
}

func TestIntegrationsHealthCmd_MixedStates(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"channels": []any{
			map[string]any{"name": "slack", "enabled": true, "status": "active"},
			map[string]any{"name": "discord", "enabled": false, "status": "disabled"},
			map[string]any{"name": "telegram", "enabled": true, "status": "error"},
			map[string]any{"name": "teams", "enabled": true, "status": "ok"},
			map[string]any{"name": "irc", "enabled": true, "status": "connected"},
		},
	}))
	defer cleanup()

	err := integrationsHealthCmd.RunE(integrationsHealthCmd, nil)
	if err != nil {
		t.Fatalf("integrations health mixed: %v", err)
	}
}
