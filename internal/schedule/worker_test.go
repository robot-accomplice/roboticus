package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

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
	w := NewCronWorker(store, "instance-1", 5*time.Second, exec)

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
	w := NewCronWorker(store, "inst", 50*time.Millisecond, exec)

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

	w := NewCronWorker(store, "test-inst", time.Second, exec)
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
