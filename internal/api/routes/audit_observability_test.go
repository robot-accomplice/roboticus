package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/testutil"
)

// ---------------------------------------------------------------------------
// audit.go — GetPolicyAudit
// ---------------------------------------------------------------------------

func TestGetPolicyAudit_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)

	// Seed parent rows for FK chain.
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)

	// Seed policy decisions.
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO policy_decisions (id, turn_id, tool_name, decision, rule_name, reason, created_at)
		 VALUES ('pd1', 't1', 'shell', 'deny', 'no_shell', 'blocked by policy', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO policy_decisions (id, turn_id, tool_name, decision, created_at)
		 VALUES ('pd2', 't1', 'read_file', 'allow', datetime('now'))`)

	r := chi.NewRouter()
	r.Get("/api/audit/policy/{turn_id}", GetPolicyAudit(store))

	req := httptest.NewRequest("GET", "/api/audit/policy/t1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	decisions, ok := body["decisions"].([]any)
	if !ok {
		t.Fatal("decisions is not an array")
	}
	if len(decisions) != 2 {
		t.Fatalf("got %d decisions, want 2", len(decisions))
	}

	// First row should have rule_name and reason populated.
	first := decisions[0].(map[string]any)
	if first["decision"] != "deny" {
		t.Errorf("first decision = %v, want deny", first["decision"])
	}
	if first["rule_name"] != "no_shell" {
		t.Errorf("rule_name = %v, want no_shell", first["rule_name"])
	}
	if first["reason"] != "blocked by policy" {
		t.Errorf("reason = %v, want blocked by policy", first["reason"])
	}

	// Second row has nullable rule_name/reason omitted.
	second := decisions[1].(map[string]any)
	if second["decision"] != "allow" {
		t.Errorf("second decision = %v, want allow", second["decision"])
	}
	if _, exists := second["rule_name"]; exists {
		t.Error("expected rule_name to be absent for allow decision")
	}
}

func TestGetPolicyAudit_EmptyResult(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Get("/api/audit/policy/{turn_id}", GetPolicyAudit(store))

	req := httptest.NewRequest("GET", "/api/audit/policy/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	decisions := body["decisions"].([]any)
	if len(decisions) != 0 {
		t.Errorf("got %d decisions, want 0", len(decisions))
	}
}

func TestGetPolicyAudit_QueryFailure(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE policy_decisions`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/api/audit/policy/{turn_id}", GetPolicyAudit(store))

	req := httptest.NewRequest("GET", "/api/audit/policy/t1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// audit.go — GetToolAudit
// ---------------------------------------------------------------------------

func TestGetToolAudit_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)

	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)

	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, output, status, duration_ms, created_at)
		 VALUES ('tc1', 't1', 'read_file', '{"path":"/tmp/x"}', '{"content":"hi"}', 'success', 42, datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, status, created_at)
		 VALUES ('tc2', 't1', 'write_file', '{"path":"/tmp/y"}', 'error', datetime('now'))`)

	r := chi.NewRouter()
	r.Get("/api/audit/tools/{turn_id}", GetToolAudit(store))

	req := httptest.NewRequest("GET", "/api/audit/tools/t1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	calls, ok := body["tool_calls"].([]any)
	if !ok {
		t.Fatal("tool_calls is not an array")
	}
	if len(calls) != 2 {
		t.Fatalf("got %d tool_calls, want 2", len(calls))
	}

	first := calls[0].(map[string]any)
	if first["tool_name"] != "read_file" {
		t.Errorf("tool_name = %v, want read_file", first["tool_name"])
	}
	if first["output"] != "{\"content\":\"hi\"}" {
		t.Errorf("output = %v", first["output"])
	}
	if first["duration_ms"].(float64) != 42 {
		t.Errorf("duration_ms = %v, want 42", first["duration_ms"])
	}

	// Second row has nullable output/duration_ms omitted.
	second := calls[1].(map[string]any)
	if _, exists := second["output"]; exists {
		t.Error("expected output to be absent for error call")
	}
	if _, exists := second["duration_ms"]; exists {
		t.Error("expected duration_ms to be absent for error call")
	}
}

func TestGetToolAudit_EmptyResult(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Get("/api/audit/tools/{turn_id}", GetToolAudit(store))

	req := httptest.NewRequest("GET", "/api/audit/tools/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	calls := body["tool_calls"].([]any)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0", len(calls))
	}
}

func TestGetToolAudit_QueryFailure(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE tool_calls`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/api/audit/tools/{turn_id}", GetToolAudit(store))

	req := httptest.NewRequest("GET", "/api/audit/tools/t1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// observability.go — ListObservabilityTraces
// ---------------------------------------------------------------------------

func TestListObservabilityTraces_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)

	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, created_at)
		 VALUES ('pt1', 't1', 's1', 'api', 200, '[]', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, created_at)
		 VALUES ('pt2', 't1', 's1', 'discord', 350, '[]', datetime('now'))`)

	handler := ListObservabilityTraces(store)
	req := httptest.NewRequest("GET", "/api/observability/traces?limit=10&offset=0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	traces := body["traces"].([]any)
	if len(traces) != 2 {
		t.Fatalf("got %d traces, want 2", len(traces))
	}
	if body["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", body["total"])
	}
	if body["limit"].(float64) != 10 {
		t.Errorf("limit = %v, want 10", body["limit"])
	}
	if body["offset"].(float64) != 0 {
		t.Errorf("offset = %v, want 0", body["offset"])
	}
}

func TestListObservabilityTraces_Pagination(t *testing.T) {
	store := testutil.TempStore(t)

	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, created_at)
		 VALUES ('pt1', 't1', 's1', 'api', 100, '[]', datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, created_at)
		 VALUES ('pt2', 't1', 's1', 'api', 200, '[]', datetime('now','+1 second'))`)

	handler := ListObservabilityTraces(store)
	req := httptest.NewRequest("GET", "/api/observability/traces?limit=1&offset=0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	traces := body["traces"].([]any)
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1 (pagination)", len(traces))
	}
	// total should still reflect all rows.
	if body["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", body["total"])
	}
}

func TestListObservabilityTraces_QueryFailure(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE pipeline_traces`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	handler := ListObservabilityTraces(store)
	req := httptest.NewRequest("GET", "/api/observability/traces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// observability.go — TraceWaterfall
// ---------------------------------------------------------------------------

func TestTraceWaterfall_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)

	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, created_at)
		 VALUES ('pt1', 't1', 's1', 'api', 250, '[{"name":"llm","ms":200}]', datetime('now'))`)

	r := chi.NewRouter()
	r.Get("/api/observability/traces/{id}/waterfall", TraceWaterfall(store))

	req := httptest.NewRequest("GET", "/api/observability/traces/pt1/waterfall", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["id"] != "pt1" {
		t.Errorf("id = %v, want pt1", body["id"])
	}
	if body["total_ms"].(float64) != 250 {
		t.Errorf("total_ms = %v, want 250", body["total_ms"])
	}
	if body["format"] != "waterfall" {
		t.Errorf("format = %v, want waterfall", body["format"])
	}

	// stages should be parsed JSON, not a string.
	stages, ok := body["stages"].([]any)
	if !ok {
		t.Fatal("stages is not an array")
	}
	if len(stages) != 1 {
		t.Fatalf("got %d stages, want 1", len(stages))
	}
}

func TestTraceWaterfall_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)

	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, created_at)
		 VALUES ('pt1', 't1', 's1', 'api', 100, 'not-json', datetime('now'))`)

	r := chi.NewRouter()
	r.Get("/api/observability/traces/{id}/waterfall", TraceWaterfall(store))

	req := httptest.NewRequest("GET", "/api/observability/traces/pt1/waterfall", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (invalid JSON falls back to empty array)", rec.Code)
	}
	body := jsonBody(t, rec)
	stages, ok := body["stages"].([]any)
	if !ok {
		t.Fatal("stages should be an empty array on invalid JSON")
	}
	if len(stages) != 0 {
		t.Errorf("got %d stages, want 0", len(stages))
	}
}

func TestTraceWaterfall_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chi.NewRouter()
	r.Get("/api/observability/traces/{id}/waterfall", TraceWaterfall(store))

	req := httptest.NewRequest("GET", "/api/observability/traces/nonexistent/waterfall", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// observability.go — DelegationOutcomes
// ---------------------------------------------------------------------------

func TestDelegationOutcomes_HappyPath(t *testing.T) {
	store := testutil.TempStore(t)

	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO delegation_outcomes (id, turn_id, session_id, task_description, subtask_count, pattern, duration_ms, success, quality_score, created_at)
		 VALUES ('do1', 't1', 's1', 'summarise doc', 3, 'fan-out', 1200, 1, 0.95, datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO delegation_outcomes (id, turn_id, session_id, task_description, subtask_count, pattern, duration_ms, success, created_at)
		 VALUES ('do2', 't1', 's1', 'failed task', 1, 'direct', 500, 0, datetime('now'))`)

	handler := DelegationOutcomes(store)
	req := httptest.NewRequest("GET", "/api/observability/delegations?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	outcomes := body["outcomes"].([]any)
	if len(outcomes) != 2 {
		t.Fatalf("got %d outcomes, want 2", len(outcomes))
	}

	// Check first outcome (most recent first due to ORDER BY DESC).
	first := outcomes[0].(map[string]any)
	if first["success"] != true && first["success"] != false {
		t.Errorf("success should be bool, got %T", first["success"])
	}

	// Verify quality_score is present on the row that has it and absent on the other.
	var withScore, withoutScore map[string]any
	for _, o := range outcomes {
		m := o.(map[string]any)
		if m["id"] == "do1" {
			withScore = m
		} else {
			withoutScore = m
		}
	}
	if withScore["quality_score"].(float64) != 0.95 {
		t.Errorf("quality_score = %v, want 0.95", withScore["quality_score"])
	}
	if _, exists := withoutScore["quality_score"]; exists {
		t.Error("expected quality_score to be absent when NULL")
	}
}

func TestDelegationOutcomes_EmptyResult(t *testing.T) {
	store := testutil.TempStore(t)

	handler := DelegationOutcomes(store)
	req := httptest.NewRequest("GET", "/api/observability/delegations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	outcomes := body["outcomes"].([]any)
	if len(outcomes) != 0 {
		t.Errorf("got %d outcomes, want 0", len(outcomes))
	}
}

func TestDelegationOutcomes_QueryFailure(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE delegation_outcomes`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	handler := DelegationOutcomes(store)
	req := httptest.NewRequest("GET", "/api/observability/delegations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// observability.go — DelegationStats
// ---------------------------------------------------------------------------

func TestDelegationStats_WithData(t *testing.T) {
	store := testutil.TempStore(t)

	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO delegation_outcomes (id, turn_id, session_id, task_description, subtask_count, pattern, duration_ms, success, quality_score, created_at)
		 VALUES ('do1', 't1', 's1', 'task a', 2, 'fan-out', 1000, 1, 0.9, datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO delegation_outcomes (id, turn_id, session_id, task_description, subtask_count, pattern, duration_ms, success, quality_score, created_at)
		 VALUES ('do2', 't1', 's1', 'task b', 1, 'direct', 500, 0, 0.5, datetime('now'))`)

	handler := DelegationStats(store)
	req := httptest.NewRequest("GET", "/api/observability/delegation-stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)

	if body["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", body["total"])
	}
	if body["successful"].(float64) != 1 {
		t.Errorf("successful = %v, want 1", body["successful"])
	}
	if body["success_rate"].(float64) != 0.5 {
		t.Errorf("success_rate = %v, want 0.5", body["success_rate"])
	}
	// avg_duration_ms = (1000 + 500) / 2 = 750.
	if body["avg_duration_ms"].(float64) != 750 {
		t.Errorf("avg_duration_ms = %v, want 750", body["avg_duration_ms"])
	}
	// avg_quality = (0.9 + 0.5) / 2 = 0.7.
	if body["avg_quality"].(float64) != 0.7 {
		t.Errorf("avg_quality = %v, want 0.7", body["avg_quality"])
	}
}

func TestDelegationStats_Empty(t *testing.T) {
	store := testutil.TempStore(t)

	handler := DelegationStats(store)
	req := httptest.NewRequest("GET", "/api/observability/delegation-stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["total"].(float64) != 0 {
		t.Errorf("total = %v, want 0", body["total"])
	}
	if body["success_rate"].(float64) != 0 {
		t.Errorf("success_rate = %v, want 0 when no data", body["success_rate"])
	}
}

func TestDelegationStats_QueryFailure(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE delegation_outcomes`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	handler := DelegationStats(store)
	req := httptest.NewRequest("GET", "/api/observability/delegation-stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
