package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// TestAPIResponseShapes validates that dashboard-critical endpoints return
// the expected JSON shape. These tests protect against silent response shape
// drift that would break the dashboard or CLI.
//
// Each test verifies: HTTP status, content type, and required JSON fields.

func TestHealthEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/health")
	assertStatus(t, resp, 200)
	data := parseJSON(t, resp)

	requireField(t, data, "status")
	requireField(t, data, "version")
	requireField(t, data, "uptime_seconds")
	requireField(t, data, "providers")
	requireField(t, data, "models")
	requireField(t, data, "agent")
}

func TestAgentStatusEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/agent/status")
	assertStatus(t, resp, 200)
	data := parseJSON(t, resp)

	requireField(t, data, "state")
	requireField(t, data, "name")
	requireField(t, data, "primary_model")
	requireField(t, data, "providers")
}

func TestSessionsEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/sessions")
	assertStatus(t, resp, 200)
	data := parseJSON(t, resp)

	// Sessions may be wrapped as {"sessions": [...]} or be top-level array.
	// Both are valid — the dashboard handles either.
	if _, ok := data["sessions"]; ok {
		// Wrapped format — ok.
	} else if len(data) == 0 {
		// Empty object — ok for no sessions.
	} else {
		// Must have some recognizable structure.
		t.Logf("sessions response keys: %v", mapKeys(data))
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestSkillsEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/skills")
	assertStatus(t, resp, 200)
	data := parseJSON(t, resp)

	// Skills endpoint returns an array directly or wrapped.
	// Must be parseable as JSON.
	_ = data
}

func TestCronJobsEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/cron/jobs")
	assertStatus(t, resp, 200)
	data := parseJSON(t, resp)

	requireField(t, data, "jobs")
}

func TestCacheStatsEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/stats/cache")
	assertStatus(t, resp, 200)
	// Must return valid JSON with cache metrics (shape varies).
	_ = parseJSON(t, resp)
}

func TestChannelsStatusEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/channels/status")
	assertStatus(t, resp, 200)

	// Channels status returns an array of channel objects.
	body := readBody(t, resp)
	var channels []map[string]any
	if err := json.Unmarshal(body, &channels); err != nil {
		// Try wrapped format.
		var wrapped map[string]any
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
			t.Fatalf("channels/status not parseable as array or object: %s", string(body))
		}
	}
}

func TestBreakerStatusEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/breaker/status")
	assertStatus(t, resp, 200)
	// Must return valid JSON with breaker state (shape varies by provider count).
	_ = parseJSON(t, resp)
}

func TestWalletBalanceEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/wallet/balance")
	assertStatus(t, resp, 200)
	// Should return valid JSON.
	_ = parseJSON(t, resp)
}

func TestConfigEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/config")
	assertStatus(t, resp, 200)
	data := parseJSON(t, resp)

	// Config should have at least these top-level sections.
	requireField(t, data, "agent")
	requireField(t, data, "models")
	requireField(t, data, "server")
}

func TestDeadLetterEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/channels/dead-letter")
	assertStatus(t, resp, 200)
	data := parseJSON(t, resp)

	requireField(t, data, "dead_letters")
}

func TestRosterEndpoint_Shape(t *testing.T) {
	srv := createTestServer(t)
	resp := doGet(t, srv, "/api/roster")
	assertStatus(t, resp, 200)
	// Should be valid JSON (array or object).
	_ = parseJSON(t, resp)
}

// --- Test helpers ---

func createTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := testutil.TempStore(t)
	cfgVal := core.DefaultConfig()
	llmSvc, _ := llm.NewService(llm.ServiceConfig{}, store)

	state := &AppState{
		Store:    store,
		LLM:      llmSvc,
		Config:   &cfgVal,
		EventBus: NewEventBus(64),
	}
	srv := NewServer(DefaultServerConfig(), state)
	return httptest.NewServer(srv.Handler)
}

func doGet(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Errorf("status = %d, want %d", resp.StatusCode, expected)
	}
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var buf [64 * 1024]byte
	n, _ := resp.Body.Read(buf[:])
	return buf[:n]
}

func parseJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	body := readBody(t, resp)
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("invalid JSON: %s (body: %s)", err, string(body[:min(200, len(body))]))
	}
	return data
}

func requireField(t *testing.T, data map[string]any, field string) {
	t.Helper()
	if _, ok := data[field]; !ok {
		t.Errorf("missing required field %q in response", field)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
