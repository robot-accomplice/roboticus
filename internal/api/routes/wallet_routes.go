package routes

import (
	"database/sql"
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
		if err := row.Scan(&balance); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query wallet balance")
			return
		}

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
		if err := row.Scan(&address); err != nil {
			if err != sql.ErrNoRows {
				writeError(w, http.StatusInternalServerError, "failed to query wallet address")
				return
			}
		}

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
			writeError(w, http.StatusInternalServerError, "failed to query swap tasks")
			return
		}
		defer func() { _ = rows.Close() }()

		tasks := make([]map[string]any, 0)
		for rows.Next() {
			var id, serviceID, status, currency, createdAt string
			var amount float64
			if err := rows.Scan(&id, &serviceID, &status, &amount, &currency, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read swap task row")
				return
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
			writeError(w, http.StatusInternalServerError, "failed to query tax tasks")
			return
		}
		defer func() { _ = rows.Close() }()

		tasks := make([]map[string]any, 0)
		for rows.Next() {
			var id, serviceID, status, currency, createdAt string
			var amount float64
			if err := rows.Scan(&id, &serviceID, &status, &amount, &currency, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read tax task row")
				return
			}
			tasks = append(tasks, map[string]any{
				"id": id, "service_id": serviceID, "status": status,
				"amount": amount, "currency": currency, "created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tax_tasks": tasks})
	}
}
