package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSessionsShowCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/sessions/s123" && !strings.HasSuffix(r.URL.Path, "/messages"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": "s123", "agent_id": "default", "scope_key": "test",
			})
		case strings.HasSuffix(r.URL.Path, "/messages"):
			json.NewEncoder(w).Encode(map[string]any{
				"messages": []any{
					map[string]any{"role": "user", "content": "Hello"},
					map[string]any{"role": "assistant", "content": strings.Repeat("x", 200)},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
		}
	}))
	defer cleanup()

	err := sessionsShowCmd.RunE(sessionsShowCmd, []string{"s123"})
	if err != nil {
		t.Fatalf("sessions show: %v", err)
	}
}

func TestSessionsDeleteCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/sessions/s123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer cleanup()

	err := sessionsDeleteCmd.RunE(sessionsDeleteCmd, []string{"s123"})
	if err != nil {
		t.Fatalf("sessions delete: %v", err)
	}
}

func TestSessionsDeleteCmd_NotFound(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("session not found"))
	}))
	defer cleanup()

	err := sessionsDeleteCmd.RunE(sessionsDeleteCmd, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestSessionsExportCmd_JSON_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "What is 2+2?"},
			map[string]any{"role": "assistant", "content": "4"},
		},
	}))
	defer cleanup()

	// Default format is "json".
	err := sessionsExportCmd.RunE(sessionsExportCmd, []string{"s456"})
	if err != nil {
		t.Fatalf("sessions export json: %v", err)
	}
}

func TestSessionsExportCmd_Markdown_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "Tell me a joke"},
			map[string]any{"role": "assistant", "content": "Why did the chicken..."},
		},
	}))
	defer cleanup()

	// Set the format flag to markdown.
	sessionsExportCmd.Flags().Set("format", "markdown")
	defer sessionsExportCmd.Flags().Set("format", "json") // restore

	err := sessionsExportCmd.RunE(sessionsExportCmd, []string{"s789"})
	if err != nil {
		t.Fatalf("sessions export markdown: %v", err)
	}
}

func TestSessionsListCmd_WithMultipleSessions(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"sessions": []any{
			map[string]any{"id": "s1", "agent_id": "default", "scope_key": "test", "nickname": "Bot1"},
			map[string]any{"id": "s2", "agent_id": "coder", "scope_key": "dev", "nickname": "Bot2"},
			map[string]any{"id": "s3", "agent_id": "default", "scope_key": "prod", "nickname": "Bot3"},
		},
	}))
	defer cleanup()

	err := sessionsListCmd.RunE(sessionsListCmd, nil)
	if err != nil {
		t.Fatalf("sessions list multiple: %v", err)
	}
}

func TestSessionsShowCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "database error"})
	}))
	defer cleanup()

	err := sessionsShowCmd.RunE(sessionsShowCmd, []string{"s999"})
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestSessionsExportCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": "session not found"})
	}))
	defer cleanup()

	err := sessionsExportCmd.RunE(sessionsExportCmd, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}
