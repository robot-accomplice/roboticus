package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/testutil"
)

// ─── Runtime Surfaces & Devices (static handlers) ───

func TestGetRuntimeSurfaces(t *testing.T) {
	handler := GetRuntimeSurfaces()
	req := httptest.NewRequest("GET", "/api/runtime/surfaces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	surfaces, ok := body["surfaces"].([]any)
	if !ok {
		t.Fatal("surfaces is not an array")
	}
	if len(surfaces) == 0 {
		t.Error("expected non-empty surfaces list")
	}
	// Verify first surface has expected fields.
	first := surfaces[0].(map[string]any)
	if first["name"] == nil || first["description"] == nil {
		t.Error("surface missing name or description")
	}
}

func TestGetRuntimeDevices(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetRuntimeDevices(store)
	req := httptest.NewRequest("GET", "/api/runtime/devices", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	devices, ok := body["devices"].([]any)
	if !ok {
		t.Fatal("devices is not an array")
	}
	if len(devices) != 0 {
		t.Errorf("expected empty devices, got %d", len(devices))
	}
}

// ─── Runtime Discovery ───

func TestGetRuntimeDiscovery_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetRuntimeDiscovery(store)
	req := httptest.NewRequest("GET", "/api/runtime/discovery", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	agents := body["agents"].([]any)
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestGetRuntimeDiscovery_WithData(t *testing.T) {
	store := testutil.TempStore(t)
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO discovered_agents (id, did, agent_card_json, capabilities, endpoint_url, trust_score, created_at)
		 VALUES ('da1', 'did:example:123', '{"name":"test-agent"}', 'chat,tool-use', 'https://agent.example.com', 0.8, datetime('now'))`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	handler := GetRuntimeDiscovery(store)
	req := httptest.NewRequest("GET", "/api/runtime/discovery", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	agents := body["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	agent := agents[0].(map[string]any)
	if agent["did"] != "did:example:123" {
		t.Errorf("did = %v", agent["did"])
	}
	if agent["endpoint_url"] != "https://agent.example.com" {
		t.Errorf("endpoint_url = %v", agent["endpoint_url"])
	}
	if agent["trust_score"].(float64) != 0.8 {
		t.Errorf("trust_score = %v", agent["trust_score"])
	}
	if agent["agent_card"] == nil {
		t.Error("expected parsed agent_card")
	}
}

func TestRegisterDiscoveredAgent_Success(t *testing.T) {
	store := testutil.TempStore(t)
	handler := RegisterDiscoveredAgent(store)
	req := httptest.NewRequest("POST", "/api/runtime/discovery",
		strings.NewReader(`{"did":"did:example:456","endpoint_url":"https://a.example.com","agent_card":{"name":"a"},"capabilities":"chat","trust_score":0.9}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["did"] != "did:example:456" {
		t.Errorf("did = %v", body["did"])
	}
	if body["id"] == nil || body["id"] == "" {
		t.Error("expected non-empty id")
	}
}

func TestRegisterDiscoveredAgent_DefaultTrustScore(t *testing.T) {
	store := testutil.TempStore(t)
	handler := RegisterDiscoveredAgent(store)
	req := httptest.NewRequest("POST", "/api/runtime/discovery",
		strings.NewReader(`{"did":"did:example:789","endpoint_url":"https://b.example.com","agent_card":{}}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	// Verify default trust_score of 0.5 was applied.
	listRec := httptest.NewRecorder()
	GetRuntimeDiscovery(store).ServeHTTP(listRec, httptest.NewRequest("GET", "/", nil))
	body := jsonBody(t, listRec)
	agents := body["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].(map[string]any)["trust_score"].(float64) != 0.5 {
		t.Errorf("trust_score = %v, want 0.5", agents[0].(map[string]any)["trust_score"])
	}
}

func TestVerifyDiscoveredAgent(t *testing.T) {
	store := testutil.TempStore(t)
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO discovered_agents (id, did, agent_card_json, endpoint_url)
		 VALUES ('da-verify', 'did:example:verify', '{}', 'https://agent.example.com')`)
	if err != nil {
		t.Fatalf("seed discovered_agents: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/api/runtime/discovery/{id}/verify", VerifyDiscoveredAgent(store))

	req := httptest.NewRequest(http.MethodPost, "/api/runtime/discovery/da-verify/verify", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRegisterDiscoveredAgent_MissingRequired(t *testing.T) {
	store := testutil.TempStore(t)
	handler := RegisterDiscoveredAgent(store)

	// Missing did.
	req := httptest.NewRequest("POST", "/api/runtime/discovery",
		strings.NewReader(`{"endpoint_url":"https://a.example.com"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	// Missing endpoint_url.
	req = httptest.NewRequest("POST", "/api/runtime/discovery",
		strings.NewReader(`{"did":"did:example:999"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing endpoint_url", rec.Code)
	}
}

func TestRegisterDiscoveredAgent_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	handler := RegisterDiscoveredAgent(store)
	req := httptest.NewRequest("POST", "/api/runtime/discovery",
		strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRuntimeDeviceLifecycle(t *testing.T) {
	store := testutil.TempStore(t)
	router := chi.NewRouter()
	router.Get("/api/runtime/devices", GetRuntimeDevices(store))
	router.Post("/api/runtime/devices/pair", PairRuntimeDevice(store))
	router.Post("/api/runtime/devices/{id}/verify", VerifyPairedDevice(store))
	router.Delete("/api/runtime/devices/{id}", UnpairDevice(store))

	pairReq := httptest.NewRequest(http.MethodPost, "/api/runtime/devices/pair",
		strings.NewReader(`{"device_id":"device-1","public_key_hex":"abcd1234","device_name":"Phone"}`))
	pairRec := httptest.NewRecorder()
	router.ServeHTTP(pairRec, pairReq)
	if pairRec.Code != http.StatusOK {
		t.Fatalf("pair status = %d, want 200", pairRec.Code)
	}

	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/runtime/devices", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	body := jsonBody(t, listRec)
	devices := body["devices"].([]any)
	if len(devices) != 1 {
		t.Fatalf("devices len = %d, want 1", len(devices))
	}
	if devices[0].(map[string]any)["state"] != "pending" {
		t.Fatalf("initial state = %v, want pending", devices[0].(map[string]any)["state"])
	}

	verifyRec := httptest.NewRecorder()
	router.ServeHTTP(verifyRec, httptest.NewRequest(http.MethodPost, "/api/runtime/devices/device-1/verify", nil))
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200", verifyRec.Code)
	}

	listRec = httptest.NewRecorder()
	router.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/runtime/devices", nil))
	body = jsonBody(t, listRec)
	devices = body["devices"].([]any)
	if devices[0].(map[string]any)["state"] != "verified" {
		t.Fatalf("verified state = %v, want verified", devices[0].(map[string]any)["state"])
	}

	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, httptest.NewRequest(http.MethodDelete, "/api/runtime/devices/device-1", nil))
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", deleteRec.Code)
	}
}

// ─── Roster (sub_agents) ───

func TestGetRoster_DefaultAgent(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetRoster(store, testConfig())
	req := httptest.NewRequest("GET", "/api/roster", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	agents := body["roster"].([]any)
	// Primary agent is always first, even with no subagents.
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent (primary), got %d", len(agents))
	}
	first := agents[0].(map[string]any)
	if first["role"] != "orchestrator" {
		t.Errorf("role = %v, want orchestrator", first["role"])
	}
}

func TestGetRoster_WithSeededAgents(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, enabled, role) VALUES ('sa1', 'researcher', 'gpt-4', 1, 'specialist')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, enabled, role) VALUES ('sa2', 'coder', 'claude-3', 0, 'specialist')`)

	handler := GetRoster(store, testConfig())
	req := httptest.NewRequest("GET", "/api/roster", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	agents := body["roster"].([]any)
	// 1 primary + 2 seeded subagents.
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}
}

func TestUpdateRosterModel_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, enabled, role) VALUES ('sa1', 'researcher', 'gpt-4', 1, 'specialist')`)

	r := chi.NewRouter()
	r.Put("/roster/{agent}/model", UpdateRosterModel(store))

	req := httptest.NewRequest("PUT", "/roster/researcher/model",
		strings.NewReader(`{"model":"claude-3"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUpdateRosterModel_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Put("/roster/{agent}/model", UpdateRosterModel(store))

	req := httptest.NewRequest("PUT", "/roster/nonexistent/model",
		strings.NewReader(`{"model":"claude-3"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateSubagent_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, enabled, role) VALUES ('sa1', 'researcher', 'gpt-4', 1, 'specialist')`)

	r := chi.NewRouter()
	r.Put("/subagents/{name}", UpdateSubagent(store))

	req := httptest.NewRequest("PUT", "/subagents/researcher",
		strings.NewReader(`{"model":"claude-3","description":"updated desc"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "updated" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestUpdateSubagent_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	r := chi.NewRouter()
	r.Put("/subagents/{name}", UpdateSubagent(store))

	req := httptest.NewRequest("PUT", "/subagents/ghost",
		strings.NewReader(`{"model":"x","description":"y"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestToggleSubagent_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, enabled, role) VALUES ('sa1', 'researcher', 'gpt-4', 1, 'specialist')`)

	r := chi.NewRouter()
	r.Post("/subagents/{name}/toggle", ToggleSubagent(store))

	req := httptest.NewRequest("POST", "/subagents/researcher/toggle", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "toggled" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestToggleSubagent_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	r := chi.NewRouter()
	r.Post("/subagents/{name}/toggle", ToggleSubagent(store))

	req := httptest.NewRequest("POST", "/subagents/ghost/toggle", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteSubagent_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, enabled, role) VALUES ('sa1', 'researcher', 'gpt-4', 1, 'specialist')`)

	r := chi.NewRouter()
	r.Delete("/subagents/{name}", DeleteSubagent(store))

	req := httptest.NewRequest("DELETE", "/subagents/researcher", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "deleted" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestDeleteSubagent_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	r := chi.NewRouter()
	r.Delete("/subagents/{name}", DeleteSubagent(store))

	req := httptest.NewRequest("DELETE", "/subagents/ghost", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ─── Session Detail ───

func TestListSessionTurns_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)

	r := chi.NewRouter()
	r.Get("/sessions/{id}/turns", ListSessionTurns(store))

	req := httptest.NewRequest("GET", "/sessions/s1/turns", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	turns := body["turns"].([]any)
	if len(turns) != 0 {
		t.Errorf("expected 0 turns, got %d", len(turns))
	}
}

func TestListSessionTurns_WithMessages(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO session_messages (id, session_id, role, content, created_at)
		 VALUES ('m1', 's1', 'user', 'hello', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO session_messages (id, session_id, role, content, created_at)
		 VALUES ('m2', 's1', 'assistant', 'hi there', datetime('now'))`)

	r := chi.NewRouter()
	r.Get("/sessions/{id}/turns", ListSessionTurns(store))

	req := httptest.NewRequest("GET", "/sessions/s1/turns", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	turns := body["turns"].([]any)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	first := turns[0].(map[string]any)
	if first["role"] != "user" {
		t.Errorf("first turn role = %v, want user", first["role"])
	}
}

func TestGetSessionFeedback_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)

	r := chi.NewRouter()
	r.Get("/sessions/{id}/feedback", GetSessionFeedback(store))

	req := httptest.NewRequest("GET", "/sessions/s1/feedback", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	fb := body["feedback"].([]any)
	if len(fb) != 0 {
		t.Errorf("expected 0 feedback, got %d", len(fb))
	}
}

func TestGetSessionFeedback_WithData(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turn_feedback (id, turn_id, session_id, grade, source, comment, created_at)
		 VALUES ('fb1', 't1', 's1', 4, 'dashboard', 'good response', datetime('now'))`)

	r := chi.NewRouter()
	r.Get("/sessions/{id}/feedback", GetSessionFeedback(store))

	req := httptest.NewRequest("GET", "/sessions/s1/feedback", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	fb := body["feedback"].([]any)
	if len(fb) != 1 {
		t.Fatalf("expected 1 feedback, got %d", len(fb))
	}
	first := fb[0].(map[string]any)
	if first["grade"].(float64) != 4 {
		t.Errorf("grade = %v, want 4", first["grade"])
	}
	if first["comment"] != "good response" {
		t.Errorf("comment = %v", first["comment"])
	}
}

func TestGetSessionInsights_Detailed(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turns (id, session_id, tokens_in, tokens_out, cost) VALUES ('t1', 's1', 100, 50, 0.01)`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turns (id, session_id, tokens_in, tokens_out, cost) VALUES ('t2', 's1', 200, 100, 0.02)`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO session_messages (id, session_id, role, content) VALUES ('m1', 's1', 'user', 'hello')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, status) VALUES ('tc1', 't1', 'search', '{}', 'success')`)

	r := chi.NewRouter()
	r.Get("/sessions/{id}/insights", GetSessionInsights(store))

	req := httptest.NewRequest("GET", "/sessions/s1/insights", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	insights, ok := body["insights"].([]any)
	if !ok {
		t.Fatal("insights is not an array")
	}
	// The handler returns insights as an array of {severity, message, suggestion} objects.
	// Verify we get at least the base metrics (turn_count, message_count, total_tokens, etc.).
	if len(insights) < 5 {
		t.Errorf("expected at least 5 insight entries, got %d", len(insights))
	}
}

func TestGetSessionInsights_ZeroTurns(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)

	r := chi.NewRouter()
	r.Get("/sessions/{id}/insights", GetSessionInsights(store))

	req := httptest.NewRequest("GET", "/sessions/s1/insights", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	insights, ok := body["insights"].([]any)
	if !ok {
		t.Fatal("insights is not an array")
	}
	// With zero turns we should still get the base metric entries.
	if len(insights) < 5 {
		t.Errorf("expected at least 5 insight entries, got %d", len(insights))
	}
}

func TestDeleteSession_CascadesMessages(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO session_messages (id, session_id, role, content) VALUES ('m1', 's1', 'user', 'hello')`)

	r := chi.NewRouter()
	r.Delete("/sessions/{id}", DeleteSession(store))

	req := httptest.NewRequest("DELETE", "/sessions/s1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "deleted" {
		t.Errorf("status = %v", body["status"])
	}

	// Verify session and messages are gone.
	var count int64
	row := store.QueryRowContext(bgCtx, `SELECT COUNT(*) FROM sessions WHERE id = 's1'`)
	_ = row.Scan(&count)
	if count != 0 {
		t.Errorf("session still exists after delete")
	}
	row = store.QueryRowContext(bgCtx, `SELECT COUNT(*) FROM session_messages WHERE session_id = 's1'`)
	_ = row.Scan(&count)
	if count != 0 {
		t.Errorf("session_messages still exist after delete")
	}
}

// TestDeleteSession_CascadesAllChildren verifies that deleting a session
// removes all dependent records (turns, tool_calls, pipeline_traces, etc.)
// without triggering FK constraint violations.
func TestDeleteSession_CascadesAllChildren(t *testing.T) {
	store := testutil.TempStore(t)
	sid := "cascade-test-session"

	// Create session with full dependency chain:
	// session → messages, turns → tool_calls, pipeline_traces → react_traces
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, 'agent1', 'test')`, sid)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO session_messages (id, session_id, role, content) VALUES ('cm1', ?, 'user', 'hello')`, sid)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turns (id, session_id) VALUES ('ct1', ?)`, sid)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, status) VALUES ('ctc1', 'ct1', 'search', '{}', 'success')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json) VALUES ('cpt1', 'ct1', ?, 'test', 100, '[]')`, sid)

	r := chi.NewRouter()
	r.Delete("/sessions/{id}", DeleteSession(store))

	req := httptest.NewRequest("DELETE", "/sessions/"+sid, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// Verify all records are gone.
	tables := []string{"sessions", "session_messages", "turns", "tool_calls", "pipeline_traces"}
	for _, table := range tables {
		var count int64
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE session_id = ?`, table)
		if table == "tool_calls" {
			query = `SELECT COUNT(*) FROM tool_calls WHERE turn_id = 'ct1'`
		}
		row := store.QueryRowContext(bgCtx, query, sid)
		if table == "tool_calls" {
			row = store.QueryRowContext(bgCtx, query)
		}
		_ = row.Scan(&count)
		if count != 0 {
			t.Errorf("%s: %d rows remain after cascade delete", table, count)
		}
	}
}

// ─── Skills ───

func seedSkill(t *testing.T, store *db.Store, id, name string, enabled int) {
	t.Helper()
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO skills (id, name, kind, source_path, content_hash, enabled, version, risk_level, created_at)
		 VALUES (?, ?, 'instruction', '/tmp/test.md', 'abc123', ?, '1.0.0', 'Safe', datetime('now'))`,
		id, name, enabled)
	if err != nil {
		t.Fatalf("seedSkill: %v", err)
	}
}

func TestDeleteSkill_Success(t *testing.T) {
	store := testutil.TempStore(t)
	seedSkill(t, store, "sk1", "test-skill", 1)

	r := chi.NewRouter()
	r.Delete("/skills/{id}", DeleteSkill(store))

	req := httptest.NewRequest("DELETE", "/skills/sk1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestDeleteSkill_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Delete("/skills/{id}", DeleteSkill(store))

	req := httptest.NewRequest("DELETE", "/skills/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestToggleSkill_Success(t *testing.T) {
	store := testutil.TempStore(t)
	seedSkill(t, store, "sk1", "test-skill", 1)

	r := chi.NewRouter()
	r.Post("/skills/{id}/toggle", ToggleSkill(store))

	req := httptest.NewRequest("POST", "/skills/sk1/toggle", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Verify it was toggled to disabled.
	var enabled int
	row := store.QueryRowContext(bgCtx, `SELECT enabled FROM skills WHERE id = 'sk1'`)
	_ = row.Scan(&enabled)
	if enabled != 0 {
		t.Errorf("enabled = %d, want 0 after toggle", enabled)
	}
}

func TestToggleSkill_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Post("/skills/{id}/toggle", ToggleSkill(store))

	req := httptest.NewRequest("POST", "/skills/ghost/toggle", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetSkill_Success(t *testing.T) {
	store := testutil.TempStore(t)
	seedSkill(t, store, "sk1", "my-skill", 1)

	r := chi.NewRouter()
	r.Get("/skills/{id}", GetSkill(store))

	req := httptest.NewRequest("GET", "/skills/sk1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["name"] != "my-skill" {
		t.Errorf("name = %v", body["name"])
	}
	if body["kind"] != "instruction" {
		t.Errorf("kind = %v", body["kind"])
	}
	if body["enabled"] != true {
		t.Errorf("enabled = %v", body["enabled"])
	}
}

func TestGetSkill_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Get("/skills/{id}", GetSkill(store))

	req := httptest.NewRequest("GET", "/skills/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateSkill_Success(t *testing.T) {
	store := testutil.TempStore(t)
	seedSkill(t, store, "sk1", "my-skill", 1)

	r := chi.NewRouter()
	r.Put("/skills/{id}", UpdateSkill(store))

	req := httptest.NewRequest("PUT", "/skills/sk1",
		strings.NewReader(`{"description":"new desc","risk_level":"Caution","version":"2.0.0"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "updated" {
		t.Errorf("status = %v", body["status"])
	}

	// Verify fields were updated.
	var desc, risk, version string
	row := store.QueryRowContext(bgCtx, `SELECT description, risk_level, version FROM skills WHERE id = 'sk1'`)
	_ = row.Scan(&desc, &risk, &version)
	if desc != "new desc" {
		t.Errorf("description = %v", desc)
	}
	if risk != "Caution" {
		t.Errorf("risk_level = %v", risk)
	}
	if version != "2.0.0" {
		t.Errorf("version = %v", version)
	}
}

func TestUpdateSkill_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Put("/skills/{id}", UpdateSkill(store))

	req := httptest.NewRequest("PUT", "/skills/sk1", strings.NewReader(`bad json`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAuditSkills(t *testing.T) {
	store := testutil.TempStore(t)
	seedSkill(t, store, "sk1", "skill-a", 1)
	seedSkill(t, store, "sk2", "skill-b", 1)
	seedSkill(t, store, "sk3", "skill-c", 0)

	handler := AuditSkills(store)
	req := httptest.NewRequest("GET", "/api/skills/audit", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["total_count"].(float64) != 3 {
		t.Errorf("total_count = %v, want 3", body["total_count"])
	}
	if body["active_count"].(float64) != 2 {
		t.Errorf("active_count = %v, want 2", body["active_count"])
	}
	if body["disabled_count"].(float64) != 1 {
		t.Errorf("disabled_count = %v, want 1", body["disabled_count"])
	}
}

func TestAuditSkills_Empty(t *testing.T) {
	store := testutil.TempStore(t)

	handler := AuditSkills(store)
	req := httptest.NewRequest("GET", "/api/skills/audit", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["total_count"].(float64) != 0 {
		t.Errorf("total_count = %v, want 0", body["total_count"])
	}
}

func TestActivateSkillFromCatalog_Success(t *testing.T) {
	store := testutil.TempStore(t)
	seedSkill(t, store, "sk1", "my-skill", 0)

	handler := ActivateSkillFromCatalog(store)
	req := httptest.NewRequest("POST", "/api/skills/catalog/activate",
		strings.NewReader(`{"name":"my-skill"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "activated" {
		t.Errorf("status = %v", body["status"])
	}

	// Verify enabled.
	var enabled int
	row := store.QueryRowContext(bgCtx, `SELECT enabled FROM skills WHERE name = 'my-skill'`)
	_ = row.Scan(&enabled)
	if enabled != 1 {
		t.Errorf("enabled = %d, want 1 after activation", enabled)
	}
}

func TestActivateSkillFromCatalog_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	handler := ActivateSkillFromCatalog(store)
	req := httptest.NewRequest("POST", "/api/skills/catalog/activate",
		strings.NewReader(`{"name":"nonexistent"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestActivateSkillFromCatalog_EmptyName(t *testing.T) {
	store := testutil.TempStore(t)

	handler := ActivateSkillFromCatalog(store)
	req := httptest.NewRequest("POST", "/api/skills/catalog/activate",
		strings.NewReader(`{"name":""}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestInstallSkillFromCatalog_MissingFields(t *testing.T) {
	store := testutil.TempStore(t)
	cfg := defaultTestConfig()

	handler := InstallSkillFromCatalog(cfg, store)

	// Missing name.
	req := httptest.NewRequest("POST", "/api/skills/catalog/install",
		strings.NewReader(`{"content":"# Test"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing name", rec.Code)
	}

	// Missing content.
	req = httptest.NewRequest("POST", "/api/skills/catalog/install",
		strings.NewReader(`{"name":"test"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing content", rec.Code)
	}
}

func TestInstallSkillFromCatalog_Success(t *testing.T) {
	store := testutil.TempStore(t)
	cfg := defaultTestConfig()
	cfg.Skills.Directory = t.TempDir()

	handler := InstallSkillFromCatalog(cfg, store)
	req := httptest.NewRequest("POST", "/api/skills/catalog/install",
		strings.NewReader(`{"name":"greeting","content":"# Greeting Skill\nSay hello."}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["name"] != "greeting" {
		t.Errorf("name = %v", body["name"])
	}
	if body["status"] != "installed" {
		t.Errorf("status = %v", body["status"])
	}
}

// ─── DeleteProviderKey ───

func TestDeleteProviderKey_NilKeystore(t *testing.T) {
	r := chi.NewRouter()
	r.Delete("/providers/{provider}/key", DeleteProviderKey(nil))

	req := httptest.NewRequest("DELETE", "/providers/openai/key", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// ─── Helpers ───

func defaultTestConfig() *core.Config {
	cfg := core.DefaultConfig()
	return &cfg
}
