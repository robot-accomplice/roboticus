package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/testutil"
)

// --- helpers to build chi-routed requests ---

func chiRequest(method, pattern, path string, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	switch method {
	case "GET":
		r.Get(pattern, handler)
	case "PUT":
		r.Put(pattern, handler)
	case "POST":
		r.Post(pattern, handler)
	}
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ===== traces.go handlers =====

func TestGetTrace_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, channel, total_ms, stages_json, created_at)
		 VALUES ('p1', 't1', 'api', 250, '[{"name":"classify","ms":50}]', datetime('now'))`)

	rec := chiRequest("GET", "/traces/{turn_id}", "/traces/t1", "", GetTrace(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["turn_id"] != "t1" {
		t.Errorf("turn_id = %v, want t1", body["turn_id"])
	}
	if body["total_ms"].(float64) != 250 {
		t.Errorf("total_ms = %v, want 250", body["total_ms"])
	}
	stages, ok := body["stages"].([]any)
	if !ok || len(stages) != 1 {
		t.Errorf("stages = %v, want 1-element array", body["stages"])
	}
}

func TestGetTrace_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/traces/{turn_id}", "/traces/nonexistent", "", GetTrace(store))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetTrace_InvalidStagesJSON(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, channel, total_ms, stages_json, created_at)
		 VALUES ('p1', 't1', 'api', 100, 'NOT-JSON', datetime('now'))`)

	rec := chiRequest("GET", "/traces/{turn_id}", "/traces/t1", "", GetTrace(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (bad JSON falls back to empty array)", rec.Code)
	}
	body := jsonBody(t, rec)
	stages, ok := body["stages"].([]any)
	if !ok || len(stages) != 0 {
		t.Errorf("stages = %v, want empty array for invalid JSON", body["stages"])
	}
}

func TestGetReactTrace_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, channel, total_ms, stages_json, created_at)
		 VALUES ('p1', 't1', 'api', 100, '[]', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO react_traces (id, pipeline_trace_id, react_json, created_at)
		 VALUES ('r1', 'p1', '{"steps":[{"thought":"test"}]}', datetime('now'))`)

	rec := chiRequest("GET", "/traces/{turn_id}/react", "/traces/t1/react", "", GetReactTrace(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["pipeline_trace_id"] != "p1" {
		t.Errorf("pipeline_trace_id = %v, want p1", body["pipeline_trace_id"])
	}
	rt, ok := body["react_trace"].(map[string]any)
	if !ok {
		t.Fatal("react_trace is not an object")
	}
	if rt["steps"] == nil {
		t.Error("react_trace.steps should not be nil")
	}
}

func TestGetReactTrace_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/traces/{turn_id}/react", "/traces/missing/react", "", GetReactTrace(store))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExportTrace_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, channel, total_ms, stages_json, created_at)
		 VALUES ('p1', 't1', 'api', 300, '[{"name":"llm","ms":200}]', datetime('now'))`)

	rec := chiRequest("GET", "/traces/{turn_id}/export", "/traces/t1/export", "", ExportTrace(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %v, want application/json", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "trace-t1.json") {
		t.Errorf("Content-Disposition = %v, want to contain trace-t1.json", cd)
	}
	body := jsonBody(t, rec)
	if body["turn_id"] != "t1" {
		t.Errorf("turn_id = %v, want t1", body["turn_id"])
	}
}

func TestExportTrace_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/traces/{turn_id}/export", "/traces/nope/export", "", ExportTrace(store))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetTraceFlow_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, channel, total_ms, stages_json, created_at)
		 VALUES ('p1', 't1', 'api', 400, '[]', datetime('now'))`)

	rec := chiRequest("GET", "/traces/{turn_id}/flow", "/traces/t1/flow", "", GetTraceFlow(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["format"] != "flow" {
		t.Errorf("format = %v, want flow", body["format"])
	}
	if body["total_ms"].(float64) != 400 {
		t.Errorf("total_ms = %v, want 400", body["total_ms"])
	}
}

func TestGetTraceFlow_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/traces/{turn_id}/flow", "/traces/nope/flow", "", GetTraceFlow(store))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ===== turn_detail.go handlers =====

func TestGetTurnContext_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO context_snapshots (turn_id, complexity_level, token_budget, system_prompt_tokens, memory_tokens, history_tokens, history_depth, model)
		 VALUES ('t1', 'L2', 8000, 500, 300, 1200, 10, 'gpt-4')`)

	rec := chiRequest("GET", "/turns/{id}/context", "/turns/t1/context", "", GetTurnContext(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["complexity_level"] != "L2" {
		t.Errorf("complexity_level = %v, want L2", body["complexity_level"])
	}
	if body["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", body["model"])
	}
	if body["system_tokens"].(float64) != 500 {
		t.Errorf("system_tokens = %v, want 500", body["system_tokens"])
	}
	if body["memory_tokens"].(float64) != 300 {
		t.Errorf("memory_tokens = %v, want 300", body["memory_tokens"])
	}
	if body["history_tokens"].(float64) != 1200 {
		t.Errorf("history_tokens = %v, want 1200", body["history_tokens"])
	}
	totalTokens := body["total_tokens"].(float64)
	if totalTokens != 2000 {
		t.Errorf("total_tokens = %v, want 2000 (500+300+1200)", totalTokens)
	}
	if body["max_tokens"].(float64) != 8000 {
		t.Errorf("max_tokens = %v, want 8000", body["max_tokens"])
	}
	if body["history_depth"].(float64) != 10 {
		t.Errorf("history_depth = %v, want 10", body["history_depth"])
	}
}

func TestGetTurnContext_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/turns/{id}/context", "/turns/missing/context", "", GetTurnContext(store))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPutTurnFeedback_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turn_feedback (id, turn_id, session_id, grade, comment)
		 VALUES ('f1', 't1', 's1', 3, 'ok')`)

	rec := chiRequest("PUT", "/turns/{id}/feedback", "/turns/t1/feedback",
		`{"grade":5,"comment":"excellent"}`, PutTurnFeedback(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "updated" {
		t.Errorf("status = %v, want updated", body["status"])
	}
	if body["turn_id"] != "t1" {
		t.Errorf("turn_id = %v, want t1", body["turn_id"])
	}
}

func TestPutTurnFeedback_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("PUT", "/turns/{id}/feedback", "/turns/t1/feedback",
		`not json`, PutTurnFeedback(store))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutTurnFeedback_GradeOutOfRange(t *testing.T) {
	store := testutil.TempStore(t)
	for _, body := range []string{
		`{"grade":0,"comment":""}`,
		`{"grade":6,"comment":""}`,
		`{"grade":-1,"comment":""}`,
	} {
		rec := chiRequest("PUT", "/turns/{id}/feedback", "/turns/t1/feedback", body, PutTurnFeedback(store))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestPutTurnFeedback_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	// No feedback row exists for t1.
	rec := chiRequest("PUT", "/turns/{id}/feedback", "/turns/t1/feedback",
		`{"grade":4,"comment":"good"}`, PutTurnFeedback(store))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetTurnTools_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, output, status, duration_ms, skill_name, created_at)
		 VALUES ('tc1', 't1', 'web_search', '{"q":"test"}', '{"results":[]}', 'success', 150, 'research', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, status, created_at)
		 VALUES ('tc2', 't1', 'calculator', '{"expr":"1+1"}', 'success', datetime('now'))`)

	rec := chiRequest("GET", "/turns/{id}/tools", "/turns/t1/tools", "", GetTurnTools(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	calls, ok := body["tool_calls"].([]any)
	if !ok {
		t.Fatal("tool_calls is not an array")
	}
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(calls))
	}
	first := calls[0].(map[string]any)
	if first["tool_name"] != "web_search" {
		t.Errorf("first tool_name = %v, want web_search", first["tool_name"])
	}
	if first["output"] != `{"results":[]}` {
		t.Errorf("first output = %v", first["output"])
	}
	if first["duration_ms"].(float64) != 150 {
		t.Errorf("first duration_ms = %v, want 150", first["duration_ms"])
	}
	if first["skill_name"] != "research" {
		t.Errorf("first skill_name = %v, want research", first["skill_name"])
	}
	// Second call has no output, duration_ms, or skill_name.
	second := calls[1].(map[string]any)
	if _, exists := second["output"]; exists {
		t.Error("second call should not have output key")
	}
	if _, exists := second["duration_ms"]; exists {
		t.Error("second call should not have duration_ms key")
	}
	if _, exists := second["skill_name"]; exists {
		t.Error("second call should not have skill_name key")
	}
}

func TestGetTurnTools_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/turns/{id}/tools", "/turns/notools/tools", "", GetTurnTools(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	calls := body["tool_calls"].([]any)
	if len(calls) != 0 {
		t.Errorf("got %d tool calls, want 0", len(calls))
	}
}

func TestGetTurnTips_HighTokens(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turns (id, session_id, tokens_in, tokens_out, cost) VALUES ('t1', 's1', 5000, 3000, 0.05)`)

	rec := chiRequest("GET", "/turns/{id}/tips", "/turns/t1/tips", "", GetTurnTips(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	tips := body["tips"].([]any)
	if len(tips) != 2 {
		t.Fatalf("got %d tips, want 2 (high input + high output)", len(tips))
	}
}

func TestGetTurnTips_LowTokens(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turns (id, session_id, tokens_in, tokens_out, cost) VALUES ('t1', 's1', 100, 50, 0.001)`)

	rec := chiRequest("GET", "/turns/{id}/tips", "/turns/t1/tips", "", GetTurnTips(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	tips := body["tips"].([]any)
	if len(tips) != 0 {
		t.Errorf("got %d tips, want 0 for low tokens", len(tips))
	}
}

func TestGetTurnTips_NoTurnData(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/turns/{id}/tips", "/turns/missing/tips", "", GetTurnTips(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (returns empty tips for missing turn)", rec.Code)
	}
	body := jsonBody(t, rec)
	tips := body["tips"].([]any)
	if len(tips) != 0 {
		t.Errorf("got %d tips, want 0", len(tips))
	}
}

func TestGetTurnModelSelection_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO model_selection_events
		 (id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model,
		  override_model, complexity, candidates_json, attribution, user_excerpt, created_at)
		 VALUES ('ms1', 't1', 's1', 'a1', 'api', 'gpt-4', 'heuristic', 'gpt-3.5',
		         'gpt-4', 'L2', '[{"model":"gpt-4","score":0.9}]', 'complexity_upgrade', 'hello world', datetime('now'))`)

	rec := chiRequest("GET", "/turns/{id}/model-selection", "/turns/t1/model-selection", "", GetTurnModelSelection(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["selected_model"] != "gpt-4" {
		t.Errorf("selected_model = %v, want gpt-4", body["selected_model"])
	}
	if body["strategy"] != "heuristic" {
		t.Errorf("strategy = %v, want heuristic", body["strategy"])
	}
	if body["primary_model"] != "gpt-3.5" {
		t.Errorf("primary_model = %v, want gpt-3.5", body["primary_model"])
	}
	if body["override_model"] != "gpt-4" {
		t.Errorf("override_model = %v, want gpt-4", body["override_model"])
	}
	if body["complexity"] != "L2" {
		t.Errorf("complexity = %v, want L2", body["complexity"])
	}
	if body["attribution"] != "complexity_upgrade" {
		t.Errorf("attribution = %v, want complexity_upgrade", body["attribution"])
	}
	candidates, ok := body["candidates"].([]any)
	if !ok || len(candidates) != 1 {
		t.Errorf("candidates = %v, want 1-element array", body["candidates"])
	}
}

func TestGetTurnModelSelection_MinimalFields(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO model_selection_events
		 (id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model, user_excerpt, candidates_json, created_at)
		 VALUES ('ms1', 't1', 's1', 'a1', 'api', 'gpt-4', 'heuristic', 'gpt-4', 'hi', '[]', datetime('now'))`)

	rec := chiRequest("GET", "/turns/{id}/model-selection", "/turns/t1/model-selection", "", GetTurnModelSelection(store))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	// Optional fields should be absent.
	if _, exists := body["override_model"]; exists {
		t.Error("override_model should not be present when NULL")
	}
	if _, exists := body["complexity"]; exists {
		t.Error("complexity should not be present when NULL")
	}
	if _, exists := body["attribution"]; exists {
		t.Error("attribution should not be present when NULL")
	}
}

func TestGetTurnModelSelection_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/turns/{id}/model-selection", "/turns/missing/model-selection", "", GetTurnModelSelection(store))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAnalyzeTurn_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turns (id, session_id, model, tokens_in, tokens_out, cost) VALUES ('t1', 's1', 'gpt-4', 1000, 500, 0.02)`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, status, created_at)
		 VALUES ('tc1', 't1', 'search', '{}', 'success', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, status, created_at)
		 VALUES ('tc2', 't1', 'calc', '{}', 'success', datetime('now'))`)

	rec := chiRequest("GET", "/turns/{id}/analyze", "/turns/t1/analyze", "", AnalyzeTurn(store, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "complete" {
		t.Errorf("status = %v, want complete", body["status"])
	}
	if body["turn_id"] != "t1" {
		t.Errorf("turn_id = %v, want t1", body["turn_id"])
	}
	tips, ok := body["heuristic_tips"].([]any)
	if !ok {
		t.Fatal("heuristic_tips is not an array")
	}
	// Should have at least analysis string
	analysis, ok := body["analysis"].(string)
	if !ok {
		t.Fatal("analysis should be a string summary")
	}
	_ = tips
	_ = analysis
}

func TestAnalyzeTurn_NoTurnData(t *testing.T) {
	store := testutil.TempStore(t)
	rec := chiRequest("GET", "/turns/{id}/analyze", "/turns/missing/analyze", "", AnalyzeTurn(store, nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAnalyzeTurn_NoToolCalls(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turns (id, session_id, model, tokens_in, tokens_out, cost) VALUES ('t1', 's1', 'gpt-4', 200, 100, 0.01)`)

	rec := chiRequest("GET", "/turns/{id}/analyze", "/turns/t1/analyze", "", AnalyzeTurn(store, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "complete" {
		t.Errorf("status = %v, want complete", body["status"])
	}
	// Tips may be empty array or nil — both are valid.
	if tips, ok := body["heuristic_tips"].([]any); ok {
		_ = tips
	}
}
