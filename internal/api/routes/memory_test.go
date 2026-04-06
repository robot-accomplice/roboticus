package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"goboticus/testutil"
)

func TestGetWorkingMemory(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO working_memory (id, session_id, entry_type, content) VALUES ('wm1', 's1', 'goal', 'test goal')`)

	handler := GetWorkingMemory(store)
	req := httptest.NewRequest("GET", "/api/memory/working", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	entries := body["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}

func TestGetEpisodicMemory(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO episodic_memory (id, classification, content, importance) VALUES ('em1', 'interaction', 'test event', 7)`)

	handler := GetEpisodicMemory(store)
	req := httptest.NewRequest("GET", "/api/memory/episodic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	entries := body["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}

func TestGetSemanticMemory(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence) VALUES ('sm1', 'facts', 'capital', 'Paris', 0.95)`)

	handler := GetSemanticMemory(store)
	req := httptest.NewRequest("GET", "/api/memory/semantic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	entries := body["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}

func TestSearchMemory(t *testing.T) {
	store := testutil.TempStore(t)
	// Seed FTS data.
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO semantic_memory (id, category, key, value) VALUES ('sm1', 'facts', 'capital', 'Paris is the capital of France')`)

	handler := SearchMemory(store)
	req := httptest.NewRequest("GET", "/api/memory/search?q=Paris", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestGetWorkingMemory_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE working_memory`); err != nil {
		t.Fatalf("drop working_memory: %v", err)
	}

	handler := GetWorkingMemory(store)
	req := httptest.NewRequest("GET", "/api/memory/working", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
