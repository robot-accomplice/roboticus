package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goboticus/testutil"
)

func TestListCronJobs(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO cron_jobs (id, name, enabled, schedule_kind, schedule_expr, agent_id, payload_json)
		 VALUES ('cj1', 'test-job', 1, 'cron', '0 * * * *', 'agent1', '{}')`)

	handler := ListCronJobs(store)
	req := httptest.NewRequest("GET", "/api/cron/jobs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	jobs := body["jobs"].([]any)
	if len(jobs) != 1 {
		t.Errorf("got %d jobs, want 1", len(jobs))
	}
}

func TestCreateCronJob(t *testing.T) {
	store := testutil.TempStore(t)
	handler := CreateCronJob(store)
	body := `{"name":"new-job","schedule_kind":"cron","schedule_expr":"0 9 * * *","agent_id":"a1","payload_json":"{}"}`
	req := httptest.NewRequest("POST", "/api/cron/jobs", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestListCronRuns(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO cron_jobs (id, name, enabled, schedule_kind, agent_id, payload_json) VALUES ('cj1', 'j1', 1, 'cron', 'a1', '{}')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO cron_runs (id, job_id, status, duration_ms) VALUES ('cr1', 'cj1', 'success', 150)`)

	handler := ListCronRuns(store)
	req := httptest.NewRequest("GET", "/api/cron/runs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	runs := body["runs"].([]any)
	if len(runs) != 1 {
		t.Errorf("got %d runs, want 1", len(runs))
	}
}
