package db

import (
	"context"
	"strings"
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

// DeleteJob removes a cron job and its run history.
func (r *CronRepository) DeleteJob(ctx context.Context, id string) (int64, error) {
	_, _ = r.q.ExecContext(ctx, `DELETE FROM cron_runs WHERE job_id = ?`, id)
	res, err := r.q.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
