package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/testutil"
)

var bgCtx = context.Background()

func testConfig() *core.Config {
	c := core.DefaultConfig()
	return &c
}

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
	if body["labels"] == nil {
		t.Error("labels should not be nil")
	}
	series, ok := body["series"].(map[string]any)
	if !ok {
		t.Fatal("series is not an object")
	}
	if series["cost_per_hour"] == nil {
		t.Error("cost_per_hour should not be nil")
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
	if first["category"] != "observability" {
		t.Fatalf("first recommendation category = %v, want observability", first["category"])
	}
}

func TestGetWorkspaceState(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetWorkspaceState(store, testConfig())
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
	store := testutil.TempStore(t)
	handler := GetThemeCatalog(store)
	req := httptest.NewRequest("GET", "/api/themes/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	themes := body["themes"].([]any)
	if len(themes) != 7 {
		t.Errorf("got %d themes, want 7", len(themes))
	}
}

func TestInstallCatalogThemeAndActivate(t *testing.T) {
	store := testutil.TempStore(t)

	installReq := httptest.NewRequest("POST", "/api/themes/catalog/install", strings.NewReader(`{"id":"dracula"}`))
	installRec := httptest.NewRecorder()
	InstallCatalogTheme(store).ServeHTTP(installRec, installReq)
	if installRec.Code != http.StatusOK {
		t.Fatalf("install status = %d", installRec.Code)
	}

	setReq := httptest.NewRequest("PUT", "/api/themes/active", strings.NewReader(`{"theme_id":"dracula"}`))
	setRec := httptest.NewRecorder()
	SetActiveTheme(store).ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusOK {
		t.Fatalf("set status = %d", setRec.Code)
	}

	getReq := httptest.NewRequest("GET", "/api/themes/active", nil)
	getRec := httptest.NewRecorder()
	GetActiveTheme(store).ServeHTTP(getRec, getReq)
	body := jsonBody(t, getRec)
	if body["id"] != "dracula" {
		t.Errorf("active theme = %v, want dracula", body["id"])
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

func TestSearchTraces(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, react_trace_json, created_at)
		 VALUES ('pt1', 't1', 's1', 'api', 250, '[{"tool":"search_files"}]', '{"guard":"approval"}', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, react_trace_json, created_at)
		 VALUES ('pt2', 't2', 's2', 'chat', 50, '[{"tool":"web_search"}]', '{"guard":"none"}', datetime('now'))`)

	req := httptest.NewRequest("GET", "/api/traces/search?tool_name=search_files&guard_name=approval&min_duration_ms=100", nil)
	rec := httptest.NewRecorder()
	SearchTraces(store).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["count"] != float64(1) {
		t.Fatalf("count = %v, want 1", body["count"])
	}
	results := body["results"].([]any)
	first := results[0].(map[string]any)
	if first["turn_id"] != "t1" {
		t.Errorf("turn_id = %v, want t1", first["turn_id"])
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
	// Create the wallet_balances table (migration 030).
	_, _ = store.ExecContext(bgCtx,
		`CREATE TABLE IF NOT EXISTS wallet_balances (
			symbol TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', balance REAL NOT NULL DEFAULT 0.0,
			contract TEXT NOT NULL DEFAULT '', decimals INTEGER NOT NULL DEFAULT 18,
			is_native INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT (datetime('now')))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO wallet_balances (symbol, name, balance, contract, decimals, is_native)
		 VALUES ('USDC', 'USD Coin', 75.0, '0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913', 6, 0)`)

	handler := GetWalletBalance(store)
	req := httptest.NewRequest("GET", "/api/wallet/balance", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	balance := body["balance"].(float64)
	if balance != 75.0 {
		t.Errorf("balance = %v, want 75.0", balance)
	}
	tokens := body["tokens"].([]any)
	if len(tokens) != 1 {
		t.Errorf("tokens count = %d, want 1", len(tokens))
	}
}

func TestGetWalletBalance_Empty(t *testing.T) {
	store := testutil.TempStore(t)

	handler := GetWalletBalance(store)
	req := httptest.NewRequest("GET", "/api/wallet/balance", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["currency"] != "USDC" {
		t.Errorf("currency = %v", body["currency"])
	}
}

func TestTriggerConsolidation(t *testing.T) {
	store := testutil.TempStore(t)
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO episodic_memory (id, content, importance, classification, memory_state)
		 VALUES (?, ?, ?, ?, ?)`,
		db.NewID(), "remember the launch checklist", 7, "fact", "active")
	if err != nil {
		t.Fatalf("seed episodic_memory: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/memory/consolidate?force=true", nil)
	rec := httptest.NewRecorder()

	TriggerConsolidation(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := jsonBody(t, rec)
	if body["ok"] != true {
		t.Fatal("expected ok=true")
	}
	report, ok := body["report"].(map[string]any)
	if !ok {
		t.Fatal("report is not an object")
	}
	if report["indexed"].(float64) == 0 {
		t.Fatal("expected consolidation to index at least one memory entry")
	}
}

func TestTriggerReindex(t *testing.T) {
	store := testutil.TempStore(t)
	// G002 fix: use binary BLOB format (Rust parity), not JSON text.
	blob := db.EmbeddingToBlob([]float32{0.12, 0.34, 0.56})
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		db.NewID(), "semantic", db.NewID(), "launch playbook", blob, 3)
	if err != nil {
		t.Fatalf("seed embeddings: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/memory/reindex", nil)
	rec := httptest.NewRecorder()

	TriggerReindex(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := jsonBody(t, rec)
	if body["ok"] != true {
		t.Fatal("expected ok=true")
	}
	if body["entry_count"].(float64) != 1 {
		t.Fatalf("entry_count = %v, want 1", body["entry_count"])
	}
	if body["built"] != false {
		t.Fatalf("built = %v, want false for a single entry", body["built"])
	}
}

func TestGetRoutingDataset(t *testing.T) {
	store := testutil.TempStore(t)
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO model_selection_events
		 (id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model, complexity, user_excerpt, candidates_json, schema_version, created_at)
		 VALUES ('mse1', 'turn-1', 'session-1', 'agent-1', 'api', 'cloud-model', 'metascore', 'cloud-model', '0.8', 'customer asks for deep analysis', '[]', 2, datetime('now'))`)
	if err != nil {
		t.Fatalf("seed model_selection_events: %v", err)
	}
	_, err = store.ExecContext(bgCtx,
		`INSERT INTO inference_costs
		 (id, turn_id, model, provider, cost, tokens_in, tokens_out, cached, latency_ms, quality_score, escalation, created_at)
		 VALUES ('cost1', 'turn-1', 'cloud-model', 'openai', 0.07, 120, 240, 1, 320, 0.91, 1, datetime('now'))`)
	if err != nil {
		t.Fatalf("seed inference_costs: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/models/routing-dataset?limit=10", nil)
	rec := httptest.NewRecorder()

	GetRoutingDataset(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := jsonBody(t, rec)
	rows := body["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["user_excerpt"] != "[redacted]" {
		t.Fatalf("user_excerpt = %v, want redacted", row["user_excerpt"])
	}
	summary := body["summary"].(map[string]any)
	if summary["total_rows"].(float64) != 1 {
		t.Fatalf("summary.total_rows = %v, want 1", summary["total_rows"])
	}
}

func TestGetRoutingDatasetTSVRequiresUserExcerptOptIn(t *testing.T) {
	store := testutil.TempStore(t)
	req := httptest.NewRequest(http.MethodGet, "/api/models/routing-dataset?format=tsv", nil)
	rec := httptest.NewRecorder()

	GetRoutingDataset(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetMCPRuntimeAndServerCatalog(t *testing.T) {
	cfg := testConfig()
	cfg.MCP.Servers = []core.MCPServerEntry{
		{Name: "catalog-server", Transport: "stdio", Command: "cat", Enabled: true},
	}

	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/runtime/mcp", nil)
	runtimeRec := httptest.NewRecorder()
	GetMCPRuntime(cfg, nil).ServeHTTP(runtimeRec, runtimeReq)
	if runtimeRec.Code != http.StatusOK {
		t.Fatalf("runtime status = %d, want 200", runtimeRec.Code)
	}
	runtimeBody := jsonBody(t, runtimeRec)
	if runtimeBody["configured_servers"].(float64) != 1 {
		t.Fatalf("configured_servers = %v, want 1", runtimeBody["configured_servers"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mcp/servers", nil)
	listRec := httptest.NewRecorder()
	ListMCPServers(cfg, nil).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	listBody := jsonBody(t, listRec)
	servers := listBody["servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("server count = %d, want 1", len(servers))
	}
	server := servers[0].(map[string]any)
	if server["name"] != "catalog-server" {
		t.Fatalf("server name = %v, want catalog-server", server["name"])
	}
}

func TestGetMCPServerAndTestEndpoint(t *testing.T) {
	cfg := testConfig()
	cfg.MCP.Servers = []core.MCPServerEntry{
		{Name: "broken-stdio", Transport: "stdio", Command: "/definitely/missing-binary", Enabled: true},
	}

	router := chi.NewRouter()
	router.Get("/api/mcp/servers/{name}", GetMCPServer(cfg, nil))
	router.Post("/api/mcp/servers/{name}/test", TestMCPServer(cfg))

	getReq := httptest.NewRequest(http.MethodGet, "/api/mcp/servers/broken-stdio", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get server status = %d, want 200", getRec.Code)
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/mcp/servers/broken-stdio/test", nil)
	testRec := httptest.NewRecorder()
	router.ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test server status = %d, want 200", testRec.Code)
	}
	testBody := jsonBody(t, testRec)
	if testBody["ok"] != false {
		t.Fatalf("ok = %v, want false for broken stdio server", testBody["ok"])
	}
}

func TestListWorkspaceTasksAndTaskEvents(t *testing.T) {
	store := testutil.TempStore(t)
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO agent_tasks (id, phase, goal, created_at, updated_at)
		 VALUES ('task-1', 'running', 'ship parity', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("seed agent_tasks: %v", err)
	}
	_, err = store.ExecContext(bgCtx,
		`INSERT INTO agent_tasks (id, phase, parent_id, goal, created_at, updated_at)
		 VALUES ('task-1-sub', 'pending', 'task-1', 'write tests', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("seed subtask: %v", err)
	}
	_, err = store.ExecContext(bgCtx,
		`INSERT INTO task_events (id, task_id, parent_task_id, assigned_to, event_type, payload_json, created_at)
		 VALUES ('evt-1', 'task-1', '', 'orchestrator', 'running', '{"step":"implement"}', datetime('now'))`)
	if err != nil {
		t.Fatalf("seed task_events: %v", err)
	}

	taskReq := httptest.NewRequest(http.MethodGet, "/api/workspace/tasks", nil)
	taskRec := httptest.NewRecorder()
	ListWorkspaceTasks(store).ServeHTTP(taskRec, taskReq)
	if taskRec.Code != http.StatusOK {
		t.Fatalf("task status = %d, want 200", taskRec.Code)
	}
	taskBody := jsonBody(t, taskRec)
	tasks := taskBody["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(tasks))
	}

	eventReq := httptest.NewRequest(http.MethodGet, "/api/admin/task-events?task_id=task-1", nil)
	eventRec := httptest.NewRecorder()
	GetTaskEvents(store).ServeHTTP(eventRec, eventReq)
	if eventRec.Code != http.StatusOK {
		t.Fatalf("event status = %d, want 200", eventRec.Code)
	}
	eventBody := jsonBody(t, eventRec)
	events := eventBody["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].(map[string]any)["event_type"] != "running" {
		t.Fatalf("event_type = %v, want running", events[0].(map[string]any)["event_type"])
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
