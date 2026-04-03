package db

import (
	"context"
	"database/sql"
)

// ApprovalRow represents a row in the approval_requests table.
type ApprovalRow struct {
	ID        string
	ToolName  string
	ToolInput string
	SessionID string
	Status    string // "pending", "approved", "denied", "timed_out"
	DecidedBy string
	DecidedAt string
	TimeoutAt string
	CreatedAt string
}

// ApprovalsRepository handles tool-call approval queue persistence.
type ApprovalsRepository struct {
	q Querier
}

// NewApprovalsRepository creates an approvals repository.
func NewApprovalsRepository(q Querier) *ApprovalsRepository {
	return &ApprovalsRepository{q: q}
}

// Create inserts a new pending approval request.
func (r *ApprovalsRepository) Create(ctx context.Context, row ApprovalRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO approval_requests (id, tool_name, tool_input, session_id, status, timeout_at)
		 VALUES (?, ?, ?, ?, 'pending', ?)`,
		row.ID, row.ToolName, row.ToolInput, row.SessionID, row.TimeoutAt)
	return err
}

// ListPending returns all pending approval requests ordered by creation time.
func (r *ApprovalsRepository) ListPending(ctx context.Context) ([]ApprovalRow, error) {
	return r.listByStatus(ctx, "pending")
}

// Get retrieves a single approval request by ID. Returns nil if not found.
func (r *ApprovalsRepository) Get(ctx context.Context, id string) (*ApprovalRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, tool_name, tool_input, COALESCE(session_id, ''), status,
		        COALESCE(decided_by, ''), COALESCE(decided_at, ''), timeout_at, created_at
		 FROM approval_requests WHERE id = ?`, id)
	var a ApprovalRow
	err := row.Scan(&a.ID, &a.ToolName, &a.ToolInput, &a.SessionID, &a.Status,
		&a.DecidedBy, &a.DecidedAt, &a.TimeoutAt, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Approve marks a request as approved by the given reviewer.
func (r *ApprovalsRepository) Approve(ctx context.Context, id, decidedBy string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE approval_requests
		 SET status = 'approved', decided_by = ?, decided_at = datetime('now')
		 WHERE id = ?`,
		decidedBy, id)
	return err
}

// Deny marks a request as denied by the given reviewer.
func (r *ApprovalsRepository) Deny(ctx context.Context, id, decidedBy string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE approval_requests
		 SET status = 'denied', decided_by = ?, decided_at = datetime('now')
		 WHERE id = ?`,
		decidedBy, id)
	return err
}

// listByStatus is a shared helper for filtering by status.
func (r *ApprovalsRepository) listByStatus(ctx context.Context, status string) ([]ApprovalRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, tool_name, tool_input, COALESCE(session_id, ''), status,
		        COALESCE(decided_by, ''), COALESCE(decided_at, ''), timeout_at, created_at
		 FROM approval_requests
		 WHERE status = ?
		 ORDER BY created_at ASC`, status)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []ApprovalRow
	for rows.Next() {
		var a ApprovalRow
		if err := rows.Scan(&a.ID, &a.ToolName, &a.ToolInput, &a.SessionID, &a.Status,
			&a.DecidedBy, &a.DecidedAt, &a.TimeoutAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
