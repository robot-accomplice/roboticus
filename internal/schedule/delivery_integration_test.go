package schedule

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/core"
	"roboticus/testutil"
)

// TestCronDeliveryE2E exercises the full cron delivery flow:
// create cron job with delivery_mode="push" → trigger execution →
// verify delivery queue has the message.
func TestCronDeliveryE2E(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// -- Step 1: insert a cron job with delivery_mode="push" and delivery_channel="test" --
	jobID := "delivery-e2e-job"
	_, err := store.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, agent_id, schedule_kind, schedule_expr,
		    schedule_every_ms, payload_json, enabled,
		    delivery_mode, delivery_channel,
		    retry_count, max_retries, retry_delay_ms)
		 VALUES (?, 'delivery-test', 'agent-1', 'interval', '', 60000,
		    '{"prompt":"hello world"}', 1,
		    'push', 'test',
		    0, 3, 60000)`,
		jobID)
	if err != nil {
		t.Fatalf("insert cron_job: %v", err)
	}

	// -- Step 2: create an executor that simulates pipeline execution --
	// The executor writes to the delivery_queue (mimicking what a real
	// pipeline connector would do after RunPipeline completes).
	executorCalled := false
	executor := CronExecutorFunc(func(ctx context.Context, job *CronJob) error {
		executorCalled = true

		// Verify the job fields were read correctly.
		if job.DeliveryMode != "push" {
			t.Errorf("expected delivery_mode=push, got %q", job.DeliveryMode)
		}
		if job.DeliveryChannel != "test" {
			t.Errorf("expected delivery_channel=test, got %q", job.DeliveryChannel)
		}

		// Simulate pipeline output → delivery queue INSERT.
		_, err := store.ExecContext(ctx,
			`INSERT INTO delivery_queue (id, channel, recipient_id, content, idempotency_key, status)
			 VALUES (?, ?, ?, ?, ?, 'pending')`,
			"dq-"+job.ID, job.DeliveryChannel, job.AgentID, "pipeline output for: "+job.Name, "idem-"+job.ID)
		return err
	})

	// -- Step 3: create and run the worker for one tick --
	errBus := core.NewErrorBus(ctx, 16)
	worker := NewCronWorker(store, "test-instance", 1*time.Second, executor, errBus)

	// Manually set last_run_at to the past so the job is due.
	past := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	_, _ = store.ExecContext(ctx,
		`UPDATE cron_jobs SET last_run_at = ? WHERE id = ?`, past, jobID)

	// Execute one tick directly.
	worker.tick(ctx)

	// -- Step 4: verify executor was called --
	if !executorCalled {
		t.Fatal("executor was never called; job may not have been due")
	}

	// -- Step 5: verify delivery_queue row exists --
	var dqID, dqChannel, dqRecipient, dqContent, dqStatus string
	err = store.QueryRowContext(ctx,
		`SELECT id, channel, recipient_id, content, status FROM delivery_queue WHERE id = ?`,
		"dq-"+jobID).Scan(&dqID, &dqChannel, &dqRecipient, &dqContent, &dqStatus)
	if err != nil {
		t.Fatalf("delivery_queue row not found: %v", err)
	}

	if dqChannel != "test" {
		t.Errorf("delivery_queue.channel = %q, want %q", dqChannel, "test")
	}
	if dqRecipient != "agent-1" {
		t.Errorf("delivery_queue.recipient_id = %q, want %q", dqRecipient, "agent-1")
	}
	if dqContent != "pipeline output for: delivery-test" {
		t.Errorf("delivery_queue.content = %q", dqContent)
	}
	if dqStatus != "pending" {
		t.Errorf("delivery_queue.status = %q, want %q", dqStatus, "pending")
	}

	// -- Step 6: verify cron_runs recorded success --
	var runStatus string
	err = store.QueryRowContext(ctx,
		`SELECT status FROM cron_runs WHERE job_id = ? ORDER BY rowid DESC LIMIT 1`,
		jobID).Scan(&runStatus)
	if err != nil {
		t.Fatalf("cron_runs row not found: %v", err)
	}
	if runStatus != "success" {
		t.Errorf("cron_runs.status = %q, want %q", runStatus, "success")
	}
}

// TestCronDeliveryE2E_ExecutorFailure verifies that when the executor fails,
// the delivery_queue remains empty and the run is recorded as failed.
func TestCronDeliveryE2E_ExecutorFailure(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	jobID := "delivery-fail-job"
	_, err := store.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, agent_id, schedule_kind, schedule_expr,
		    schedule_every_ms, payload_json, enabled,
		    delivery_mode, delivery_channel,
		    retry_count, max_retries, retry_delay_ms)
		 VALUES (?, 'fail-test', 'agent-2', 'interval', '', 60000,
		    '{}', 1,
		    'push', 'test',
		    0, 3, 60000)`,
		jobID)
	if err != nil {
		t.Fatalf("insert cron_job: %v", err)
	}

	executor := CronExecutorFunc(func(_ context.Context, _ *CronJob) error {
		return context.DeadlineExceeded // simulate timeout
	})

	errBus := core.NewErrorBus(ctx, 16)
	worker := NewCronWorker(store, "test-instance", 1*time.Second, executor, errBus)

	past := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	_, _ = store.ExecContext(ctx,
		`UPDATE cron_jobs SET last_run_at = ? WHERE id = ?`, past, jobID)

	worker.tick(ctx)

	// Delivery queue should be empty.
	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM delivery_queue WHERE channel = 'test'`).Scan(&count)
	if count != 0 {
		t.Errorf("delivery_queue should be empty on failure, got %d rows", count)
	}

	// Run should be recorded as failed.
	var runStatus string
	err = store.QueryRowContext(ctx,
		`SELECT status FROM cron_runs WHERE job_id = ? ORDER BY rowid DESC LIMIT 1`,
		jobID).Scan(&runStatus)
	if err != nil {
		t.Fatalf("cron_runs row not found: %v", err)
	}
	if runStatus != "failed" {
		t.Errorf("cron_runs.status = %q, want %q", runStatus, "failed")
	}
}
