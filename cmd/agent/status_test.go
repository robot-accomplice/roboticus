package agent

import (
	"roboticus/cmd/internal/testhelp"
	"encoding/json"
	"net/http"
	"testing"
)

func TestStatusCmd_WithMockServer(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"uptime": "2h30m",
				"go":     "go1.22",
				"providers": []any{
					map[string]any{"name": "openai", "format": "openai", "state": "healthy"},
					map[string]any{"name": "anthropic", "format": "anthropic", "state": "healthy"},
				},
			})
		case "/api/agent/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "running",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
		}
	}))
	defer cleanup()

	err := statusCmd.RunE(statusCmd, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
}

func TestStatusCmd_HealthFailure(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "database locked"})
	}))
	defer cleanup()

	err := statusCmd.RunE(statusCmd, nil)
	if err == nil {
		t.Fatal("expected error when health check fails")
	}
}

func TestStatusCmd_NoProviders(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"uptime": "1m",
				"go":     "go1.22",
			})
		case "/api/agent/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "idle"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
		}
	}))
	defer cleanup()

	err := statusCmd.RunE(statusCmd, nil)
	if err != nil {
		t.Fatalf("status without providers: %v", err)
	}
}

func TestStatusCmd_AgentStatusUnavailable(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"uptime": "5s",
				"go":     "go1.22",
			})
		default:
			// Agent status 404 - should not cause an error.
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
		}
	}))
	defer cleanup()

	err := statusCmd.RunE(statusCmd, nil)
	if err != nil {
		t.Fatalf("status with unavailable agent: %v", err)
	}
}
