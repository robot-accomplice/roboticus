package db

import "context"

type DelegationRow struct {
	ID            string
	ParentTaskID  string
	SubagentID    string
	Status        string
	ResultSummary string
	ErrorMessage  string
	DurationMs    int64
	CreatedAt     string
}

type DelegationRepository struct {
	q Querier
}

func NewDelegationRepository(q Querier) *DelegationRepository {
	return &DelegationRepository{q: q}
}

func (r *DelegationRepository) Save(ctx context.Context, row DelegationRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO agent_delegation_outcomes (id, parent_task_id, subagent_id, status, result_summary, error_message, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.ParentTaskID, row.SubagentID, row.Status, row.ResultSummary, row.ErrorMessage, row.DurationMs,
	)
	return err
}

func (r *DelegationRepository) List(ctx context.Context, parentTaskID string) ([]DelegationRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, parent_task_id, subagent_id, status, result_summary, error_message, duration_ms, created_at
		 FROM agent_delegation_outcomes WHERE parent_task_id = ? ORDER BY created_at DESC`, parentTaskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []DelegationRow
	for rows.Next() {
		var dr DelegationRow
		if err := rows.Scan(&dr.ID, &dr.ParentTaskID, &dr.SubagentID, &dr.Status, &dr.ResultSummary, &dr.ErrorMessage, &dr.DurationMs, &dr.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, dr)
	}
	return result, rows.Err()
}

func (r *DelegationRepository) UpdateOutcome(ctx context.Context, id, status, summary, errMsg string, durationMs int64) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE agent_delegation_outcomes SET status = ?, result_summary = ?, error_message = ?, duration_ms = ? WHERE id = ?`,
		status, summary, errMsg, durationMs, id,
	)
	return err
}
