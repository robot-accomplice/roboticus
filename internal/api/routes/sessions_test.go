package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roboticus/testutil"
)

func TestListSessions(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key, status) VALUES ('s1', 'agent1', 'test', 'active')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key, status) VALUES ('s2', 'agent1', 'test2', 'active')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)

	handler := ListSessions(store)
	req := httptest.NewRequest("GET", "/api/sessions?agent_id=agent1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	sessions := body["sessions"].([]any)
	if len(sessions) < 1 {
		t.Error("should return at least 1 session")
	}
	var sawTurnCount bool
	for _, raw := range sessions {
		session := raw.(map[string]any)
		if session["id"] == "s1" {
			sawTurnCount = true
			if session["turn_count"] != float64(1) {
				t.Fatalf("turn_count = %v, want 1", session["turn_count"])
			}
		}
	}
	if !sawTurnCount {
		t.Fatal("expected seeded session with turn_count")
	}
}

func TestListSessionsIncludesContextInventory(t *testing.T) {
	store := testutil.TempStore(t)
	mustExec := func(query string) {
		t.Helper()
		if _, err := store.ExecContext(bgCtx, query); err != nil {
			t.Fatalf("seed query failed: %v\n%s", err, query)
		}
	}
	mustExec(`INSERT INTO sessions (id, agent_id, scope_key, status, created_at, updated_at) VALUES ('s1', 'agent1', 'test', 'active', '2026-04-28 10:00:00', '2026-04-28 10:01:00')`)
	mustExec(`INSERT INTO turns (id, session_id, tokens_in, tokens_out, cost, created_at) VALUES ('t1', 's1', 100, 50, 0.0123, '2026-04-28 10:02:00')`)
	mustExec(`INSERT INTO turns (id, session_id, tokens_in, tokens_out, cost, created_at) VALUES ('t2', 's1', 25, 75, 0.0042, '2026-04-28 10:03:00')`)
	mustExec(`INSERT INTO session_messages (id, session_id, role, content, created_at) VALUES ('m1', 's1', 'user', 'hello', '2026-04-28 10:04:00')`)
	mustExec(`INSERT INTO session_messages (id, session_id, role, content, created_at) VALUES ('m2', 's1', 'assistant', 'hi', '2026-04-28 10:05:00')`)
	mustExec(`INSERT INTO context_snapshots (turn_id, complexity_level, token_budget, created_at) VALUES ('t1', 'L1', 8000, '2026-04-28 10:06:00')`)
	mustExec(`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, created_at) VALUES ('pt1', 't1', 's1', 'api', 10, '[]', '2026-04-28 10:07:00')`)

	handler := ListSessions(store)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	sessions := body["sessions"].([]any)
	for _, raw := range sessions {
		session := raw.(map[string]any)
		if session["id"] != "s1" {
			continue
		}
		if session["turn_count"] != float64(2) {
			t.Fatalf("turn_count = %v, want 2", session["turn_count"])
		}
		if session["message_count"] != float64(2) {
			t.Fatalf("message_count = %v, want 2", session["message_count"])
		}
		if session["trace_count"] != float64(1) {
			t.Fatalf("trace_count = %v, want 1", session["trace_count"])
		}
		if session["snapshot_count"] != float64(1) {
			t.Fatalf("snapshot_count = %v, want 1", session["snapshot_count"])
		}
		if session["total_tokens"] != float64(250) {
			t.Fatalf("total_tokens = %v, want 250", session["total_tokens"])
		}
		if session["total_cost"] == float64(0) {
			t.Fatal("total_cost should be non-zero")
		}
		if session["last_activity_at"] != "2026-04-28 10:07:00" {
			t.Fatalf("last_activity_at = %v, want trace timestamp", session["last_activity_at"])
		}
		return
	}
	t.Fatal("expected seeded session")
}

func TestCreateSession(t *testing.T) {
	store := testutil.TempStore(t)
	handler := CreateSession(store)
	body := `{"agent_id":"agent1","scope":"test"}`
	req := httptest.NewRequest("POST", "/api/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	result := jsonBody(t, rec)
	if result["id"] == nil || result["id"] == "" {
		t.Error("should return session id")
	}
}

func TestDeleteSession(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('del1', 'agent1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO session_messages (id, session_id, role, content) VALUES ('m1', 'del1', 'user', 'hello')`)

	handler := DeleteSession(store)
	req := httptest.NewRequest("DELETE", "/api/sessions/del1", nil)
	rec := httptest.NewRecorder()
	// chi URL param not set — handler uses chi.URLParam which returns ""
	// Need to test via the handler directly with a valid session ID.
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestGetSessionInsights(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('ins1', 'a1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id, tokens_in, tokens_out, cost) VALUES ('t1', 'ins1', 100, 50, 0.01)`)

	handler := GetSessionInsights(store)
	req := httptest.NewRequest("GET", "/api/sessions/ins1/insights", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	insights, ok := body["insights"].([]any)
	if !ok {
		t.Fatal("insights should be an array")
	}
	if len(insights) == 0 {
		t.Error("should include insight entries")
	}
}

func TestListSessions_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE sessions`); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}

	handler := ListSessions(store)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
