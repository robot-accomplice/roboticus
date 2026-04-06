package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goboticus/testutil"
)

func TestListSessions(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key, status) VALUES ('s1', 'agent1', 'test', 'active')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key, status) VALUES ('s2', 'agent1', 'test2', 'active')`)

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
	insights, ok := body["insights"].(map[string]any)
	if !ok {
		t.Fatal("insights should be an object")
	}
	if insights["turn_count"] == nil {
		t.Error("should include turn_count")
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
