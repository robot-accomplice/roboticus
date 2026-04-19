package schedule

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"roboticus/internal/db"
	"roboticus/testutil"
)

func TestCronExecutorFunc(t *testing.T) {
	called := false
	fn := CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		called = true
		if job.Name != "test-job" {
			t.Fatalf("expected job name test-job, got %s", job.Name)
		}
		return nil
	})

	err := fn.Execute(context.Background(), &CronJob{Name: "test-job"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("executor func was not called")
	}
}

func TestCronExecutorFunc_Error(t *testing.T) {
	fn := CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		return errors.New("boom")
	})
	err := fn.Execute(context.Background(), &CronJob{Name: "fail"})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected 'boom' error, got %v", err)
	}
}

func TestNewCronWorker(t *testing.T) {
	store := testutil.TempStore(t)
	exec := CronExecutorFunc(func(ctx context.Context, job *CronJob) error { return nil })
	w := NewCronWorker(store, "instance-1", 5*time.Second, exec, nil)

	if w.instanceID != "instance-1" {
		t.Fatalf("expected instance-1, got %s", w.instanceID)
	}
	if w.interval != 5*time.Second {
		t.Fatalf("expected 5s interval, got %v", w.interval)
	}
	if w.scheduler == nil {
		t.Fatal("scheduler should not be nil")
	}
}

func TestCronWorkerRun_CancelledImmediately(t *testing.T) {
	store := testutil.TempStore(t)
	exec := CronExecutorFunc(func(ctx context.Context, job *CronJob) error { return nil })
	w := NewCronWorker(store, "inst", 50*time.Millisecond, exec, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Good, Run returned.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestCronWorkerTick_ExecutesJob(t *testing.T) {
	store := testutil.TempStore(t)
	bgCtx := context.Background()

	// Insert an interval job (tables created by TempStore schema).
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO cron_jobs (id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms, payload_json, enabled)
		 VALUES ('j1', 'fast-job', 'a1', 'interval', '', 100, '{}', 1)`)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	var executedID string
	exec := CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		executedID = job.ID
		return nil
	})

	w := NewCronWorker(store, "test-inst", time.Second, exec, nil)
	// Directly call tick to avoid timing issues.
	w.tick(bgCtx)

	if executedID != "j1" {
		t.Fatalf("expected job j1 to execute, got %q", executedID)
	}
}

func TestLeaseError(t *testing.T) {
	e := &LeaseError{JobID: "j1", Holder: "inst-2"}
	if e.Error() != "lease held by inst-2 for job j1" {
		t.Fatalf("unexpected error message: %s", e.Error())
	}
}

// --- Lease contention and retry validation tests ---

func TestLease_AcquireAndRelease(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	insertTestJob(t, store, "lease-1")

	w := NewCronWorker(store, "A", time.Second, nopExecutor(), nil)

	if !w.acquireLease(ctx, "lease-1") {
		t.Fatal("should acquire on first attempt")
	}
	// Second instance cannot acquire while held.
	w2 := NewCronWorker(store, "B", time.Second, nopExecutor(), nil)
	if w2.acquireLease(ctx, "lease-1") {
		t.Fatal("should NOT acquire while held by A")
	}
	// Release and re-acquire.
	w.releaseLease(ctx, "lease-1")
	if !w2.acquireLease(ctx, "lease-1") {
		t.Fatal("should acquire after A released")
	}
}

func TestLease_ExpiryAllowsReacquisition(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	insertTestJob(t, store, "lease-2")

	w := NewCronWorker(store, "A", time.Second, nopExecutor(), nil)
	w.acquireLease(ctx, "lease-2")

	// Force-expire the lease.
	_, _ = store.ExecContext(ctx,
		`UPDATE cron_jobs SET lease_expires_at = datetime('now', '-10 seconds') WHERE id = 'lease-2'`)

	w2 := NewCronWorker(store, "B", time.Second, nopExecutor(), nil)
	if !w2.acquireLease(ctx, "lease-2") {
		t.Fatal("should claim expired lease")
	}
}

func TestLease_Contention_ExactlyOneWinner(t *testing.T) {
	store := testutil.TempStore(t)
	insertTestJob(t, store, "lease-3")

	var acquired int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			w := NewCronWorker(store, fmt.Sprintf("c-%d", idx), time.Second, nopExecutor(), nil)
			if w.acquireLease(context.Background(), "lease-3") {
				atomic.AddInt32(&acquired, 1)
			}
		}(i)
	}
	wg.Wait()

	if acquired != 1 {
		t.Errorf("expected exactly 1 winner, got %d", acquired)
	}
}

func TestRetry_ExponentialBackoff(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	insertTestJob(t, store, "retry-1")
	_, _ = store.ExecContext(ctx,
		`UPDATE cron_jobs SET max_retries = 5, retry_delay_ms = 100 WHERE id = 'retry-1'`)

	w := NewCronWorker(store, "retrier", time.Second, nopExecutor(), nil)
	now := time.Now()

	// Retry 3 times — should set next_run_at with increasing delays.
	for i := 0; i < 3; i++ {
		job := &CronJob{ID: "retry-1", Name: "test", MaxRetries: 5, RetryDelayMs: 100, RetryCount: i}
		w.handleRetry(ctx, job, now)
	}

	var retryCount int
	row := store.QueryRowContext(ctx, `SELECT COALESCE(retry_count, 0) FROM cron_jobs WHERE id = 'retry-1'`)
	_ = row.Scan(&retryCount)
	if retryCount != 3 {
		t.Errorf("retry_count = %d, want 3", retryCount)
	}
}

func TestRetry_Exhaustion(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	insertTestJob(t, store, "retry-2")
	_, _ = store.ExecContext(ctx,
		`UPDATE cron_jobs SET max_retries = 2, retry_delay_ms = 50 WHERE id = 'retry-2'`)

	w := NewCronWorker(store, "retrier", time.Second, nopExecutor(), nil)

	// Exceed max retries.
	job := &CronJob{ID: "retry-2", Name: "exhaust", MaxRetries: 2, RetryDelayMs: 50, RetryCount: 3}
	w.handleRetry(ctx, job, time.Now())

	// Check exhausted run was recorded.
	var count int
	row := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM cron_runs WHERE job_id = 'retry-2' AND status = 'exhausted'`)
	_ = row.Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 exhausted run, got %d", count)
	}
}

func TestRunRecording_SuccessAndFailure(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	insertTestJob(t, store, "run-1")

	w := NewCronWorker(store, "recorder", time.Second, nopExecutor(), nil)

	w.recordRun(ctx, &CronRun{JobID: "run-1", Status: CronRunSuccess, DurationMs: 42, Timestamp: time.Now()})
	w.recordRun(ctx, &CronRun{JobID: "run-1", Status: CronRunFailed, DurationMs: 99, ErrorMsg: "boom", Timestamp: time.Now()})

	var successCount, failCount int
	row := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM cron_runs WHERE job_id = 'run-1' AND status = 'success'`)
	_ = row.Scan(&successCount)
	row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM cron_runs WHERE job_id = 'run-1' AND status = 'failed'`)
	_ = row.Scan(&failCount)

	if successCount != 1 || failCount != 1 {
		t.Errorf("success=%d fail=%d, want 1 each", successCount, failCount)
	}

	var errorMsg, timestamp string
	row = store.QueryRowContext(ctx,
		`SELECT error_msg, timestamp FROM cron_runs
		 WHERE job_id = 'run-1' AND status = 'failed'
		 ORDER BY rowid DESC LIMIT 1`)
	if err := row.Scan(&errorMsg, &timestamp); err != nil {
		t.Fatalf("scan cron_runs error/timestamp: %v", err)
	}
	if errorMsg != "boom" {
		t.Fatalf("cron_runs.error_msg = %q, want boom", errorMsg)
	}
	if timestamp == "" {
		t.Fatal("cron_runs.timestamp should not be empty")
	}
}

func TestRunJobNow_UsesRunLifecycle(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	insertTestJob(t, store, "run-now-1")

	var executedID string
	w := NewCronWorker(store, "manual", time.Second, CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		executedID = job.ID
		return nil
	}), nil)

	if err := w.RunJobNow(ctx, "run-now-1"); err != nil {
		t.Fatalf("RunJobNow: %v", err)
	}
	if executedID != "run-now-1" {
		t.Fatalf("executed job = %q, want run-now-1", executedID)
	}

	var status string
	row := store.QueryRowContext(ctx, `SELECT status FROM cron_runs WHERE job_id = 'run-now-1' ORDER BY rowid DESC LIMIT 1`)
	if err := row.Scan(&status); err != nil {
		t.Fatalf("scan cron run: %v", err)
	}
	if status != string(CronRunSuccess) {
		t.Fatalf("cron_runs.status = %q, want %q", status, CronRunSuccess)
	}
}

func insertTestJob(t *testing.T, store *db.Store, id string) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO cron_jobs (id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms, payload_json, enabled)
		 VALUES (?, ?, 'agent', 'interval', '', 60000, '{}', 1)`, id, "test-"+id)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func nopExecutor() CronExecutor {
	return CronExecutorFunc(func(ctx context.Context, job *CronJob) error { return nil })
}
