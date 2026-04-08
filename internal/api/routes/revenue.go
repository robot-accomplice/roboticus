package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
)

// ListServiceCatalog returns available services.
func ListServiceCatalog(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svcRepo := db.NewServiceRequestsRepository(store)
		services, err := svcRepo.ListServiceIDs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query service catalog")
			return
		}
		// Convert to API format.
		var result []map[string]any
		for _, s := range services {
			result = append(result, map[string]any{"service_id": s["service_id"]})
		}
		if result == nil {
			result = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, result)
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

		repo := db.NewRevenueRepository(store)
		filter := db.RevenueOpportunityFilter{Status: status, Limit: limit}
		rows, err := repo.ListOpportunities(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query revenue opportunities")
			return
		}
		var opps []map[string]any
		for _, row := range rows {
			opps = append(opps, map[string]any{
				"id": row.ID, "strategy": row.Strategy, "source": row.Source,
				"status": row.Status, "score": row.ConfidenceScore,
				"estimated_value_usd": row.ExpectedRevenueUSDC,
				"created_at":          row.CreatedAt,
			})
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
		repo := db.NewRevenueRepository(store)
		oppRow, err := repo.GetOpportunity(r.Context(), id)
		if err != nil || oppRow == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "opportunity not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": oppRow.ID, "strategy": oppRow.Strategy, "source": oppRow.Source,
			"status": oppRow.Status, "score": oppRow.ConfidenceScore,
			"estimated_value_usd": oppRow.ExpectedRevenueUSDC, "created_at": oppRow.CreatedAt,
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
		repo := db.NewRevenueRepository(store)
		if err := repo.CreateOpportunity(r.Context(), db.RevenueOpportunityRow{
			ID: id, Strategy: req.Strategy, Source: req.Source,
			Status: "intake", ConfidenceScore: req.Score, ExpectedRevenueUSDC: req.Value,
		}); err != nil {
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
		repo := db.NewRevenueRepository(store)
		if err := repo.UpdateOpportunityStatus(r.Context(), id, targetStatus); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "opportunity not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": targetStatus})
	}
}

// ListServiceRequests returns service requests.
func ListServiceRequests(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svcRepo := db.NewServiceRequestsRepository(store)
		reqs, err := svcRepo.List(r.Context(), 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query service requests")
			return
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
		svcRepo := db.NewServiceRequestsRepository(store)
		svcReq, err := svcRepo.Get(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "request not found"})
			return
		}
		writeJSON(w, http.StatusOK, svcReq)
	}
}

// TransitionServiceRequest moves a service request through its lifecycle.
func TransitionServiceRequest(store *db.Store, targetStatus string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		svcRepo := db.NewServiceRequestsRepository(store)
		if err := svcRepo.UpdateStatus(r.Context(), id, targetStatus); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "service request not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": targetStatus})
	}
}

// CreateServiceQuote inserts a service request with status "quoted".
func CreateServiceQuote(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ServiceID   string  `json:"service_id"`
			RequesterID string  `json:"requester_id"`
			AmountUSD   float64 `json:"amount_usd"`
			Description string  `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		id := db.NewID()
		svcRepo := db.NewServiceRequestsRepository(store)
		err := svcRepo.Create(r.Context(), db.ServiceRequestRow{
			ID: id, ServiceID: req.ServiceID, Requester: req.RequesterID,
			Status: "quoted", QuotedAmount: req.AmountUSD, Currency: "USDC",
			Recipient: req.RequesterID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "quoted"})
	}
}

// VerifyServicePayment transitions a service request to "payment_verified".
func VerifyServicePayment(store *db.Store) http.HandlerFunc {
	return TransitionServiceRequest(store, "payment_verified")
}

// FulfillServiceRequest transitions a service request to "fulfilled".
func FulfillServiceRequest(store *db.Store) http.HandlerFunc {
	return TransitionServiceRequest(store, "fulfilled")
}

// FailServiceRequest transitions a service request to "failed".
func FailServiceRequest(store *db.Store) http.HandlerFunc {
	return TransitionServiceRequest(store, "failed")
}

// RecordOpportunityFeedback records feedback for a revenue opportunity.
func RecordOpportunityFeedback(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		oppID := chi.URLParam(r, "id")
		var req struct {
			Strategy string  `json:"strategy"`
			Grade    float64 `json:"grade"`
			Source   string  `json:"source"`
			Comment  string  `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Strategy == "" {
			req.Strategy = "unknown"
		}
		if req.Source == "" {
			req.Source = "api"
		}
		id := db.NewID()
		repo := db.NewRevenueRepository(store)
		err := repo.CreateFeedback(r.Context(), db.RevenueFeedbackRow{
			ID: id, OpportunityID: oppID, Strategy: req.Strategy,
			Grade: req.Grade, Source: req.Source, Comment: req.Comment,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "opportunity_id": oppID})
	}
}

// IntakeMicroBounty creates a revenue opportunity with strategy "micro-bounty".
func IntakeMicroBounty(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Source string  `json:"source"`
			Score  float64 `json:"score"`
			Value  float64 `json:"estimated_value_usd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		id := db.NewID()
		repo := db.NewRevenueRepository(store)
		if err := repo.CreateOpportunity(r.Context(), db.RevenueOpportunityRow{
			ID: id, Strategy: "micro-bounty", Source: req.Source,
			Status: "intake", ConfidenceScore: req.Score, ExpectedRevenueUSDC: req.Value,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "strategy": "micro-bounty", "status": "intake"})
	}
}

// IntakeOracleFeed creates a revenue opportunity with strategy "oracle-feed".
func IntakeOracleFeed(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Source string  `json:"source"`
			Score  float64 `json:"score"`
			Value  float64 `json:"estimated_value_usd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		id := db.NewID()
		repo := db.NewRevenueRepository(store)
		if err := repo.CreateOpportunity(r.Context(), db.RevenueOpportunityRow{
			ID: id, Strategy: "oracle-feed", Source: req.Source,
			Status: "intake", ConfidenceScore: req.Score, ExpectedRevenueUSDC: req.Value,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "strategy": "oracle-feed", "status": "intake"})
	}
}
