package db

import (
	"context"
	"database/sql"
)

// DeliveryRow represents a row in the delivery_queue table.
type DeliveryRow struct {
	ID             string
	Channel        string
	RecipientID    string
	Content        string
	IdempotencyKey string
	Status         string // "pending", "in_flight", "delivered", "dead_letter"
	Attempts       int
	MaxAttempts    int
	NextRetryAt    string
	LastError      string
	CreatedAt      string
}

// DeliveryRepository handles outbound message delivery queue persistence.
type DeliveryRepository struct {
	q Querier
}

// NewDeliveryRepository creates a delivery repository.
func NewDeliveryRepository(q Querier) *DeliveryRepository {
	return &DeliveryRepository{q: q}
}

// Enqueue inserts a new delivery item with pending status.
func (r *DeliveryRepository) Enqueue(ctx context.Context, row DeliveryRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO delivery_queue
		 (id, channel, recipient_id, content, idempotency_key, max_attempts)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		row.ID, row.Channel, row.RecipientID, row.Content,
		row.IdempotencyKey, row.MaxAttempts)
	return err
}

// ListPending returns items ready to deliver (status=pending or in_flight past retry time), up to limit.
func (r *DeliveryRepository) ListPending(ctx context.Context, limit int) ([]DeliveryRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, channel, recipient_id, content, idempotency_key, status,
		        attempts, max_attempts, next_retry_at, COALESCE(last_error,''), created_at
		 FROM delivery_queue
		 WHERE status IN ('pending', 'in_flight') AND next_retry_at <= datetime('now')
		 ORDER BY next_retry_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDeliveryRows(rows)
}

// MarkInFlight transitions a delivery item to in_flight.
func (r *DeliveryRepository) MarkInFlight(ctx context.Context, id string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE delivery_queue SET status = 'in_flight', attempts = attempts + 1 WHERE id = ?`, id)
	return err
}

// MarkDelivered marks a delivery item as successfully delivered.
func (r *DeliveryRepository) MarkDelivered(ctx context.Context, id string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE delivery_queue SET status = 'delivered' WHERE id = ?`, id)
	return err
}

// MarkFailed records a delivery failure and reschedules or dead-letters.
func (r *DeliveryRepository) MarkFailed(ctx context.Context, id, errMsg, nextRetryAt string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE delivery_queue
		 SET last_error = ?,
		     next_retry_at = ?,
		     status = CASE WHEN attempts >= max_attempts THEN 'dead_letter' ELSE 'pending' END
		 WHERE id = ?`,
		errMsg, nextRetryAt, id)
	return err
}

// GetByID retrieves a delivery item by ID. Returns nil if not found.
func (r *DeliveryRepository) GetByID(ctx context.Context, id string) (*DeliveryRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, channel, recipient_id, content, idempotency_key, status,
		        attempts, max_attempts, next_retry_at, COALESCE(last_error,''), created_at
		 FROM delivery_queue WHERE id = ?`, id)
	var d DeliveryRow
	err := row.Scan(&d.ID, &d.Channel, &d.RecipientID, &d.Content, &d.IdempotencyKey,
		&d.Status, &d.Attempts, &d.MaxAttempts, &d.NextRetryAt, &d.LastError, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// CountByStatus returns the count of items in each status.
func (r *DeliveryRepository) CountByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM delivery_queue GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[status] = count
	}
	return result, rows.Err()
}

// ReplayDeadLetter resets a dead-lettered item to pending for retry.
func (r *DeliveryRepository) ReplayDeadLetter(ctx context.Context, id string) (int64, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE delivery_queue SET status = 'pending', last_error = NULL WHERE id = ? AND status = 'dead_letter'`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanDeliveryRows(rows *sql.Rows) ([]DeliveryRow, error) {
	var result []DeliveryRow
	for rows.Next() {
		var d DeliveryRow
		if err := rows.Scan(&d.ID, &d.Channel, &d.RecipientID, &d.Content, &d.IdempotencyKey,
			&d.Status, &d.Attempts, &d.MaxAttempts, &d.NextRetryAt, &d.LastError, &d.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
