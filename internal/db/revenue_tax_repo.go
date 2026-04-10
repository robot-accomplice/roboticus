// Revenue tax task lifecycle — operates on the tasks table,
// filtering by source JSON type "revenue_tax_payout".
//
// Ported from Rust: crates/roboticus-db/src/revenue_tax_tasks.rs

package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// RevenueTaxTaskRow represents a revenue tax payout task from the tasks table.
type RevenueTaxTaskRow struct {
	ID            string
	OpportunityID string
	Title         string
	Status        string
	SourceJSON    string
	CreatedAt     string
	UpdatedAt     string
}

// RevenueTaxRepository manages revenue tax task lifecycle.
type RevenueTaxRepository struct {
	q Querier
}

// NewRevenueTaxRepository creates a revenue tax repository.
func NewRevenueTaxRepository(q Querier) *RevenueTaxRepository {
	return &RevenueTaxRepository{q: q}
}

// ListRevenueTaxTasks returns recent tax payout tasks filtered by source JSON type.
func (r *RevenueTaxRepository) ListRevenueTaxTasks(ctx context.Context, limit int) ([]RevenueTaxTaskRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, title, status, source, created_at, updated_at
		 FROM tasks
		 WHERE lower(COALESCE(source, '')) LIKE '%"type":"revenue_tax_payout"%'
		 ORDER BY updated_at DESC, created_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []RevenueTaxTaskRow
	for rows.Next() {
		var t RevenueTaxTaskRow
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

// GetRevenueTaxTask returns a single tax task by opportunity ID.
func (r *RevenueTaxRepository) GetRevenueTaxTask(ctx context.Context, opportunityID string) (*RevenueTaxTaskRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, title, status, source, created_at, updated_at
		 FROM tasks WHERE id = ?`, opportunityID)

	var t RevenueTaxTaskRow
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

// MarkRevenueTaxInProgress transitions a tax task from pending → in_progress.
func (r *RevenueTaxRepository) MarkRevenueTaxInProgress(ctx context.Context, opportunityID string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'in_progress', updated_at = datetime('now')
		 WHERE id = ? AND status = 'pending'`, opportunityID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClaimRevenueTaxSubmission transitions in_progress → submitting (optimistic lock).
func (r *RevenueTaxRepository) ClaimRevenueTaxSubmission(ctx context.Context, opportunityID string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'submitting', updated_at = datetime('now')
		 WHERE id = ? AND status = 'in_progress'`, opportunityID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReleaseRevenueTaxClaim reverts submitting → in_progress on submission failure.
func (r *RevenueTaxRepository) ReleaseRevenueTaxClaim(ctx context.Context, opportunityID string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'in_progress', updated_at = datetime('now')
		 WHERE id = ? AND status = 'submitting'`, opportunityID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkRevenueTaxFailed records a failure with reason.
func (r *RevenueTaxRepository) MarkRevenueTaxFailed(ctx context.Context, opportunityID, reason string) (bool, error) {
	return r.updateRevenueTaxStatus(ctx, opportunityID, "failed", &reason, nil, []string{"in_progress", "submitting"})
}

// MarkRevenueTaxConfirmed marks a tax payout as confirmed with tx hash.
func (r *RevenueTaxRepository) MarkRevenueTaxConfirmed(ctx context.Context, opportunityID, txHash string) (bool, error) {
	return r.updateRevenueTaxStatus(ctx, opportunityID, "confirmed", nil, &txHash, []string{"submitted", "submitting"})
}

// MarkRevenueTaxSubmitted marks a tax payout as submitted with tx hash.
func (r *RevenueTaxRepository) MarkRevenueTaxSubmitted(ctx context.Context, opportunityID, txHash string) (bool, error) {
	return r.updateRevenueTaxStatus(ctx, opportunityID, "submitted", nil, &txHash, []string{"submitting"})
}

// updateRevenueTaxStatus validates current status, merges metadata into source JSON, and updates atomically.
func (r *RevenueTaxRepository) updateRevenueTaxStatus(
	ctx context.Context,
	opportunityID, newStatus string,
	failureReason, txHash *string,
	allowedFromStatuses []string,
) (bool, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT source, status FROM tasks WHERE id = ?`, opportunityID)

	var sourceRaw sql.NullString
	var currentStatus string
	if err := row.Scan(&sourceRaw, &currentStatus); err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("tax task %s not found", opportunityID)
		}
		return false, err
	}

	allowed := false
	for _, s := range allowedFromStatuses {
		if currentStatus == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return false, fmt.Errorf("tax task %s in status %q, expected one of [%s]",
			opportunityID, currentStatus, strings.Join(allowedFromStatuses, ", "))
	}

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
		source["tax_tx_hash"] = *txHash
	}

	updatedSource, err := json.Marshal(source)
	if err != nil {
		return false, err
	}

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
