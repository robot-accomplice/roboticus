package schedule

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// CronWorker polls for due cron jobs and executes them with lease-based locking.
type CronWorker struct {
	store      *db.Store
	scheduler  *DurableScheduler
	instanceID string
	interval   time.Duration
	executor   CronExecutor
	errBus     *core.ErrorBus
}

// CronExecutor defines how a cron job is executed.
type CronExecutor interface {
	Execute(ctx context.Context, job *CronJob) error
}

// CronExecutorFunc is a function adapter for CronExecutor.
type CronExecutorFunc func(ctx context.Context, job *CronJob) error

func (f CronExecutorFunc) Execute(ctx context.Context, job *CronJob) error { return f(ctx, job) }

// NewCronWorker creates a cron worker.
func NewCronWorker(store *db.Store, instanceID string, interval time.Duration, executor CronExecutor, errBus *core.ErrorBus) *CronWorker {
	return &CronWorker{
		store:      store,
		scheduler:  NewDurableScheduler(),
		instanceID: instanceID,
		interval:   interval,
		executor:   executor,
		errBus:     errBus,
	}
}

// Run starts the cron worker loop. Blocks until context is cancelled.
func (w *CronWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log.Info().Str("instance", w.instanceID).Dur("interval", w.interval).Msg("cron worker started")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("cron worker stopping")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *CronWorker) tick(ctx context.Context) {
	now := time.Now()

	jobs, err := w.listEnabledJobs(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cron worker: failed to list jobs")
		return
	}

	for _, job := range jobs {
		if !w.scheduler.IsDue(job, now) {
			continue
		}

		// Acquire lease.
		if !w.acquireLease(ctx, job.ID) {
			continue
		}
		if err := w.executeJob(ctx, job, now); err != nil {
			// executeJob already records run history and retry state; here we only
			// preserve visibility for the worker loop.
			log.Debug().Err(err).Str("job_id", job.ID).Msg("cron worker: job execution returned error")
		}
		w.releaseLease(ctx, job.ID)
	}
}

// RunJobNow executes a single enabled cron job through the same lease/run-history
// lifecycle used by the durable worker loop.
func (w *CronWorker) RunJobNow(ctx context.Context, jobID string) error {
	job, err := w.loadEnabledJob(ctx, jobID)
	if err != nil {
		return err
	}
	if !w.acquireLease(ctx, job.ID) {
		return &LeaseError{JobID: job.ID, Holder: "another instance"}
	}
	defer w.releaseLease(ctx, job.ID)
	return w.executeJob(ctx, job, time.Now())
}

func (w *CronWorker) listEnabledJobs(ctx context.Context) ([]*CronJob, error) {
	rows, err := w.store.QueryContext(ctx,
		`SELECT id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms,
		        payload_json, enabled, last_run_at, next_run_at,
		        COALESCE(retry_count, 0), COALESCE(max_retries, 3), COALESCE(retry_delay_ms, 60000),
		        COALESCE(delivery_mode, 'none'), COALESCE(delivery_channel, '')
		 FROM cron_jobs WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []*CronJob
	for rows.Next() {
		var job CronJob
		var lastRun, nextRun *string
		if err := rows.Scan(&job.ID, &job.Name, &job.AgentID, &job.Kind, &job.Expression,
			&job.IntervalMs, &job.PayloadJSON, &job.Enabled, &lastRun, &nextRun,
			&job.RetryCount, &job.MaxRetries, &job.RetryDelayMs,
			&job.DeliveryMode, &job.DeliveryChannel); err != nil {
			continue
		}
		if lastRun != nil {
			t, _ := time.Parse(time.RFC3339, *lastRun)
			job.LastRunAt = &t
		}
		if nextRun != nil {
			t, _ := time.Parse(time.RFC3339, *nextRun)
			job.NextRunAt = &t
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

func (w *CronWorker) acquireLease(ctx context.Context, jobID string) bool {
	// Use inline lease columns on cron_jobs (matches roboticus approach).
	// Atomically claim the job if unleased or if the previous lease has expired.
	res, err := w.store.ExecContext(ctx,
		`UPDATE cron_jobs
		 SET lease_holder = ?, lease_expires_at = datetime('now', '+60 seconds')
		 WHERE id = ? AND (lease_holder IS NULL OR lease_expires_at < datetime('now'))`,
		w.instanceID, jobID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (w *CronWorker) releaseLease(ctx context.Context, jobID string) {
	if _, err := w.store.ExecContext(ctx,
		`UPDATE cron_jobs SET lease_holder = NULL, lease_expires_at = NULL
		 WHERE id = ? AND lease_holder = ?`,
		jobID, w.instanceID); err != nil {
		w.errBus.ReportIfErr(err, "scheduler", "release_lease", core.SevWarning)
	}
}

func (w *CronWorker) executeJob(ctx context.Context, job *CronJob, now time.Time) error {
	start := time.Now()
	err := w.executor.Execute(ctx, job)
	duration := time.Since(start)

	run := &CronRun{
		JobID:      job.ID,
		DurationMs: duration.Milliseconds(),
		Timestamp:  now,
	}
	if err != nil {
		run.Status = CronRunFailed
		run.ErrorMsg = err.Error()
		log.Warn().Str("job", job.Name).Str("job_id", job.ID).Str("agent_id", job.AgentID).Err(err).Int64("duration_ms", duration.Milliseconds()).Msg("cron job failed")
		w.recordRun(ctx, run)
		w.handleRetry(ctx, job, now)
		return err
	}

	run.Status = CronRunSuccess
	log.Info().Str("job", job.Name).Str("job_id", job.ID).Str("agent_id", job.AgentID).Int64("duration_ms", duration.Milliseconds()).Msg("cron job completed")
	w.recordRun(ctx, run)
	w.resetRetry(ctx, job.ID)
	w.updateLastRun(ctx, job, now)
	return nil
}

func (w *CronWorker) loadEnabledJob(ctx context.Context, jobID string) (*CronJob, error) {
	row := w.store.QueryRowContext(ctx,
		`SELECT id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms,
		        payload_json, enabled, last_run_at, next_run_at,
		        COALESCE(retry_count, 0), COALESCE(max_retries, 3), COALESCE(retry_delay_ms, 60000),
		        COALESCE(delivery_mode, 'none'), COALESCE(delivery_channel, '')
		 FROM cron_jobs
		 WHERE id = ? AND enabled = 1`,
		jobID,
	)

	var job CronJob
	var lastRun, nextRun *string
	var intervalMs *int64
	if err := row.Scan(&job.ID, &job.Name, &job.AgentID, &job.Kind, &job.Expression,
		&intervalMs, &job.PayloadJSON, &job.Enabled, &lastRun, &nextRun,
		&job.RetryCount, &job.MaxRetries, &job.RetryDelayMs,
		&job.DeliveryMode, &job.DeliveryChannel); err != nil {
		return nil, err
	}
	if intervalMs != nil {
		job.IntervalMs = *intervalMs
	}
	if lastRun != nil {
		t, _ := time.Parse(time.RFC3339, *lastRun)
		job.LastRunAt = &t
	}
	if nextRun != nil {
		t, _ := time.Parse(time.RFC3339, *nextRun)
		job.NextRunAt = &t
	}
	return &job, nil
}

func (w *CronWorker) recordRun(ctx context.Context, run *CronRun) {
	_, err := w.store.ExecContext(ctx,
		`INSERT INTO cron_runs (job_id, status, duration_ms, error_msg, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		run.JobID, run.Status, run.DurationMs, run.ErrorMsg,
		run.Timestamp.UTC().Format(time.RFC3339))
	if err != nil {
		w.errBus.ReportIfErr(err, "scheduler", "record_run", core.SevWarning)
	}
}

func (w *CronWorker) updateLastRun(ctx context.Context, job *CronJob, now time.Time) {
	nextRun := w.scheduler.CalculateNextRun(job, now)
	var nextRunStr *string
	if nextRun != nil {
		s := nextRun.UTC().Format(time.RFC3339)
		nextRunStr = &s
	}
	if _, err := w.store.ExecContext(ctx,
		`UPDATE cron_jobs SET last_run_at = ?, next_run_at = ? WHERE id = ?`,
		now.UTC().Format(time.RFC3339), nextRunStr, job.ID); err != nil {
		w.errBus.ReportIfErr(err, "scheduler", "update_last_run", core.SevWarning)
	}
}

// handleRetry increments the retry counter and schedules a retry with
// exponential backoff, or records exhaustion when max retries are exceeded.
func (w *CronWorker) handleRetry(ctx context.Context, job *CronJob, now time.Time) {
	newCount := job.RetryCount + 1
	maxRetries := job.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	if newCount > maxRetries {
		// Exhausted — record final status and advance to next regular run.
		w.recordRun(ctx, &CronRun{
			JobID:     job.ID,
			Status:    "exhausted",
			ErrorMsg:  "max retries exceeded",
			Timestamp: now,
		})
		w.resetRetry(ctx, job.ID)
		w.updateLastRun(ctx, job, now)
		log.Error().Str("job", job.Name).Int("retries", maxRetries).Msg("cron job retries exhausted")
		return
	}

	// Exponential backoff: delay * 2^(retryCount-1).
	delayMs := job.RetryDelayMs
	if delayMs <= 0 {
		delayMs = 60000
	}
	backoff := delayMs
	for i := 1; i < newCount; i++ {
		backoff *= 2
	}
	retryAt := now.Add(time.Duration(backoff) * time.Millisecond)
	retryStr := retryAt.UTC().Format(time.RFC3339)

	if _, err := w.store.ExecContext(ctx,
		`UPDATE cron_jobs SET retry_count = ?, next_run_at = ?, last_run_at = ? WHERE id = ?`,
		newCount, retryStr, now.UTC().Format(time.RFC3339), job.ID); err != nil {
		w.errBus.ReportIfErr(err, "scheduler", "handle_retry", core.SevWarning)
	}

	log.Debug().Str("job", job.Name).Int("retry", newCount).Time("retry_at", retryAt).Msg("cron job scheduled for retry")
}

// resetRetry resets the retry counter on successful execution.
func (w *CronWorker) resetRetry(ctx context.Context, jobID string) {
	if _, err := w.store.ExecContext(ctx,
		`UPDATE cron_jobs SET retry_count = 0 WHERE id = ?`, jobID); err != nil {
		w.errBus.ReportIfErr(err, "scheduler", "reset_retry", core.SevWarning)
	}
}
