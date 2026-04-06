package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestMemoryStatsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/working"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": []any{"a", "b", "c"},
			})
		case strings.HasSuffix(r.URL.Path, "/episodic"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"memories": []any{"d", "e"},
			})
		case strings.HasSuffix(r.URL.Path, "/semantic"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"other_key": "no entries",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
		}
	}))
	defer cleanup()

	err := memoryStatsCmd.RunE(memoryStatsCmd, nil)
	if err != nil {
		t.Fatalf("memory stats: %v", err)
	}
}

func TestMemoryStatsCmd_AllFailing(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "down"})
	}))
	defer cleanup()

	// memory stats should not return an error; it just skips failing tiers.
	err := memoryStatsCmd.RunE(memoryStatsCmd, nil)
	if err != nil {
		t.Fatalf("memory stats all failing: %v", err)
	}
}

func TestMemorySearchCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "search failed"})
	}))
	defer cleanup()

	err := memorySearchCmd.RunE(memorySearchCmd, []string{"test-query"})
	if err == nil {
		t.Fatal("expected error for failed search")
	}
}

func TestMemorySearchCmd_SimpleQuery(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.String(), "q=robotics") {
			t.Errorf("expected query param q=robotics in URL, got %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{
			map[string]any{"content": "robotics fact", "score": 0.9},
		}})
	}))
	defer cleanup()

	err := memorySearchCmd.RunE(memorySearchCmd, []string{"robotics"})
	if err != nil {
		t.Fatalf("memory search: %v", err)
	}
}
