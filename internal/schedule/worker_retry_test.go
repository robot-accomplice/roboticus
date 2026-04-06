package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"roboticus/testutil"
)

func TestCronWorker_RetryIncrementsOnFailure(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert a job.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms, payload_json, enabled, retry_count, max_retries, retry_delay_ms)
		 VALUES ('j1', 'test-job', 'a1', 'interval', '', 60000, '{}', 1, 0, 3, 1000)`)

	failExec := CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		return errors.New("simulated failure")
	})

	worker := NewCronWorker(store, "test-instance", time.Minute, failExec)
	worker.tick(ctx)

	// Check retry count was incremented.
	var retryCount int
	row := store.QueryRowContext(ctx, `SELECT retry_count FROM cron_jobs WHERE id = 'j1'`)
	if err := row.Scan(&retryCount); err != nil {
		t.Fatalf("scan retry_count: %v", err)
	}
	if retryCount != 1 {
		t.Errorf("retry_count = %d, want 1", retryCount)
	}
}

func TestCronWorker_RetryResetsOnSuccess(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert a job with retry_count=2 (was failing).
	_, _ = store.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms, payload_json, enabled, retry_count, max_retries, retry_delay_ms)
		 VALUES ('j2', 'test-job', 'a1', 'interval', '', 60000, '{}', 1, 2, 3, 1000)`)

	successExec := CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		return nil
	})

	worker := NewCronWorker(store, "test-instance", time.Minute, successExec)
	worker.tick(ctx)

	var retryCount int
	row := store.QueryRowContext(ctx, `SELECT retry_count FROM cron_jobs WHERE id = 'j2'`)
	if err := row.Scan(&retryCount); err != nil {
		t.Fatalf("scan retry_count: %v", err)
	}
	if retryCount != 0 {
		t.Errorf("retry_count = %d, want 0 after success", retryCount)
	}
}

func TestCronWorker_RetryExhaustion(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert a job at max retries.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms, payload_json, enabled, retry_count, max_retries, retry_delay_ms)
		 VALUES ('j3', 'test-job', 'a1', 'interval', '', 60000, '{}', 1, 3, 3, 1000)`)

	failExec := CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		return errors.New("persistent failure")
	})

	worker := NewCronWorker(store, "test-instance", time.Minute, failExec)
	worker.tick(ctx)

	// After exhaustion, retry_count should be reset to 0.
	var retryCount int
	row := store.QueryRowContext(ctx, `SELECT retry_count FROM cron_jobs WHERE id = 'j3'`)
	if err := row.Scan(&retryCount); err != nil {
		t.Fatalf("scan retry_count: %v", err)
	}
	if retryCount != 0 {
		t.Errorf("retry_count = %d, want 0 after exhaustion reset", retryCount)
	}

	// Should have an "exhausted" run recorded.
	var exhaustedCount int
	row = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM cron_runs WHERE job_id = 'j3' AND status = 'exhausted'`)
	if err := row.Scan(&exhaustedCount); err != nil {
		t.Fatalf("scan exhausted count: %v", err)
	}
	if exhaustedCount != 1 {
		t.Errorf("exhausted runs = %d, want 1", exhaustedCount)
	}
}
