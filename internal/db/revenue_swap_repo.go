// Revenue swap task lifecycle — operates on the tasks table,
// filtering by source JSON type "revenue_swap".
//
// Ported from Rust: crates/roboticus-db/src/revenue_swap_tasks.rs

package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// RevenueSwapTaskRow represents a revenue swap task from the tasks table.
type RevenueSwapTaskRow struct {
	ID            string
	OpportunityID string
	Title         string
	Status        string
	SourceJSON    string
	CreatedAt     string
	UpdatedAt     string
}

// RevenueSwapRepository manages revenue swap task lifecycle.
type RevenueSwapRepository struct {
	q Querier
}

// NewRevenueSwapRepository creates a revenue swap repository.
func NewRevenueSwapRepository(q Querier) *RevenueSwapRepository {
	return &RevenueSwapRepository{q: q}
}

// ListRevenueSwapTasks returns recent swap tasks filtered by source JSON type.
func (r *RevenueSwapRepository) ListRevenueSwapTasks(ctx context.Context, limit int) ([]RevenueSwapTaskRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, title, status, source, created_at, updated_at
		 FROM tasks
		 WHERE lower(COALESCE(source, '')) LIKE '%"type":"revenue_swap"%'
		 ORDER BY updated_at DESC, created_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []RevenueSwapTaskRow
	for rows.Next() {
		var t RevenueSwapTaskRow
		var source sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Status, &source, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.SourceJSON = source.String
		t.OpportunityID = t.ID
		result = append(result, t)
	}
	return result, rows.Err()
}

// GetRevenueSwapTask returns a single swap task by opportunity ID.
func (r *RevenueSwapRepository) GetRevenueSwapTask(ctx context.Context, opportunityID string) (*RevenueSwapTaskRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, title, status, source, created_at, updated_at
		 FROM tasks WHERE id = ?`, opportunityID)

	var t RevenueSwapTaskRow
	var source sql.NullString
	err := row.Scan(&t.ID, &t.Title, &t.Status, &source, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.SourceJSON = source.String
	t.OpportunityID = t.ID
	return &t, nil
}

// MarkRevenueSwapInProgress transitions a swap task from pending → in_progress.
func (r *RevenueSwapRepository) MarkRevenueSwapInProgress(ctx context.Context, opportunityID string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'in_progress', updated_at = datetime('now')
		 WHERE id = ? AND status = 'pending'`, opportunityID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClaimRevenueSwapSubmission transitions in_progress → submitting (optimistic lock).
func (r *RevenueSwapRepository) ClaimRevenueSwapSubmission(ctx context.Context, opportunityID string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'submitting', updated_at = datetime('now')
		 WHERE id = ? AND status = 'in_progress'`, opportunityID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReleaseRevenueSwapClaim reverts submitting → in_progress on submission failure.
func (r *RevenueSwapRepository) ReleaseRevenueSwapClaim(ctx context.Context, opportunityID string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'in_progress', updated_at = datetime('now')
		 WHERE id = ? AND status = 'submitting'`, opportunityID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkRevenueSwapFailed records a failure with reason.
func (r *RevenueSwapRepository) MarkRevenueSwapFailed(ctx context.Context, opportunityID, reason string) (bool, error) {
	return r.updateRevenueSwapStatus(ctx, opportunityID, "failed", &reason, nil, []string{"in_progress", "submitting"})
}

// MarkRevenueSwapConfirmed marks a swap as confirmed with tx hash.
func (r *RevenueSwapRepository) MarkRevenueSwapConfirmed(ctx context.Context, opportunityID, txHash string) (bool, error) {
	return r.updateRevenueSwapStatus(ctx, opportunityID, "confirmed", nil, &txHash, []string{"submitted", "submitting"})
}

// MarkRevenueSwapSubmitted marks a swap as submitted with tx hash.
func (r *RevenueSwapRepository) MarkRevenueSwapSubmitted(ctx context.Context, opportunityID, txHash string) (bool, error) {
	return r.updateRevenueSwapStatus(ctx, opportunityID, "submitted", nil, &txHash, []string{"submitting"})
}

// updateRevenueSwapStatus is the internal helper that validates current status,
// merges metadata into the source JSON, and atomically updates.
func (r *RevenueSwapRepository) updateRevenueSwapStatus(
	ctx context.Context,
	opportunityID, newStatus string,
	failureReason, txHash *string,
	allowedFromStatuses []string,
) (bool, error) {
	// Read current state.
	row := r.q.QueryRowContext(ctx,
		`SELECT source, status FROM tasks WHERE id = ?`, opportunityID)

	var sourceRaw sql.NullString
	var currentStatus string
	if err := row.Scan(&sourceRaw, &currentStatus); err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("swap task %s not found", opportunityID)
		}
		return false, err
	}

	// Validate status transition.
	allowed := false
	for _, s := range allowedFromStatuses {
		if currentStatus == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return false, fmt.Errorf("swap task %s in status %q, expected one of [%s]",
			opportunityID, currentStatus, strings.Join(allowedFromStatuses, ", "))
	}

	// Merge metadata into source JSON.
	var source map[string]any
	if sourceRaw.Valid && sourceRaw.String != "" {
		if err := json.Unmarshal([]byte(sourceRaw.String), &source); err != nil {
			source = make(map[string]any)
		}
	} else {
		source = make(map[string]any)
	}

	if failureReason != nil {
		source["failure_reason"] = *failureReason
	}
	if txHash != nil {
		source["swap_tx_hash"] = *txHash
	}

	updatedSource, err := json.Marshal(source)
	if err != nil {
		return false, err
	}

	// Atomic update with status guard.
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = ?, source = ?, updated_at = datetime('now')
		 WHERE id = ? AND status = ?`,
		newStatus, string(updatedSource), opportunityID, currentStatus)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
