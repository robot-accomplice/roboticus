package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// --- Existing DashboardHandler tests ---

func TestDashboardHandler_StatusOK(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestDashboardHandler_ContentType(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
}

func TestDashboardHandler_CSPHeader(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("CSP header missing")
	}
	if !strings.Contains(csp, "nonce-") {
		t.Error("CSP should contain a nonce")
	}
	if !strings.Contains(csp, "script-src") {
		t.Error("CSP should contain script-src directive")
	}
	if !strings.Contains(csp, "fonts.googleapis.com") {
		t.Error("CSP should allow Google Fonts")
	}
}

func TestDashboardHandler_NonceInScriptTags(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `<script nonce="`) {
		t.Error("script tags should have nonce attribute")
	}
	// Bare <script> without nonce should not exist.
	if strings.Contains(body, "<script>") {
		t.Error("found <script> without nonce")
	}
}

func TestDashboardHandler_ContainsRoboticus(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Roboticus") {
		t.Error("dashboard should contain 'Roboticus' branding")
	}
}

func TestDashboardHandler_PromptCompressionMarkedBenchmarkOnly(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Experimental benchmark-only feature") {
		t.Fatal("dashboard should mark prompt compression as benchmark-only")
	}
	if !strings.Contains(body, "Not recommended for live use on the current runtime") {
		t.Fatal("dashboard should warn that prompt compression is not recommended for live use")
	}
}

func TestDashboardHandler_SecurityHeaders(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}

func TestDashboardHandler_UniqueNoncePerRequest(t *testing.T) {
	handler := DashboardHandler()

	// Two requests should get different nonces.
	req1 := httptest.NewRequest("GET", "/", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "/", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	csp1 := rec1.Header().Get("Content-Security-Policy")
	csp2 := rec2.Header().Get("Content-Security-Policy")
	if csp1 == csp2 {
		t.Error("two requests should have different nonces")
	}
}

func TestGenerateNonce(t *testing.T) {
	n1 := generateNonce()
	n2 := generateNonce()

	if len(n1) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("nonce length = %d, want 32", len(n1))
	}
	if n1 == n2 {
		t.Error("two nonces should differ")
	}
}

// --- Dashboard page-level data source tests ---

// newTestServer creates a minimal API server backed by a temp DB for dashboard
// endpoint testing. No LLM calls are made; routes that need an LLM service
// receive a minimal mock.
func newTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	store := testutil.TempStore(t)

	mockLLM := testutil.MockLLMServer(t, func(body map[string]any) (int, any) {
		return 200, map[string]any{
			"id": "chatcmpl-test", "model": "test-model",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		}
	})

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{{
			Name: "mock", URL: mockLLM.URL, Format: llm.FormatOpenAI,
			IsLocal: true, ChatPath: "/v1/chat/completions", AuthHeader: "Bearer",
		}},
		Primary: "mock/test-model",
	}, store)
	if err != nil {
		t.Fatalf("llm service: %v", err)
	}

	cfgVal := core.DefaultConfig()
	cfg := &cfgVal
	eventBus := NewEventBus(64)

	state := &AppState{
		Store:    store,
		LLM:      llmSvc,
		Config:   cfg,
		EventBus: eventBus,
	}

	srv := NewServer(context.Background(), DefaultServerConfig(), state)
	ts := httptest.NewServer(srv.Handler)

	return ts, func() { ts.Close() }
}

// assertEndpointJSON hits an endpoint, asserts 200 status and valid JSON.
func assertEndpointJSON(t *testing.T, client *http.Client, base, path string) {
	t.Helper()

	resp, err := client.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s: status %d, want 200; body=%s", path, resp.StatusCode, string(body))
		return
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Errorf("GET %s: invalid JSON: %v; body=%s", path, err, string(body))
	}
}

func TestDashboard_OverviewDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/health",
		"/api/agent/status",
		"/api/sessions",
		"/api/skills",
		"/api/cron/jobs",
		"/api/stats/cache",
		"/api/wallet/balance",
		"/api/channels/status",
		"/api/breaker/status",
		"/api/stats/costs",
		"/api/stats/timeseries",
		"/api/workspace/state",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_SessionsPageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/sessions",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_MemoryPageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/memory/working",
		"/api/memory/episodic",
		"/api/memory/semantic",
		"/api/memory/semantic/categories",
		"/api/memory/health",
		"/api/stats/memory-analytics",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_SkillsPageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/skills",
		"/api/skills/catalog",
		"/api/skills/audit",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_SchedulerPageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/cron/jobs",
		"/api/cron/runs",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_MetricsPageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/stats/costs",
		"/api/stats/cache",
		"/api/stats/timeseries",
		"/api/stats/efficiency",
		"/api/stats/transactions",
		"/api/stats/throttle",
		"/api/delegations",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_WalletPageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/wallet/balance",
		"/api/wallet/address",
		"/api/services/swaps",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_SettingsPageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/config",
		"/api/config/capabilities",
		"/api/models/available",
		"/api/routing/profile",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}

func TestDashboard_WorkspacePageDataSources(t *testing.T) {
	ts, cleanup := newTestServer(t)
	defer cleanup()
	client := ts.Client()

	endpoints := []string{
		"/api/workspace/state",
		"/api/workspace/tasks",
		"/api/admin/task-events",
		"/api/plugins",
		"/api/subagents",
		"/api/roster",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			assertEndpointJSON(t, client, ts.URL, ep)
		})
	}
}
