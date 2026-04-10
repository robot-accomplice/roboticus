package db

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
)

// CronRepository handles cron job persistence.
type CronRepository struct {
	q Querier
}

// NewCronRepository creates a cron repository.
func NewCronRepository(q Querier) *CronRepository {
	return &CronRepository{q: q}
}

// CreateJob inserts a new cron job.
func (r *CronRepository) CreateJob(ctx context.Context, id, name, description, kind, expr string, intervalMs any, agentID, payloadJSON string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, description, schedule_kind, schedule_expr, schedule_every_ms, agent_id, payload_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, description, kind, expr, intervalMs, agentID, payloadJSON)
	return err
}

// UpdateJob updates cron job fields dynamically.
func (r *CronRepository) UpdateJob(ctx context.Context, id string, setClauses []string, args []any) error {
	if len(setClauses) == 0 {
		return nil
	}
	query := "UPDATE cron_jobs SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	args = append(args, id)
	_, err := r.q.ExecContext(ctx, query, args...)
	return err
}

// TryAcquireLease attempts to acquire the execution lease for a cron job.
// Returns true if the lease was acquired (rows affected > 0).
// The lease is granted only when no other holder owns it or the existing lease has expired.
func (r *CronRepository) TryAcquireLease(ctx context.Context, jobID, holderID string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE cron_jobs SET lease_holder = ?, lease_expires_at = datetime('now', '+60 seconds')
		 WHERE id = ? AND (lease_holder IS NULL OR lease_expires_at < datetime('now'))`,
		holderID, jobID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ReleaseLease releases the execution lease for a cron job, but only if the
// caller is the current holder.
func (r *CronRepository) ReleaseLease(ctx context.Context, jobID, holderID string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE cron_jobs SET lease_holder = NULL, lease_expires_at = NULL
		 WHERE id = ? AND lease_holder = ?`,
		jobID, holderID)
	return err
}

// DeleteJob removes a cron job and its run history.
func (r *CronRepository) DeleteJob(ctx context.Context, id string) (int64, error) {
	// Best-effort: run history is secondary to the job itself.
	if _, err := r.q.ExecContext(ctx, `DELETE FROM cron_runs WHERE job_id = ?`, id); err != nil {
		log.Warn().Err(err).Str("job_id", id).Msg("cron: failed to delete run history")
	}
	res, err := r.q.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
