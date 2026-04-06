package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPluginsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"plugins": []any{
			map[string]any{"name": "calculator", "version": "1.0"},
		},
	}))
	defer cleanup()

	err := pluginsListCmd.RunE(pluginsListCmd, nil)
	if err != nil {
		t.Fatalf("plugins list: %v", err)
	}
}

func TestPluginsListCmd_FallbackToSkills(t *testing.T) {
	callCount := 0
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"plugins": []any{}})
	}))
	defer cleanup()

	err := pluginsListCmd.RunE(pluginsListCmd, nil)
	if err != nil {
		t.Fatalf("plugins list: %v", err)
	}
	if callCount < 1 {
		t.Errorf("expected at least 1 API call, got %d", callCount)
	}
}

func TestPluginsListCmd_BothEndpointsFail(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
	}))
	defer cleanup()

	err := pluginsListCmd.RunE(pluginsListCmd, nil)
	if err == nil {
		t.Fatal("expected error when both endpoints fail")
	}
}
