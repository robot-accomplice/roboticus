package db

import (
	"context"
	"errors"
)

// ErrNoRowsAffected indicates an update/delete matched no rows.
var ErrNoRowsAffected = errors.New("no rows affected")

// ServiceRequestRow represents a row in the service_requests table.
type ServiceRequestRow struct {
	ID             string
	ServiceID      string
	Requester      string
	Status         string
	QuotedAmount   float64
	Currency       string
	Recipient      string
	ParametersJSON string
}

// ServiceRequestsRepository handles service request persistence.
type ServiceRequestsRepository struct {
	q Querier
}

// NewServiceRequestsRepository creates a service requests repository.
func NewServiceRequestsRepository(q Querier) *ServiceRequestsRepository {
	return &ServiceRequestsRepository{q: q}
}

// Create inserts a new service request.
func (r *ServiceRequestsRepository) Create(ctx context.Context, row ServiceRequestRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO service_requests (id, service_id, requester, status, quoted_amount, currency, recipient, parameters_json, quote_expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now', '+1 hour'))`,
		row.ID, row.ServiceID, row.Requester, row.Status, row.QuotedAmount,
		row.Currency, row.Recipient, row.ParametersJSON)
	return err
}

// List returns recent service requests as maps (for API formatting).
func (r *ServiceRequestsRepository) List(ctx context.Context, limit int) ([]map[string]any, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, service_id, requester, status, quoted_amount, currency, created_at
		 FROM service_requests ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []map[string]any
	for rows.Next() {
		var id, svcID, requester, status, currency, created string
		var amount float64
		if err := rows.Scan(&id, &svcID, &requester, &status, &amount, &currency, &created); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "service_id": svcID, "requester": requester,
			"status": status, "quoted_amount": amount, "currency": currency, "created_at": created,
		})
	}
	return result, nil
}

// Get returns a single service request by ID.
func (r *ServiceRequestsRepository) Get(ctx context.Context, id string) (map[string]any, error) {
	var svcID, requester, status, currency, created string
	var amount float64
	row := r.q.QueryRowContext(ctx,
		`SELECT service_id, requester, status, quoted_amount, currency, created_at
		 FROM service_requests WHERE id = ?`, id)
	if err := row.Scan(&svcID, &requester, &status, &amount, &currency, &created); err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "service_id": svcID, "requester": requester,
		"status": status, "quoted_amount": amount, "currency": currency, "created_at": created,
	}, nil
}

// ListServiceIDs returns distinct service IDs.
func (r *ServiceRequestsRepository) ListServiceIDs(ctx context.Context) ([]map[string]string, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT DISTINCT service_id FROM service_requests WHERE service_id IS NOT NULL AND service_id != ''`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []map[string]string
	for rows.Next() {
		var svcID string
		if err := rows.Scan(&svcID); err != nil {
			continue
		}
		result = append(result, map[string]string{"service_id": svcID})
	}
	return result, nil
}

// ListByServicePattern returns service requests matching a service_id LIKE pattern.
func (r *ServiceRequestsRepository) ListByServicePattern(ctx context.Context, pattern string, limit int) ([]map[string]any, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, service_id, status, quoted_amount, currency, created_at
		 FROM service_requests WHERE service_id LIKE ?
		 ORDER BY created_at DESC LIMIT ?`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []map[string]any
	for rows.Next() {
		var id, svcID, status, currency, created string
		var amount float64
		if err := rows.Scan(&id, &svcID, &status, &amount, &currency, &created); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "service_id": svcID, "status": status,
			"amount": amount, "currency": currency, "created_at": created,
		})
	}
	return result, nil
}

// UpdateStatus updates a service request's status. Returns ErrNotFound if no row matched.
func (r *ServiceRequestsRepository) UpdateStatus(ctx context.Context, id, status string) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE service_requests SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		status, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}
