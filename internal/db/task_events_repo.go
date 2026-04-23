package db

import "context"

// TaskEventRow is an operator-visible task lifecycle event.
type TaskEventRow struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	ParentTaskID string `json:"parent_task_id,omitempty"`
	AssignedTo   string `json:"assigned_to,omitempty"`
	EventType    string `json:"event_type"`
	PayloadJSON  string `json:"payload_json"`
	CreatedAt    string `json:"created_at"`
}

// TaskEventsRepository handles task event persistence and queries.
type TaskEventsRepository struct {
	q Querier
}

// NewTaskEventsRepository creates a task events repository.
func NewTaskEventsRepository(q Querier) *TaskEventsRepository {
	return &TaskEventsRepository{q: q}
}

// Append records a new task lifecycle event.
func (r *TaskEventsRepository) Append(ctx context.Context, row TaskEventRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO task_events (id, task_id, parent_task_id, assigned_to, event_type, payload_json)
		 VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		row.ID, row.TaskID, row.ParentTaskID, row.AssignedTo, row.EventType, row.PayloadJSON,
	)
	return err
}

// ListRecent returns recent task events, optionally scoped to a task.
func (r *TaskEventsRepository) ListRecent(ctx context.Context, taskID string, limit int) ([]TaskEventRow, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, task_id, COALESCE(parent_task_id, ''), COALESCE(assigned_to, ''), event_type, payload_json, created_at
		FROM task_events`
	args := make([]any, 0, 2)
	if taskID != "" {
		query += ` WHERE task_id = ?`
		args = append(args, taskID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	events := make([]TaskEventRow, 0)
	for rows.Next() {
		var row TaskEventRow
		if err := rows.Scan(&row.ID, &row.TaskID, &row.ParentTaskID, &row.AssignedTo, &row.EventType, &row.PayloadJSON, &row.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, row)
	}
	return events, rows.Err()
}
