package schedule

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
)

// CronWorker polls for due cron jobs and executes them with lease-based locking.
type CronWorker struct {
	store      *db.Store
	scheduler  *DurableScheduler
	instanceID string
	interval   time.Duration
	executor   CronExecutor
}

// CronExecutor defines how a cron job is executed.
type CronExecutor interface {
	Execute(ctx context.Context, job *CronJob) error
}

// CronExecutorFunc is a function adapter for CronExecutor.
type CronExecutorFunc func(ctx context.Context, job *CronJob) error

func (f CronExecutorFunc) Execute(ctx context.Context, job *CronJob) error { return f(ctx, job) }

// NewCronWorker creates a cron worker.
func NewCronWorker(store *db.Store, instanceID string, interval time.Duration, executor CronExecutor) *CronWorker {
	return &CronWorker{
		store:      store,
		scheduler:  NewDurableScheduler(),
		instanceID: instanceID,
		interval:   interval,
		executor:   executor,
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

		// Execute.
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
			log.Warn().Str("job", job.Name).Err(err).Msg("cron job failed")
		} else {
			run.Status = CronRunSuccess
			log.Debug().Str("job", job.Name).Dur("duration", duration).Msg("cron job completed")
		}

		w.recordRun(ctx, run)
		w.updateLastRun(ctx, job, now)
		w.releaseLease(ctx, job.ID)
	}
}

func (w *CronWorker) listEnabledJobs(ctx context.Context) ([]*CronJob, error) {
	rows, err := w.store.QueryContext(ctx,
		`SELECT id, name, agent_id, schedule_kind, schedule_expr, schedule_every_ms,
		        payload_json, enabled, last_run_at, next_run_at
		 FROM cron_jobs WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*CronJob
	for rows.Next() {
		var job CronJob
		var lastRun, nextRun *string
		if err := rows.Scan(&job.ID, &job.Name, &job.AgentID, &job.Kind, &job.Expression,
			&job.IntervalMs, &job.PayloadJSON, &job.Enabled, &lastRun, &nextRun); err != nil {
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
	_, _ = w.store.ExecContext(ctx,
		`UPDATE cron_jobs SET lease_holder = NULL, lease_expires_at = NULL
		 WHERE id = ? AND lease_holder = ?`,
		jobID, w.instanceID)
}

func (w *CronWorker) recordRun(ctx context.Context, run *CronRun) {
	_, _ = w.store.ExecContext(ctx,
		`INSERT INTO cron_runs (job_id, status, duration_ms, error_msg, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		run.JobID, run.Status, run.DurationMs, run.ErrorMsg,
		run.Timestamp.UTC().Format(time.RFC3339))
}

func (w *CronWorker) updateLastRun(ctx context.Context, job *CronJob, now time.Time) {
	nextRun := w.scheduler.CalculateNextRun(job, now)
	var nextRunStr *string
	if nextRun != nil {
		s := nextRun.UTC().Format(time.RFC3339)
		nextRunStr = &s
	}
	_, _ = w.store.ExecContext(ctx,
		`UPDATE cron_jobs SET last_run_at = ?, next_run_at = ? WHERE id = ?`,
		now.UTC().Format(time.RFC3339), nextRunStr, job.ID)
}
