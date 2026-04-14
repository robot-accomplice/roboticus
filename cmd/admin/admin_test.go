package admin

import (
	"roboticus/cmd/internal/testhelp"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminStatsCmd_PartialFailure(t *testing.T) {
	callCount := 0
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
