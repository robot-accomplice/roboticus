package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLogsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify query params.
		if !strings.Contains(r.URL.String(), "lines=50") {
			t.Errorf("expected default lines=50 in URL, got %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []any{
				"2026-04-06T12:00:00Z INFO  server started",
				"2026-04-06T12:01:00Z DEBUG request received",
			},
		})
	}))
	defer cleanup()

	err := logsCmd.RunE(logsCmd, nil)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
}

func TestLogsCmd_StructuredEntries(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{
			map[string]any{"time": "2026-04-06T12:00:00Z", "level": "info", "message": "started"},
			map[string]any{"time": "2026-04-06T12:01:00Z", "level": "error", "message": "oops"},
		},
	}))
	defer cleanup()

	err := logsCmd.RunE(logsCmd, nil)
	if err != nil {
		t.Fatalf("logs structured: %v", err)
	}
}

func TestLogsCmd_NonArrayEntries(t *testing.T) {
	// When the response doesn't have an "entries" array, it should fall back to printJSON.
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"log": "raw log data",
	}))
	defer cleanup()

	err := logsCmd.RunE(logsCmd, nil)
	if err != nil {
		t.Fatalf("logs non-array: %v", err)
	}
}

func TestLogsCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"error": "shutting down"})
	}))
	defer cleanup()

	err := logsCmd.RunE(logsCmd, nil)
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
}

func TestFollowLogs_ConnectionRefused(t *testing.T) {
	// Point viper at a port that's guaranteed not listening.
	orig := viper.GetInt("server.port")
	viper.Set("server.port", 19999)
	defer viper.Set("server.port", orig)

	err := followLogs("/api/logs?lines=10")
	if err == nil {
		t.Fatal("expected connection error from followLogs")
	}
	if !strings.Contains(err.Error(), "connection failed") {
		t.Errorf("expected 'connection failed' in error, got: %v", err)
	}
}
