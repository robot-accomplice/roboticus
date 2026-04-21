package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

type DelegatedTaskRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Priority    int    `json:"priority"`
	Source      string `json:"source,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type DelegatedTaskSummary struct {
	DelegatedTaskRow
	LatestEventType string `json:"latest_event_type,omitempty"`
	LatestEventAt   string `json:"latest_event_at,omitempty"`
	AssignedTo      string `json:"assigned_to,omitempty"`
	EventCount      int    `json:"event_count"`
}

type DelegatedTaskStatus struct {
	Task     DelegatedTaskSummary `json:"task"`
	Events   []TaskEventRow       `json:"events,omitempty"`
	Outcomes []DelegationRow      `json:"delegation_outcomes,omitempty"`
}

type DelegatedTaskRetryResult struct {
	Updated     bool                 `json:"updated"`
	PriorStatus string               `json:"prior_status,omitempty"`
	Task        *DelegatedTaskStatus `json:"task,omitempty"`
}

type DelegatedTaskLifecycleRepository struct {
	q        Querier
	events   *TaskEventsRepository
	outcomes *DelegationRepository
}

func NewDelegatedTaskLifecycleRepository(q Querier) *DelegatedTaskLifecycleRepository {
	return &DelegatedTaskLifecycleRepository{
		q:        q,
		events:   NewTaskEventsRepository(q),
		outcomes: NewDelegationRepository(q),
	}
}

func (r *DelegatedTaskLifecycleRepository) ListOpen(ctx context.Context, limit int) ([]DelegatedTaskSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.q.QueryContext(ctx, `
		SELECT t.id,
		       COALESCE(t.title, ''),
		       COALESCE(t.description, ''),
		       t.status,
		       t.priority,
		       COALESCE(t.source, ''),
		       t.created_at,
		       t.updated_at,
		       COALESCE((SELECT te.event_type
		                   FROM task_events te
		                  WHERE te.task_id = t.id
		                  ORDER BY te.created_at DESC
		                  LIMIT 1), ''),
		       COALESCE((SELECT te.created_at
		                   FROM task_events te
		                  WHERE te.task_id = t.id
		                  ORDER BY te.created_at DESC
		                  LIMIT 1), ''),
		       COALESCE((SELECT te.assigned_to
		                   FROM task_events te
		                  WHERE te.task_id = t.id AND te.assigned_to <> ''
		                  ORDER BY te.created_at DESC
		                  LIMIT 1), ''),
		       COALESCE((SELECT COUNT(*)
		                   FROM task_events te
		                  WHERE te.task_id = t.id), 0)
		  FROM tasks t
		 WHERE lower(t.status) NOT IN ('completed', 'done', 'finished', 'failed', 'error', 'cancelled', 'canceled')
		 ORDER BY t.updated_at DESC, t.created_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	summaries := make([]DelegatedTaskSummary, 0)
	for rows.Next() {
		var row DelegatedTaskSummary
		if err := rows.Scan(
			&row.ID,
			&row.Title,
			&row.Description,
			&row.Status,
			&row.Priority,
			&row.Source,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.LatestEventType,
			&row.LatestEventAt,
			&row.AssignedTo,
			&row.EventCount,
		); err != nil {
			return nil, err
		}
		summaries = append(summaries, row)
	}
	return summaries, rows.Err()
}

func (r *DelegatedTaskLifecycleRepository) GetStatus(ctx context.Context, taskID string, eventLimit int) (*DelegatedTaskStatus, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT t.id,
		       COALESCE(t.title, ''),
		       COALESCE(t.description, ''),
		       t.status,
		       t.priority,
		       COALESCE(t.source, ''),
		       t.created_at,
		       t.updated_at,
		       COALESCE((SELECT te.event_type
		                   FROM task_events te
		                  WHERE te.task_id = t.id
		                  ORDER BY te.created_at DESC
		                  LIMIT 1), ''),
		       COALESCE((SELECT te.created_at
		                   FROM task_events te
		                  WHERE te.task_id = t.id
		                  ORDER BY te.created_at DESC
		                  LIMIT 1), ''),
		       COALESCE((SELECT te.assigned_to
		                   FROM task_events te
		                  WHERE te.task_id = t.id AND te.assigned_to <> ''
		                  ORDER BY te.created_at DESC
		                  LIMIT 1), ''),
		       COALESCE((SELECT COUNT(*)
		                   FROM task_events te
		                  WHERE te.task_id = t.id), 0)
		  FROM tasks t
		 WHERE t.id = ?`, taskID)

	var summary DelegatedTaskSummary
	err := row.Scan(
		&summary.ID,
		&summary.Title,
		&summary.Description,
		&summary.Status,
		&summary.Priority,
		&summary.Source,
		&summary.CreatedAt,
		&summary.UpdatedAt,
		&summary.LatestEventType,
		&summary.LatestEventAt,
		&summary.AssignedTo,
		&summary.EventCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	events, err := r.events.ListRecent(ctx, taskID, eventLimit)
	if err != nil {
		return nil, err
	}
	outcomes, err := r.outcomes.List(ctx, taskID)
	if err != nil {
		return nil, err
	}

	return &DelegatedTaskStatus{
		Task:     summary,
		Events:   events,
		Outcomes: outcomes,
	}, nil
}

func (r *DelegatedTaskLifecycleRepository) Retry(ctx context.Context, taskID, reason, requestedBy string) (*DelegatedTaskRetryResult, error) {
	status, err := r.GetStatus(ctx, taskID, 20)
	if err != nil || status == nil {
		return nil, err
	}

	priorStatus := status.Task.Status
	if isOpenDelegatedTaskStatus(priorStatus) {
		return &DelegatedTaskRetryResult{
			Updated:     false,
			PriorStatus: priorStatus,
			Task:        status,
		}, nil
	}

	_, err = r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'pending', updated_at = datetime('now') WHERE id = ?`,
		taskID,
	)
	if err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(map[string]any{
		"reason":        strings.TrimSpace(reason),
		"prior_status":  priorStatus,
		"requested_by":  strings.TrimSpace(requestedBy),
		"retry_trigger": "runtime_tool",
	})
	if err := r.events.Append(ctx, TaskEventRow{
		ID:          uuid.NewString(),
		TaskID:      taskID,
		EventType:   "retry_requested",
		PayloadJSON: string(payload),
		AssignedTo:  strings.TrimSpace(requestedBy),
	}); err != nil {
		return nil, err
	}

	updated, err := r.GetStatus(ctx, taskID, 20)
	if err != nil {
		return nil, err
	}
	return &DelegatedTaskRetryResult{
		Updated:     true,
		PriorStatus: priorStatus,
		Task:        updated,
	}, nil
}

func isOpenDelegatedTaskStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending", "in_progress", "submitting", "queued", "running":
		return true
	default:
		return false
	}
}
