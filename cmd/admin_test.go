package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminBreakerResetCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/breaker/reset/openai" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"reset": true})
	}))
	defer cleanup()

	err := adminBreakerResetCmd.RunE(adminBreakerResetCmd, []string{"openai"})
	if err != nil {
		t.Fatalf("admin breaker-reset: %v", err)
	}
}

func TestAdminStatsCmd_PartialFailure(t *testing.T) {
	callCount := 0
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Let the second endpoint fail with a non-JSON response on a closed connection.
		if r.URL.Path == "/api/stats/cache" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"cache unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "ok"})
	}))
	defer cleanup()

	// admin stats should not return an error even if one endpoint fails;
	// it prints a warning and continues.
	err := adminStatsCmd.RunE(adminStatsCmd, nil)
	if err != nil {
		t.Fatalf("admin stats with partial failure: %v", err)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 API calls, got %d", callCount)
	}
}
