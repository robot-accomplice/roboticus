package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roboticus/internal/pipeline"
	"roboticus/testutil"
)

type stubCronRunner struct {
	run func(ctx context.Context, cfg pipeline.Config, input pipeline.Input) (*pipeline.Outcome, error)
}

func (s stubCronRunner) Run(ctx context.Context, cfg pipeline.Config, input pipeline.Input) (*pipeline.Outcome, error) {
	return s.run(ctx, cfg, input)
}

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

func TestListCronJobs_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE cron_jobs`); err != nil {
		t.Fatalf("drop cron_jobs: %v", err)
	}

	handler := ListCronJobs(store)
	req := httptest.NewRequest("GET", "/api/cron/jobs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestRunCronJobNow_UsesWorkerLifecycle(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO cron_jobs (id, name, enabled, schedule_kind, schedule_expr, agent_id, payload_json)
		 VALUES ('cj-run', 'run-job', 1, 'cron', '0 * * * *', 'agent1', '{}')`)

	var gotInput pipeline.Input
	handler := RunCronJobNow(stubCronRunner{
		run: func(ctx context.Context, cfg pipeline.Config, input pipeline.Input) (*pipeline.Outcome, error) {
			gotInput = input
			return &pipeline.Outcome{Content: "ok"}, nil
		},
	}, store, "CronBot")

	req := httptest.NewRequest("POST", "/api/cron/jobs/cj-run/run", nil)
	rec := httptest.NewRecorder()
	chiRouter("POST", "/api/cron/jobs/{id}/run", handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotInput.AgentID != "agent1" {
		t.Fatalf("agent_id = %q, want agent1", gotInput.AgentID)
	}
	if gotInput.AgentName != "CronBot" {
		t.Fatalf("agent_name = %q, want CronBot", gotInput.AgentName)
	}

	var runCount int
	row := store.QueryRowContext(bgCtx, `SELECT COUNT(*) FROM cron_runs WHERE job_id = 'cj-run'`)
	if err := row.Scan(&runCount); err != nil {
		t.Fatalf("scan cron_runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("cron run count = %d, want 1", runCount)
	}
}
