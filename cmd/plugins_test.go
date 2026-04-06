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
		if r.URL.Path == "/api/plugins/catalog/install" {
			// First request fails.
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
			return
		}
		// Fallback to skills endpoint.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"skills": []any{}})
	}))
	defer cleanup()

	err := pluginsListCmd.RunE(pluginsListCmd, nil)
	if err != nil {
		t.Fatalf("plugins list fallback: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 API calls for fallback, got %d", callCount)
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
