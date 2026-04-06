package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roboticus/testutil"
)

var bgCtx = context.Background()

func jsonBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	return body
}

// --- Stats tests ---

func TestGetTransactions(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := t
	_ = ctx
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO transactions (id, tx_type, amount, currency, created_at) VALUES ('tx1', 'credit', 10.5, 'USDC', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO transactions (id, tx_type, amount, currency, created_at) VALUES ('tx2', 'debit', 3.0, 'USDC', datetime('now'))`)

	handler := GetTransactions(store)
	req := httptest.NewRequest("GET", "/api/stats/transactions?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	txns, ok := body["transactions"].([]any)
	if !ok {
		t.Fatal("transactions is not an array")
	}
	if len(txns) != 2 {
		t.Errorf("got %d transactions, want 2", len(txns))
	}
}

func TestGetEfficiency(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost, cached, latency_ms, created_at)
		 VALUES ('c1', 'gpt-4', 'openai', 100, 50, 0.01, 0, 500, datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost, cached, latency_ms, created_at)
		 VALUES ('c2', 'gpt-4', 'openai', 200, 100, 0.02, 1, 300, datetime('now'))`)

	handler := GetEfficiency(store)
	req := httptest.NewRequest("GET", "/api/stats/efficiency?period=24h", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["requests"].(float64) != 2 {
		t.Errorf("requests = %v, want 2", body["requests"])
	}
	if body["total_tokens"].(float64) != 450 {
		t.Errorf("total_tokens = %v, want 450", body["total_tokens"])
	}
	if body["cache_hit_rate"].(float64) != 50 {
		t.Errorf("cache_hit_rate = %v, want 50", body["cache_hit_rate"])
	}
}

func TestGetModelSelections(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO model_selection_events (id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model, user_excerpt, candidates_json, created_at)
		 VALUES ('ms1', 't1', 's1', 'a1', 'api', 'gpt-4', 'heuristic', 'gpt-4', 'hello', '[]', datetime('now'))`)

	handler := GetModelSelections(store)
	req := httptest.NewRequest("GET", "/api/models/selections", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	events := body["events"].([]any)
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
}

func TestGetTimeseries(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost, created_at)
		 VALUES ('c1', 'gpt-4', 'openai', 100, 50, 0.01, datetime('now'))`)

	handler := GetTimeseries(store)
	req := httptest.NewRequest("GET", "/api/stats/timeseries?days=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	series, ok := body["series"].(map[string]any)
	if !ok {
		t.Fatal("series is not an object")
	}
	if series["buckets"] == nil {
		t.Error("buckets should not be nil")
	}
}

func TestGetRecommendations_ReturnsSignals(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost, cached, latency_ms, created_at)
		 VALUES
		 ('c1', 'gpt-4', 'openai', 3000, 2000, 0.05, 0, 2200, datetime('now')),
		 ('c2', 'gpt-4', 'openai', 3200, 1800, 0.04, 0, 1800, datetime('now'))`)

	handler := GetRecommendations(store)
	req := httptest.NewRequest("GET", "/api/stats/recommendations?period=24h", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	recommendations, ok := body["recommendations"].([]any)
	if !ok {
		t.Fatal("recommendations is not an array")
	}
	if len(recommendations) == 0 {
		t.Fatal("expected non-empty recommendations")
	}
}

func TestGenerateRecommendations_DelegatesToAnalysis(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GenerateRecommendations(store)
	req := httptest.NewRequest("POST", "/api/stats/recommendations/generate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	recommendations, ok := body["recommendations"].([]any)
	if !ok {
		t.Fatal("recommendations is not an array")
	}
	if len(recommendations) != 1 {
		t.Fatalf("got %d recommendations, want 1", len(recommendations))
	}
	first, ok := recommendations[0].(map[string]any)
	if !ok {
		t.Fatal("first recommendation is not an object")
	}
	if first["type"] != "observability" {
		t.Fatalf("first recommendation type = %v, want observability", first["type"])
	}
}

func TestGetWorkspaceState(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetWorkspaceState(store)
	req := httptest.NewRequest("GET", "/api/workspace/state", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["status"] != "running" {
		t.Errorf("status = %v", body["status"])
	}
	if body["goroutines"].(float64) < 1 {
		t.Error("goroutines should be >= 1")
	}
}

func TestGetSemanticCategories(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence) VALUES ('s1', 'facts', 'k1', 'v1', 0.9)`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence) VALUES ('s2', 'facts', 'k2', 'v2', 0.8)`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence) VALUES ('s3', 'prefs', 'k3', 'v3', 0.7)`)

	handler := GetSemanticCategories(store)
	req := httptest.NewRequest("GET", "/api/memory/semantic/categories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	cats := body["categories"].([]any)
	if len(cats) != 2 {
		t.Errorf("got %d categories, want 2", len(cats))
	}
}

func TestGetThemeCatalog(t *testing.T) {
	handler := GetThemeCatalog()
	req := httptest.NewRequest("GET", "/api/themes/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	themes := body["themes"].([]any)
	if len(themes) != 5 {
		t.Errorf("got %d themes, want 5", len(themes))
	}
}

func TestGetSetActiveTheme(t *testing.T) {
	store := testutil.TempStore(t)

	// Set theme.
	setReq := httptest.NewRequest("PUT", "/api/themes/active",
		strings.NewReader(`{"theme_id":"nord"}`))
	setRec := httptest.NewRecorder()
	SetActiveTheme(store).ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusOK {
		t.Fatalf("set status = %d", setRec.Code)
	}

	// Get theme.
	getReq := httptest.NewRequest("GET", "/api/themes/active", nil)
	getRec := httptest.NewRecorder()
	GetActiveTheme(store).ServeHTTP(getRec, getReq)
	body := jsonBody(t, getRec)
	if body["id"] != "nord" {
		t.Errorf("active theme = %v, want nord", body["id"])
	}
}

func TestSetActiveTheme_InvalidTheme(t *testing.T) {
	store := testutil.TempStore(t)

	req := httptest.NewRequest("PUT", "/api/themes/active",
		strings.NewReader(`{"theme_id":"not-a-theme"}`))
	rec := httptest.NewRecorder()
	SetActiveTheme(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["detail"] != "unknown theme_id" {
		t.Fatalf("detail = %v, want unknown theme_id", body["detail"])
	}
}

func TestListDelegations(t *testing.T) {
	store := testutil.TempStore(t)
	// Seed parent rows for FK constraints.
	if _, err := store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('sd1', 'agent1', 'test')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('td1', 'sd1')`); err != nil {
		t.Fatalf("seed turn: %v", err)
	}
	if _, err := store.ExecContext(bgCtx,
		`INSERT INTO delegation_outcomes (id, session_id, turn_id, task_description, assigned_agents_json, pattern, created_at)
		 VALUES ('d1', 'sd1', 'td1', 'test task', '["agent1"]', 'direct', datetime('now'))`); err != nil {
		t.Fatalf("seed delegation: %v", err)
	}

	handler := ListDelegations(store)
	req := httptest.NewRequest("GET", "/api/delegations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	delegations := body["delegations"].([]any)
	if len(delegations) != 1 {
		t.Errorf("got %d delegations, want 1", len(delegations))
	}
}

func TestGetThrottleStats(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO abuse_events (id, actor_id, origin, channel, signal_type, severity, action_taken, score, created_at)
		 VALUES ('a1', 'user1', 'api', 'web', 'rate_burst', 'low', 'allow', 0.15, datetime('now'))`)

	handler := GetThrottleStats(store)
	req := httptest.NewRequest("GET", "/api/stats/throttle?hours=24", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["total_events"].(float64) != 1 {
		t.Errorf("total_events = %v, want 1", body["total_events"])
	}
}

func TestListTraces(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, channel, total_ms, stages_json, created_at)
		 VALUES ('p1', 't1', 'api', 150, '[]', datetime('now'))`)

	handler := ListTraces(store)
	req := httptest.NewRequest("GET", "/api/traces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	traces := body["traces"].([]any)
	if len(traces) != 1 {
		t.Errorf("got %d traces, want 1", len(traces))
	}
}

func TestListTraces_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE pipeline_traces`); err != nil {
		t.Fatalf("drop pipeline_traces: %v", err)
	}

	handler := ListTraces(store)
	req := httptest.NewRequest("GET", "/api/traces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestGetWalletBalance(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO transactions (id, tx_type, amount, currency, created_at) VALUES ('t1', 'credit', 100.0, 'USDC', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO transactions (id, tx_type, amount, currency, created_at) VALUES ('t2', 'debit', 25.0, 'USDC', datetime('now'))`)

	handler := GetWalletBalance(store)
	req := httptest.NewRequest("GET", "/api/wallet/balance", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	balance := body["balance"].(float64)
	if balance != 75.0 {
		t.Errorf("balance = %v, want 75.0", balance)
	}
}

// --- Helpers tests ---

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		query    string
		key      string
		fallback int
		want     int
	}{
		{"", "limit", 50, 50},
		{"limit=10", "limit", 50, 10},
		{"limit=abc", "limit", 50, 50},
		{"limit=-1", "limit", 50, 50},
		{"limit=0", "limit", 50, 50},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/?"+tt.query, nil)
		got := parseIntParam(req, tt.key, tt.fallback)
		if got != tt.want {
			t.Errorf("parseIntParam(%q, %q, %d) = %d, want %d", tt.query, tt.key, tt.fallback, got, tt.want)
		}
	}
}

func TestParsePeriodHours(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 24},
		{"24h", 24},
		{"7d", 168},
		{"48", 48},
		{"abc", 24},
	}
	for _, tt := range tests {
		got := parsePeriodHours(tt.input, 24)
		if got != tt.want {
			t.Errorf("parsePeriodHours(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
