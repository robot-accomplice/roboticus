package db

import (
	"context"
	"fmt"
	"time"
)

// RevenueAccountingRepository handles revenue transaction persistence.
type RevenueAccountingRepository struct {
	q Querier
}

// NewRevenueAccountingRepository creates a revenue accounting repository.
func NewRevenueAccountingRepository(q Querier) *RevenueAccountingRepository {
	return &RevenueAccountingRepository{q: q}
}

// RecordTransaction inserts a revenue transaction.
func (r *RevenueAccountingRepository) RecordTransaction(ctx context.Context, oppID, txType string, amount float64) error {
	id := fmt.Sprintf("tx-%d", time.Now().UnixNano())
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO transactions (id, tx_type, amount, counterparty)
		 VALUES (?, ?, ?, ?)`,
		id, txType, amount, oppID,
	)
	return err
}

// SumByType returns the sum of transaction amounts for a given type.
func (r *RevenueAccountingRepository) SumByType(ctx context.Context, txType string) (float64, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0.0) FROM transactions WHERE tx_type = ?`,
		txType,
	)
	var total float64
	err := row.Scan(&total)
	return total, err
}
