package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAuthStatusCmd_WithProviders(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"api_key_env": "OPENAI_API_KEY",
			},
			"anthropic": map[string]any{
				"api_key_env": "ANTHROPIC_API_KEY",
			},
		},
	}))
	defer cleanup()

	err := authStatusCmd.RunE(authStatusCmd, nil)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}
}

func TestAuthStatusCmd_NoProviders(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"other": "data",
	}))
	defer cleanup()

	err := authStatusCmd.RunE(authStatusCmd, nil)
	if err != nil {
		t.Fatalf("auth status no providers: %v", err)
	}
}

func TestAuthStatusCmd_EmptyKeyEnv(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"api_key_env": "",
			},
		},
	}))
	defer cleanup()

	err := authStatusCmd.RunE(authStatusCmd, nil)
	if err != nil {
		t.Fatalf("auth status empty key env: %v", err)
	}
}

func TestAuthLogoutCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/providers/openai/key" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer cleanup()

	err := authLogoutCmd.RunE(authLogoutCmd, []string{"openai"})
	if err != nil {
		t.Fatalf("auth logout: %v", err)
	}
}

func TestAuthLogoutCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("provider not found"))
	}))
	defer cleanup()

	err := authLogoutCmd.RunE(authLogoutCmd, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestAuthStatusCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "internal error"})
	}))
	defer cleanup()

	err := authStatusCmd.RunE(authStatusCmd, nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
