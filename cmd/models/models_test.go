package models

import (
	"encoding/json"
	"net/http"
	"roboticus/cmd/internal/testhelp"
	"testing"
)

func TestModelsListCmd_NonArrayModels(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"info": "model data not available",
	}))
	defer cleanup()

	err := modelsListCmd.RunE(modelsListCmd, nil)
	if err != nil {
		t.Fatalf("models list non-array: %v", err)
	}
}

func TestModelsListCmd_MultipleModels(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"models": []any{
			map[string]any{"id": "gpt-4o", "provider": "openai", "context_window": float64(128000)},
			map[string]any{"id": "claude-3-opus", "provider": "anthropic", "context_window": float64(200000)},
			map[string]any{"id": "gemini-pro", "provider": "google", "context_window": float64(32000)},
		},
	}))
	defer cleanup()

	err := modelsListCmd.RunE(modelsListCmd, nil)
	if err != nil {
		t.Fatalf("models list multiple: %v", err)
	}
}

func TestModelsListCmd_ServerError(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal error"})
	}))
	defer cleanup()

	err := modelsListCmd.RunE(modelsListCmd, nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestModelsDiagnosticsCmd_ServerError(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "routing error"})
	}))
	defer cleanup()

	err := modelsDiagnosticsCmd.RunE(modelsDiagnosticsCmd, nil)
	if err == nil {
		t.Fatal("expected error for routing diagnostics failure")
	}
}
