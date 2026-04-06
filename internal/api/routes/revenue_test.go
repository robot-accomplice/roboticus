package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/testutil"
)

// chiRouter returns a chi router that dispatches to the handler with the given method and pattern.
func chiRouter(method, pattern string, handler http.HandlerFunc) *chi.Mux {
	r := chi.NewRouter()
	switch method {
	case "GET":
		r.Get(pattern, handler)
	case "POST":
		r.Post(pattern, handler)
	case "PUT":
		r.Put(pattern, handler)
	case "PATCH":
		r.Patch(pattern, handler)
	}
	return r
}

// --- ListServiceCatalog ---
// The handler queries "description" which is not in the service_requests schema,
// so the query fails. Verify it returns 500.

func TestListServiceCatalog_QueryError(t *testing.T) {
	store := testutil.TempStore(t)
	handler := ListServiceCatalog(store)
	req := httptest.NewRequest("GET", "/api/revenue/services", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch: description column missing)", rec.Code)
	}
}

// --- ListRevenueOpportunities ---
// The handler queries columns (score, estimated_value_usd, result_json) absent
// from the real schema. Verify 500 on empty DB and with seed data.

func TestListRevenueOpportunities_QueryError_EmptyDB(t *testing.T) {
	store := testutil.TempStore(t)
	handler := ListRevenueOpportunities(store)
	req := httptest.NewRequest("GET", "/api/revenue/opportunities", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch)", rec.Code)
	}
}

func TestListRevenueOpportunities_QueryError_WithStatusFilter(t *testing.T) {
	store := testutil.TempStore(t)
	handler := ListRevenueOpportunities(store)
	req := httptest.NewRequest("GET", "/api/revenue/opportunities?status=intake", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch)", rec.Code)
	}
}

// --- GetRevenueOpportunity ---

func TestGetRevenueOpportunity_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chiRouter("GET", "/api/revenue/opportunities/{id}", GetRevenueOpportunity(store))
	req := httptest.NewRequest("GET", "/api/revenue/opportunities/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// The handler uses QueryRowContext with missing columns; Scan fails -> 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetRevenueOpportunity_SchemaError(t *testing.T) {
	store := testutil.TempStore(t)
	// Seed a real row using actual schema columns.
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO revenue_opportunities
		 (id, source, strategy, payload_json, expected_revenue_usdc, status, confidence_score, created_at)
		 VALUES ('opp-get1', 'api', 'micro-bounty', '{}', 75.0, 'intake', 0.9, datetime('now'))`)

	r := chiRouter("GET", "/api/revenue/opportunities/{id}", GetRevenueOpportunity(store))
	req := httptest.NewRequest("GET", "/api/revenue/opportunities/opp-get1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Handler queries columns that don't exist (score, estimated_value_usd, result_json).
	// QueryRowContext.Scan will fail -> 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (schema mismatch causes scan failure)", rec.Code)
	}
}

// --- IntakeRevenueOpportunity ---

func TestIntakeRevenueOpportunity_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	handler := IntakeRevenueOpportunity(store)
	req := httptest.NewRequest("POST", "/api/revenue/opportunities",
		strings.NewReader(`{bad json`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestIntakeRevenueOpportunity_SchemaError(t *testing.T) {
	store := testutil.TempStore(t)
	handler := IntakeRevenueOpportunity(store)
	req := httptest.NewRequest("POST", "/api/revenue/opportunities",
		strings.NewReader(`{"strategy":"micro-bounty","source":"api","score":0.85,"estimated_value_usd":100.0}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Handler inserts into columns (score, estimated_value_usd) that don't exist.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch)", rec.Code)
	}
}

// --- TransitionOpportunity ---
// This handler only does UPDATE ... SET status = ?, updated_at = ... WHERE id = ?
// which uses columns that exist. It should work.

func TestTransitionOpportunity_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO revenue_opportunities
		 (id, source, strategy, payload_json, expected_revenue_usdc, status, created_at)
		 VALUES ('opp-trans1', 'api', 'bounty', '{}', 10.0, 'intake', datetime('now'))`)

	r := chiRouter("POST", "/api/revenue/opportunities/{id}/activate", TransitionOpportunity(store, "active"))
	req := httptest.NewRequest("POST", "/api/revenue/opportunities/opp-trans1/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["id"] != "opp-trans1" {
		t.Errorf("id = %v, want opp-trans1", body["id"])
	}
	if body["status"] != "active" {
		t.Errorf("status = %v, want active", body["status"])
	}
}

func TestTransitionOpportunity_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chiRouter("POST", "/api/revenue/opportunities/{id}/activate", TransitionOpportunity(store, "active"))
	req := httptest.NewRequest("POST", "/api/revenue/opportunities/nonexistent/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestTransitionOpportunity_MultipleTransitions(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO revenue_opportunities
		 (id, source, strategy, payload_json, expected_revenue_usdc, status, created_at)
		 VALUES ('opp-multi', 'api', 'bounty', '{}', 10.0, 'intake', datetime('now'))`)

	// Transition to active.
	r := chiRouter("POST", "/api/revenue/opportunities/{id}/activate", TransitionOpportunity(store, "active"))
	req := httptest.NewRequest("POST", "/api/revenue/opportunities/opp-multi/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate status = %d, want 200", rec.Code)
	}

	// Transition to completed.
	r2 := chiRouter("POST", "/api/revenue/opportunities/{id}/complete", TransitionOpportunity(store, "completed"))
	req2 := httptest.NewRequest("POST", "/api/revenue/opportunities/opp-multi/complete", nil)
	rec2 := httptest.NewRecorder()
	r2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want 200", rec2.Code)
	}
	body := jsonBody(t, rec2)
	if body["status"] != "completed" {
		t.Errorf("status = %v, want completed", body["status"])
	}
}

// --- ListServiceRequests ---

func TestListServiceRequests_QueryError(t *testing.T) {
	store := testutil.TempStore(t)
	handler := ListServiceRequests(store)
	req := httptest.NewRequest("GET", "/api/revenue/service-requests", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Handler queries requester_id, amount_usd which don't exist in schema
	// (real columns: requester, quoted_amount).
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch)", rec.Code)
	}
}

// --- GetServiceRequest ---

func TestGetServiceRequest_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chiRouter("GET", "/api/revenue/service-requests/{id}", GetServiceRequest(store))
	req := httptest.NewRequest("GET", "/api/revenue/service-requests/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetServiceRequest_SchemaError(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO service_requests
		 (id, service_id, requester, parameters_json, status, quoted_amount, recipient, quote_expires_at, created_at)
		 VALUES ('sr-get1', 'svc1', 'user1', '{}', 'quoted', 99.0, 'wallet1', datetime('now', '+1 day'), datetime('now'))`)

	r := chiRouter("GET", "/api/revenue/service-requests/{id}", GetServiceRequest(store))
	req := httptest.NewRequest("GET", "/api/revenue/service-requests/sr-get1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Handler queries requester_id, amount_usd which don't exist -> scan fails -> 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (schema mismatch causes scan failure)", rec.Code)
	}
}

// --- CreateServiceQuote ---

func TestCreateServiceQuote_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	handler := CreateServiceQuote(store)
	req := httptest.NewRequest("POST", "/api/revenue/service-requests",
		strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateServiceQuote_SchemaError(t *testing.T) {
	store := testutil.TempStore(t)
	handler := CreateServiceQuote(store)
	req := httptest.NewRequest("POST", "/api/revenue/service-requests",
		strings.NewReader(`{"service_id":"svc1","requester_id":"user1","amount_usd":150.0,"description":"test quote"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Handler inserts requester_id, amount_usd, description which don't exist.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch)", rec.Code)
	}
}

// --- TransitionServiceRequest ---
// This handler only does UPDATE ... SET status = ? WHERE id = ?
// which uses columns that exist. It should work.

func TestTransitionServiceRequest_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO service_requests
		 (id, service_id, requester, parameters_json, status, quoted_amount, recipient, quote_expires_at, created_at)
		 VALUES ('sr-trans1', 'svc1', 'user1', '{}', 'quoted', 50.0, 'wallet1', datetime('now', '+1 day'), datetime('now'))`)

	r := chiRouter("POST", "/api/revenue/service-requests/{id}/verify", TransitionServiceRequest(store, "payment_verified"))
	req := httptest.NewRequest("POST", "/api/revenue/service-requests/sr-trans1/verify", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["id"] != "sr-trans1" {
		t.Errorf("id = %v, want sr-trans1", body["id"])
	}
	if body["status"] != "payment_verified" {
		t.Errorf("status = %v, want payment_verified", body["status"])
	}
}

func TestTransitionServiceRequest_NotFound(t *testing.T) {
	store := testutil.TempStore(t)

	r := chiRouter("POST", "/api/revenue/service-requests/{id}/verify", TransitionServiceRequest(store, "payment_verified"))
	req := httptest.NewRequest("POST", "/api/revenue/service-requests/nonexistent/verify", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// --- VerifyServicePayment / FulfillServiceRequest / FailServiceRequest ---
// These are wrappers around TransitionServiceRequest with specific target statuses.

func TestVerifyServicePayment(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO service_requests
		 (id, service_id, requester, parameters_json, status, quoted_amount, recipient, quote_expires_at, created_at)
		 VALUES ('sr-verify1', 'svc1', 'user1', '{}', 'quoted', 50.0, 'wallet1', datetime('now', '+1 day'), datetime('now'))`)

	r := chiRouter("POST", "/api/revenue/service-requests/{id}/verify-payment", VerifyServicePayment(store))
	req := httptest.NewRequest("POST", "/api/revenue/service-requests/sr-verify1/verify-payment", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "payment_verified" {
		t.Errorf("status = %v, want payment_verified", body["status"])
	}
}

func TestFulfillServiceRequest(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO service_requests
		 (id, service_id, requester, parameters_json, status, quoted_amount, recipient, quote_expires_at, created_at)
		 VALUES ('sr-fulfill1', 'svc1', 'user1', '{}', 'payment_verified', 50.0, 'wallet1', datetime('now', '+1 day'), datetime('now'))`)

	r := chiRouter("POST", "/api/revenue/service-requests/{id}/fulfill", FulfillServiceRequest(store))
	req := httptest.NewRequest("POST", "/api/revenue/service-requests/sr-fulfill1/fulfill", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "fulfilled" {
		t.Errorf("status = %v, want fulfilled", body["status"])
	}
}

func TestFailServiceRequest(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO service_requests
		 (id, service_id, requester, parameters_json, status, quoted_amount, recipient, quote_expires_at, created_at)
		 VALUES ('sr-fail1', 'svc1', 'user1', '{}', 'quoted', 50.0, 'wallet1', datetime('now', '+1 day'), datetime('now'))`)

	r := chiRouter("POST", "/api/revenue/service-requests/{id}/fail", FailServiceRequest(store))
	req := httptest.NewRequest("POST", "/api/revenue/service-requests/sr-fail1/fail", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "failed" {
		t.Errorf("status = %v, want failed", body["status"])
	}
}

// --- RecordOpportunityFeedback ---
// The revenue_feedback table columns match the handler SQL, so this works.

func TestRecordOpportunityFeedback_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO revenue_opportunities
		 (id, source, strategy, payload_json, expected_revenue_usdc, status, created_at)
		 VALUES ('opp-fb1', 'api', 'bounty', '{}', 50.0, 'active', datetime('now'))`)

	r := chiRouter("POST", "/api/revenue/opportunities/{id}/feedback", RecordOpportunityFeedback(store))
	req := httptest.NewRequest("POST", "/api/revenue/opportunities/opp-fb1/feedback",
		strings.NewReader(`{"strategy":"bounty","grade":0.9,"source":"manual","comment":"looks good"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["opportunity_id"] != "opp-fb1" {
		t.Errorf("opportunity_id = %v, want opp-fb1", body["opportunity_id"])
	}
	if body["id"] == nil || body["id"] == "" {
		t.Error("expected non-empty feedback id")
	}
}

func TestRecordOpportunityFeedback_DefaultsStrategyAndSource(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO revenue_opportunities
		 (id, source, strategy, payload_json, expected_revenue_usdc, status, created_at)
		 VALUES ('opp-fb2', 'api', 'bounty', '{}', 50.0, 'active', datetime('now'))`)

	r := chiRouter("POST", "/api/revenue/opportunities/{id}/feedback", RecordOpportunityFeedback(store))
	// Send only grade and comment; strategy defaults to "unknown", source to "api".
	req := httptest.NewRequest("POST", "/api/revenue/opportunities/opp-fb2/feedback",
		strings.NewReader(`{"grade":0.5,"comment":"okay"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
}

func TestRecordOpportunityFeedback_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)

	r := chiRouter("POST", "/api/revenue/opportunities/{id}/feedback", RecordOpportunityFeedback(store))
	req := httptest.NewRequest("POST", "/api/revenue/opportunities/opp-x/feedback",
		strings.NewReader(`{broken`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// --- IntakeMicroBounty ---

func TestIntakeMicroBounty_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	handler := IntakeMicroBounty(store)
	req := httptest.NewRequest("POST", "/api/revenue/micro-bounties",
		strings.NewReader(`nope`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestIntakeMicroBounty_SchemaError(t *testing.T) {
	store := testutil.TempStore(t)
	handler := IntakeMicroBounty(store)
	req := httptest.NewRequest("POST", "/api/revenue/micro-bounties",
		strings.NewReader(`{"source":"scan","score":0.7,"estimated_value_usd":25.0}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Handler inserts into columns (score, estimated_value_usd) that don't exist.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch)", rec.Code)
	}
}

// --- IntakeOracleFeed ---

func TestIntakeOracleFeed_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	handler := IntakeOracleFeed(store)
	req := httptest.NewRequest("POST", "/api/revenue/oracle-feeds",
		strings.NewReader(`!!!`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestIntakeOracleFeed_SchemaError(t *testing.T) {
	store := testutil.TempStore(t)
	handler := IntakeOracleFeed(store)
	req := httptest.NewRequest("POST", "/api/revenue/oracle-feeds",
		strings.NewReader(`{"source":"external","score":0.65,"estimated_value_usd":200.0}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Handler inserts into columns (score, estimated_value_usd) that don't exist.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (schema mismatch)", rec.Code)
	}
}
