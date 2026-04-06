package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/testutil"
)

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	if cfg.Port != core.DefaultServerPort {
		t.Errorf("Port = %d, want %d", cfg.Port, core.DefaultServerPort)
	}
	if cfg.Bind != core.DefaultServerBind {
		t.Errorf("Bind = %q, want %q", cfg.Bind, core.DefaultServerBind)
	}
	if cfg.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 60*time.Second {
		t.Errorf("WriteTimeout = %v", cfg.WriteTimeout)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey should be empty by default, got %q", cfg.APIKey)
	}
}

// minimalAppState creates a minimal AppState for NewServer tests.
func minimalAppState(t *testing.T, store *db.Store) *AppState {
	t.Helper()
	cfg := core.DefaultConfig()
	return &AppState{
		Store:    store,
		Config:   &cfg,
		EventBus: NewEventBus(16),
	}
}

func TestNewServer_ReturnsNonNil(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "test-key"

	srv := NewServer(cfg, state)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	expectedAddr := fmt.Sprintf("%s:%d", core.DefaultServerBind, core.DefaultServerPort)
	if srv.Addr != expectedAddr {
		t.Errorf("Addr = %q, want %q", srv.Addr, expectedAddr)
	}
	if srv.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 60*time.Second {
		t.Errorf("WriteTimeout = %v", srv.WriteTimeout)
	}
}

func TestNewServer_HealthEndpointPublic(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "secret-key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// Health endpoint should not require API key.
	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/api/health status = %d, want 200", resp.StatusCode)
	}

	// Also /health alias.
	resp2, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("/health status = %d, want 200", resp2.StatusCode)
	}
}

func TestNewServer_AuthenticatedEndpointRequiresKey(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "my-secret"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// Unauthenticated request to authenticated route should fail.
	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated /api/sessions status = %d, want 401", resp.StatusCode)
	}

	// Authenticated request should succeed.
	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("x-api-key", "my-secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GET /api/sessions: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("authenticated /api/sessions status = %d, want 200", resp2.StatusCode)
	}
}

func TestNewServer_OpenAPIEndpoint(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("GET /openapi.yaml: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
}

func TestNewServer_DocsEndpoint(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/docs")
	if err != nil {
		t.Fatalf("GET /api/docs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "swagger-ui") {
		t.Error("docs page should contain swagger-ui")
	}
}

func TestNewServer_AgentCardEndpoint(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("GET /.well-known/agent.json: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestNewServer_DashboardEndpoint(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/dashboard", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /dashboard: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestNewServer_AnalysisLimitConcurrency(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// The analysis endpoints exist and respond (even if with 404 for missing session).
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/nonexistent/analyze", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/sessions/nonexistent/analyze: %v", err)
	}
	defer resp.Body.Close()
	// We just want to confirm the route exists; error response is fine.
	if resp.StatusCode == http.StatusMethodNotAllowed {
		t.Error("route should exist")
	}
}

func TestNewServer_CORSHeaders(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	state.Config.CORS.AllowedOrigins = []string{"http://example.com"}
	cfg := DefaultServerConfig()

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "http://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Errorf("ACAO = %q, want http://example.com", got)
	}
}

func TestNewServer_SecurityHeaders(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestNewServer_ConfigEndpoint(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/config", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestNewServer_StatsEndpoints(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	endpoints := []string{
		"/api/stats/costs",
		"/api/stats/cache",
		"/api/stats/transactions",
		"/api/stats/efficiency",
		"/api/stats/timeseries",
		"/api/stats/throttle",
		"/api/stats/memory-analytics",
	}

	for _, ep := range endpoints {
		req, _ := http.NewRequest("GET", ts.URL+ep, nil)
		req.Header.Set("x-api-key", "key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("GET %s: %v", ep, err)
			continue
		}
		resp.Body.Close()
		// Just verify the route exists (not 404/405).
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("GET %s: status = %d, route should exist", ep, resp.StatusCode)
		}
	}
}

func TestNewServer_MemoryEndpoints(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	endpoints := []string{
		"/api/memory/working",
		"/api/memory/episodic",
		"/api/memory/semantic",
		"/api/memory/semantic/categories",
		"/api/memory/health",
	}

	for _, ep := range endpoints {
		req, _ := http.NewRequest("GET", ts.URL+ep, nil)
		req.Header.Set("x-api-key", "key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("GET %s: %v", ep, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("GET %s: route not found (status %d)", ep, resp.StatusCode)
		}
	}
}

func TestNewServer_WebSocketRouteExists(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// A plain GET to /ws without upgrade should fail with a websocket error, not 404.
	req, _ := http.NewRequest("GET", ts.URL+"/ws", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /ws: %v", err)
	}
	defer resp.Body.Close()
	// Should not be 404 (route exists), but will fail because it's not a WS upgrade.
	if resp.StatusCode == http.StatusNotFound {
		t.Error("/ws route should exist")
	}
}

func TestNewServer_RateLimitWired(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	state.Config.RateLimit.Enabled = true
	state.Config.RateLimit.RequestsPerWindow = 3
	state.Config.RateLimit.WindowSeconds = 60
	cfg := DefaultServerConfig()

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// Make requests until rate limited.
	var lastStatus int
	for i := 0; i < 5; i++ {
		resp, err := http.Get(ts.URL + "/api/health")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		lastStatus = resp.StatusCode
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exceeding rate limit, got %d", lastStatus)
	}
}

func TestNewServer_SkillsEndpoints(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/skills", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Error("/api/skills route should exist")
	}
}

func TestNewServer_TracesEndpoints(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/traces", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Error("/api/traces route should exist")
	}
}

func TestNewServer_CronEndpoints(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/cron/jobs", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Error("/api/cron/jobs route should exist")
	}
}

func TestNewServer_NoApprovals(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	// Approvals is nil by default.
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// /api/approvals should not exist when Approvals is nil.
	req, _ := http.NewRequest("GET", ts.URL+"/api/approvals", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("/api/approvals with nil service should be 404/405, got %d", resp.StatusCode)
	}
}

func TestNewServer_NoBrowser(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	// Browser is nil by default.
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// /api/browser/status should not exist when Browser is nil.
	req, _ := http.NewRequest("GET", ts.URL+"/api/browser/status", nil)
	req.Header.Set("x-api-key", "key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("/api/browser/status with nil browser should be 404/405, got %d", resp.StatusCode)
	}
}

func TestNewServer_BodyLimitWired(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()
	cfg.APIKey = "key"

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// Send a body larger than 1MB to an endpoint that reads the body.
	bigBody := strings.NewReader(strings.Repeat("x", 2<<20)) // 2MB
	req, _ := http.NewRequest("POST", ts.URL+"/api/agent/message", bigBody)
	req.Header.Set("x-api-key", "key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	// The request should fail somehow due to body limit.
	// It may be 400, 413, or 500 depending on how the handler reads the body.
	if resp.StatusCode == http.StatusOK {
		t.Error("oversized body should not succeed")
	}
}

func TestNewServer_PrefixedAndUnprefixedHealth(t *testing.T) {
	store := testutil.TempStore(t)
	state := minimalAppState(t, store)
	cfg := DefaultServerConfig()

	srv := NewServer(cfg, state)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// Both /health and /api/health should return the same structure.
	for _, path := range []string{"/health", "/api/health"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Errorf("%s: invalid JSON: %v", path, err)
			continue
		}
		if _, ok := result["status"]; !ok {
			t.Errorf("%s: response missing 'status' field", path)
		}
	}
}
