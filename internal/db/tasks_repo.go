package db

import (
	"context"
	"database/sql"
)

type TaskRow struct {
	ID          string
	Phase       string
	ParentID    string
	Goal        string
	CurrentStep int
	CreatedAt   string
	UpdatedAt   string
}

type TasksRepository struct {
	q Querier
}

func NewTasksRepository(q Querier) *TasksRepository {
	return &TasksRepository{q: q}
}

func (r *TasksRepository) Create(ctx context.Context, row TaskRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO agent_tasks (id, phase, parent_id, goal) VALUES (?, ?, ?, ?)`,
		row.ID, row.Phase, row.ParentID, row.Goal,
	)
	return err
}

func (r *TasksRepository) UpdatePhase(ctx context.Context, id, phase string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE agent_tasks SET phase = ?, updated_at = datetime('now') WHERE id = ?`,
		phase, id,
	)
	return err
}

func (r *TasksRepository) Get(ctx context.Context, id string) (*TaskRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, phase, parent_id, goal, current_step, created_at, updated_at FROM agent_tasks WHERE id = ?`, id)
	var tr TaskRow
	var parentID sql.NullString
	err := row.Scan(&tr.ID, &tr.Phase, &parentID, &tr.Goal, &tr.CurrentStep, &tr.CreatedAt, &tr.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tr.ParentID = parentID.String
	return &tr, nil
}

func (r *TasksRepository) List(ctx context.Context, phase string) ([]TaskRow, error) {
	query := `SELECT id, phase, parent_id, goal, current_step, created_at, updated_at FROM agent_tasks`
	var args []any
	if phase != "" {
		query += " WHERE phase = ?"
		args = append(args, phase)
	}
	query += " ORDER BY created_at DESC"

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []TaskRow
	for rows.Next() {
		var tr TaskRow
		var parentID sql.NullString
		if err := rows.Scan(&tr.ID, &tr.Phase, &parentID, &tr.Goal, &tr.CurrentStep, &tr.CreatedAt, &tr.UpdatedAt); err != nil {
			return nil, err
		}
		tr.ParentID = parentID.String
		result = append(result, tr)
	}
	return result, rows.Err()
}

func (r *TasksRepository) ListSubtasks(ctx context.Context, parentID string) ([]TaskRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, phase, parent_id, goal, current_step, created_at, updated_at FROM agent_tasks WHERE parent_id = ? ORDER BY created_at ASC`,
		parentID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []TaskRow
	for rows.Next() {
		var tr TaskRow
		var pid sql.NullString
		if err := rows.Scan(&tr.ID, &tr.Phase, &pid, &tr.Goal, &tr.CurrentStep, &tr.CreatedAt, &tr.UpdatedAt); err != nil {
			return nil, err
		}
		tr.ParentID = pid.String
		result = append(result, tr)
	}
	return result, rows.Err()
}
