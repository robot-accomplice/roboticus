package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCircuitResetCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/breaker/reset/anthropic" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"reset": true, "provider": "anthropic"})
	}))
	defer cleanup()

	err := circuitResetCmd.RunE(circuitResetCmd, []string{"anthropic"})
	if err != nil {
		t.Fatalf("circuit reset: %v", err)
	}
}

func TestCircuitStatusCmd_NonArrayBreakers(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"status": "all clear",
	}))
	defer cleanup()

	err := circuitStatusCmd.RunE(circuitStatusCmd, nil)
	if err != nil {
		t.Fatalf("circuit status non-array: %v", err)
	}
}

func TestCircuitResetCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": "unknown provider"})
	}))
	defer cleanup()

	err := circuitResetCmd.RunE(circuitResetCmd, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestCircuitStatusCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"error": "service down"})
	}))
	defer cleanup()

	err := circuitStatusCmd.RunE(circuitStatusCmd, nil)
	if err == nil {
		t.Fatal("expected error for 503")
	}
}
