package routes

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/db"
)

// ListServiceCatalog returns available services.
func ListServiceCatalog(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT DISTINCT service_id, description FROM service_requests
			 GROUP BY service_id ORDER BY COUNT(*) DESC LIMIT 50`)
		if err != nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		defer func() { _ = rows.Close() }()

		var services []map[string]any
		for rows.Next() {
			var id, desc string
			if rows.Scan(&id, &desc) == nil {
				services = append(services, map[string]any{"service_id": id, "description": desc})
			}
		}
		if services == nil {
			services = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, services)
	}
}

// ListRevenueOpportunities returns revenue opportunities with optional status filter.
func ListRevenueOpportunities(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		limit := 50
		if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
			limit = v
		}

		var rows queryResult
		var err error
		if status != "" {
			rows, err = store.QueryContext(r.Context(),
				`SELECT id, strategy, source, status, score, estimated_value_usd,
				        plan_json, result_json, created_at
				 FROM revenue_opportunities WHERE status = ?
				 ORDER BY created_at DESC LIMIT ?`, status, limit)
		} else {
			rows, err = store.QueryContext(r.Context(),
				`SELECT id, strategy, source, status, score, estimated_value_usd,
				        plan_json, result_json, created_at
				 FROM revenue_opportunities
				 ORDER BY created_at DESC LIMIT ?`, limit)
		}
		if err != nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		defer func() { _ = rows.Close() }()

		var opps []map[string]any
		for rows.Next() {
			var id, strategy, source, st, planJSON, resultJSON, createdAt string
			var score, value float64
			if rows.Scan(&id, &strategy, &source, &st, &score, &value, &planJSON, &resultJSON, &createdAt) == nil {
				opps = append(opps, map[string]any{
					"id": id, "strategy": strategy, "source": source,
					"status": st, "score": score, "estimated_value_usd": value,
					"created_at": createdAt,
				})
			}
		}
		if opps == nil {
			opps = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, opps)
	}
}

// GetRevenueOpportunity returns a single opportunity by ID.
func GetRevenueOpportunity(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, strategy, source, status, score, estimated_value_usd,
			        plan_json, result_json, created_at
			 FROM revenue_opportunities WHERE id = ?`, id)

		var opp struct {
			ID, Strategy, Source, Status, PlanJSON, ResultJSON, CreatedAt string
			Score, Value                                                  float64
		}
		if err := row.Scan(&opp.ID, &opp.Strategy, &opp.Source, &opp.Status,
			&opp.Score, &opp.Value, &opp.PlanJSON, &opp.ResultJSON, &opp.CreatedAt); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "opportunity not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": opp.ID, "strategy": opp.Strategy, "source": opp.Source,
			"status": opp.Status, "score": opp.Score, "estimated_value_usd": opp.Value,
			"plan_json": opp.PlanJSON, "result_json": opp.ResultJSON, "created_at": opp.CreatedAt,
		})
	}
}

// IntakeRevenueOpportunity creates a new revenue opportunity.
func IntakeRevenueOpportunity(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Strategy string  `json:"strategy"`
			Source   string  `json:"source"`
			Score    float64 `json:"score"`
			Value    float64 `json:"estimated_value_usd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		id := db.NewID()
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO revenue_opportunities (id, strategy, source, status, score, estimated_value_usd)
			 VALUES (?, ?, ?, 'intake', ?, ?)`,
			id, req.Strategy, req.Source, req.Score, req.Value)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "intake"})
	}
}

// TransitionOpportunity moves an opportunity through its lifecycle.
func TransitionOpportunity(store *db.Store, targetStatus string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		result, err := store.ExecContext(r.Context(),
			`UPDATE revenue_opportunities SET status = ?, updated_at = datetime('now') WHERE id = ?`,
			targetStatus, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "opportunity not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": targetStatus})
	}
}

// ListServiceRequests returns service requests.
func ListServiceRequests(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, service_id, requester_id, status, amount_usd, created_at
			 FROM service_requests ORDER BY created_at DESC LIMIT 50`)
		if err != nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		defer func() { _ = rows.Close() }()

		var reqs []map[string]any
		for rows.Next() {
			var id, svcID, requester, status, createdAt string
			var amount float64
			if rows.Scan(&id, &svcID, &requester, &status, &amount, &createdAt) == nil {
				reqs = append(reqs, map[string]any{
					"id": id, "service_id": svcID, "requester_id": requester,
					"status": status, "amount_usd": amount, "created_at": createdAt,
				})
			}
		}
		if reqs == nil {
			reqs = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, reqs)
	}
}

// GetServiceRequest returns a single service request.
func GetServiceRequest(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, service_id, requester_id, status, amount_usd, created_at
			 FROM service_requests WHERE id = ?`, id)

		var svcID, requester, status, createdAt string
		var amount float64
		if err := row.Scan(&id, &svcID, &requester, &status, &amount, &createdAt); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "request not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "service_id": svcID, "requester_id": requester,
			"status": status, "amount_usd": amount, "created_at": createdAt,
		})
	}
}

// TransitionServiceRequest moves a service request through its lifecycle.
func TransitionServiceRequest(store *db.Store, targetStatus string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		result, err := store.ExecContext(r.Context(),
			`UPDATE service_requests SET status = ?, updated_at = datetime('now') WHERE id = ?`,
			targetStatus, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "service request not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": targetStatus})
	}
}

// queryResult matches *sql.Rows for testability.
type queryResult interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
}
