package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/testutil"
)

func TestListSkills(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO skills (id, name, kind, source_path, content_hash, enabled, version, risk_level)
		 VALUES ('sk1', 'test-skill', 'structured', '/path', 'abc', 1, '1.0.0', 'Safe')`)

	handler := ListSkills(store)
	req := httptest.NewRequest("GET", "/api/skills", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	skills := body["skills"].([]any)
	if len(skills) != 1 {
		t.Errorf("got %d skills, want 1", len(skills))
	}
}

func TestGetCosts(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost) VALUES ('c1', 'gpt-4', 'openai', 100, 50, 0.01)`)

	handler := GetCosts(store)
	req := httptest.NewRequest("GET", "/api/stats/costs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["requests"].(float64) != 1 {
		t.Errorf("requests = %v, want 1", body["requests"])
	}
}

func TestGetCacheStats(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO semantic_cache (id, prompt_hash, response, model) VALUES ('sc1', 'h1', 'resp', 'gpt-4')`)

	handler := GetCacheStats(store)
	req := httptest.NewRequest("GET", "/api/stats/cache", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["cached_entries"].(float64) != 1 {
		t.Errorf("cached_entries = %v, want 1", body["cached_entries"])
	}
}

func TestPostTurnFeedback_InvalidGrade(t *testing.T) {
	store := testutil.TempStore(t)
	handler := PostTurnFeedback(store)
	body := `{"grade": 6}`
	req := httptest.NewRequest("POST", "/api/turns/t1/feedback", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGetTurnFeedback_ReturnsStoredFeedback(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'scope')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turn_feedback (id, turn_id, session_id, grade, source, comment) VALUES ('f1', 't1', 's1', 5, 'user', 'great')`)

	handler := GetTurnFeedback(store)
	req := httptest.NewRequest("GET", "/api/turns/t1/feedback", nil)
	rec := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Get("/api/turns/{id}/feedback", handler)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	feedback := body["feedback"].(map[string]any)
	if feedback["grade"].(float64) != 5 {
		t.Fatalf("grade = %v, want 5", feedback["grade"])
	}
}

func TestListSubagents(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, role, enabled) VALUES ('sa1', 'coder', 'gpt-4', 'specialist', 1)`)

	handler := ListSubagents(store)
	req := httptest.NewRequest("GET", "/api/subagents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	agents := body["subagents"].([]any)
	if len(agents) != 1 {
		t.Errorf("got %d subagents, want 1", len(agents))
	}
}

func TestGetDeadLetters(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO delivery_queue (id, channel, recipient_id, content, status) VALUES ('dq1', 'telegram', 'u1', 'msg', 'dead_letter')`)

	handler := GetDeadLetters(store)
	req := httptest.NewRequest("GET", "/api/channels/dead-letter", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	letters := body["dead_letters"].([]any)
	if len(letters) != 1 {
		t.Errorf("got %d dead letters, want 1", len(letters))
	}
}

func TestGetMemoryAnalytics(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO context_snapshots (turn_id, complexity_level, token_budget, memory_tokens, system_prompt_tokens, history_tokens)
		 VALUES ('t1', 'L1', 8000, 500, 200, 1000)`)

	handler := GetMemoryAnalytics(store)
	req := httptest.NewRequest("GET", "/api/stats/memory-analytics?hours=24", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["total_turns"].(float64) != 1 {
		t.Errorf("total_turns = %v, want 1", body["total_turns"])
	}
}

func TestListSkills_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE skills`); err != nil {
		t.Fatalf("drop skills: %v", err)
	}

	handler := ListSkills(store)
	req := httptest.NewRequest("GET", "/api/skills", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestGetDeadLetters_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE delivery_queue`); err != nil {
		t.Fatalf("drop delivery_queue: %v", err)
	}

	handler := GetDeadLetters(store)
	req := httptest.NewRequest("GET", "/api/channels/dead-letter", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
