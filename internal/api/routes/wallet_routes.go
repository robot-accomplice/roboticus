package routes

import (
	"net/http"

	"goboticus/internal/db"
)

// GetWalletBalance returns wallet balance from the transactions table.
func GetWalletBalance(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Calculate balance from transaction history.
		var balance float64
		row := store.QueryRowContext(r.Context(),
			`SELECT COALESCE(SUM(CASE WHEN tx_type = 'credit' THEN amount ELSE -amount END), 0)
			 FROM transactions`)
		_ = row.Scan(&balance)

		writeJSON(w, http.StatusOK, map[string]any{
			"balance":  balance,
			"currency": "USDC",
			"network":  "Base",
			"chain_id": baseChainID,
		})
	}
}

// GetWalletAddress returns wallet address from the identity table.
func GetWalletAddress(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var address string
		row := store.QueryRowContext(r.Context(),
			`SELECT value FROM identity WHERE key = 'wallet_address'`)
		_ = row.Scan(&address)

		writeJSON(w, http.StatusOK, map[string]any{
			"address":  address,
			"chain_id": baseChainID,
			"network":  "Base",
		})
	}
}

// GetSwaps returns swap service tasks from service_requests.
func GetSwaps(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, service_id, status, quoted_amount, currency, created_at
			 FROM service_requests WHERE service_id LIKE '%swap%'
			 ORDER BY created_at DESC LIMIT 20`)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"swap_tasks": make([]any, 0)})
			return
		}
		defer func() { _ = rows.Close() }()

		tasks := make([]map[string]any, 0)
		for rows.Next() {
			var id, serviceID, status, currency, createdAt string
			var amount float64
			if err := rows.Scan(&id, &serviceID, &status, &amount, &currency, &createdAt); err != nil {
				continue
			}
			tasks = append(tasks, map[string]any{
				"id": id, "service_id": serviceID, "status": status,
				"amount": amount, "currency": currency, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"swap_tasks": tasks})
	}
}

// GetTaxPayouts returns tax payout tasks from service_requests.
func GetTaxPayouts(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, service_id, status, quoted_amount, currency, created_at
			 FROM service_requests WHERE service_id LIKE '%tax%'
			 ORDER BY created_at DESC LIMIT 20`)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"tax_tasks": make([]any, 0)})
			return
		}
		defer func() { _ = rows.Close() }()

		tasks := make([]map[string]any, 0)
		for rows.Next() {
			var id, serviceID, status, currency, createdAt string
			var amount float64
			if err := rows.Scan(&id, &serviceID, &status, &amount, &currency, &createdAt); err != nil {
				continue
			}
			tasks = append(tasks, map[string]any{
				"id": id, "service_id": serviceID, "status": status,
				"amount": amount, "currency": currency, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tax_tasks": tasks})
	}
}
