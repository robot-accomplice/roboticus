package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/llm"
	"roboticus/testutil"
)

func TestGetRoutingProfile_Defaults(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetRoutingProfile(store)
	req := httptest.NewRequest("GET", "/api/routing/profile", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var profile routingProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if profile.Efficacy != 0.35 {
		t.Errorf("efficacy = %f, want 0.35", profile.Efficacy)
	}
	if profile.Cost != 0.20 {
		t.Errorf("cost = %f, want 0.20", profile.Cost)
	}
	if profile.Availability != 0.25 {
		t.Errorf("availability = %f, want 0.25", profile.Availability)
	}
}

func TestPutRoutingProfile_NormalizesWeights(t *testing.T) {
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	body := `{"efficacy": 3, "cost": 1, "availability": 1, "locality": 0.5, "confidence": 0.5, "speed": 1}`
	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var profile routingProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	sum := profile.Efficacy + profile.Cost + profile.Availability + profile.Locality + profile.Confidence + profile.Speed
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("sum = %f, want 1.0", sum)
	}
	// Efficacy=3 is the largest weight in the input, so it should be the largest after normalization.
	if profile.Efficacy < profile.Cost || profile.Efficacy < profile.Speed {
		t.Errorf("efficacy = %f should be largest weight (cost=%f, speed=%f)", profile.Efficacy, profile.Cost, profile.Speed)
	}
}

func TestPutRoutingProfile_RejectsNegative(t *testing.T) {
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	body := `{"efficacy": -1, "cost": 1, "speed": 1}`
	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutRoutingProfile_RejectsAllZero(t *testing.T) {
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	body := `{"efficacy": 0, "cost": 0, "speed": 0}`
	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRoutingProfile_PersistenceRoundTrip(t *testing.T) {
	store := testutil.TempStore(t)

	// PUT a profile.
	putHandler := PutRoutingProfile(store)
	body := `{"efficacy": 0.8, "cost": 0.1, "speed": 0.1}`
	putReq := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	putHandler.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d", putRec.Code)
	}

	// GET it back.
	getHandler := GetRoutingProfile(store)
	getReq := httptest.NewRequest("GET", "/api/routing/profile", nil)
	getRec := httptest.NewRecorder()
	getHandler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", getRec.Code)
	}

	var profile routingProfile
	if err := json.Unmarshal(getRec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if profile.Efficacy < 0.7 {
		t.Errorf("correctness = %f, expected > 0.7", profile.Efficacy)
	}
}

// TestRoutingProfile_E2E_WeightVerification is the full end-to-end test:
//  1. Sets custom weights via PUT
//  2. Verifies GET returns both persisted and active weights
//  3. Verifies the weights differ from defaults
//  4. Simulates a "reload" by creating a new router with the same store
//     and verifying weights persist across the reload boundary.
func TestRoutingProfile_E2E_WeightVerification(t *testing.T) {
	store := testutil.TempStore(t)
	router := llm.NewRouter(nil, llm.RouterConfig{})

	// Sanity: active weights start at defaults.
	defaults := llm.DefaultRoutingWeights()
	active := router.GetRoutingWeights()
	if active.Efficacy != defaults.Efficacy {
		t.Fatalf("initial active efficacy = %f, want default %f", active.Efficacy, defaults.Efficacy)
	}

	// ---- Step 1: PUT custom weights (heavy on efficacy, zero locality) ----
	customBody := `{"efficacy": 9, "cost": 1, "availability": 0, "locality": 0, "confidence": 0, "speed": 0}`
	putHandler := PutRoutingProfile(store, router)
	putReq := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(customBody))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	putHandler.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", putRec.Code, putRec.Body.String())
	}

	// ---- Step 2: GET and verify both persisted + active_weights ----
	getHandler := GetRoutingProfile(store, router)
	getReq := httptest.NewRequest("GET", "/api/routing/profile", nil)
	getRec := httptest.NewRecorder()
	getHandler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", getRec.Code)
	}

	var resp routingProfileResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}

	// Persisted weights should be dominated by efficacy.
	if resp.Efficacy < 0.8 {
		t.Errorf("persisted efficacy = %f, want > 0.8", resp.Efficacy)
	}

	// active_weights must be present.
	if resp.ActiveWeights == nil {
		t.Fatal("active_weights is nil, expected populated struct")
	}

	// Active weights must match persisted (PUT pushed to router).
	if resp.ActiveWeights.Efficacy != resp.Efficacy {
		t.Errorf("active efficacy = %f != persisted %f", resp.ActiveWeights.Efficacy, resp.Efficacy)
	}
	if resp.ActiveWeights.Cost != resp.Cost {
		t.Errorf("active cost = %f != persisted %f", resp.ActiveWeights.Cost, resp.Cost)
	}

	// ---- Step 3: Verify weights differ from defaults ----
	if resp.Efficacy == defaults.Efficacy {
		t.Errorf("persisted efficacy %f should differ from default %f", resp.Efficacy, defaults.Efficacy)
	}
	if resp.ActiveWeights.Efficacy == defaults.Efficacy {
		t.Errorf("active efficacy %f should differ from default %f", resp.ActiveWeights.Efficacy, defaults.Efficacy)
	}

	// ---- Step 4: Simulate reload — new router, same store ----
	router2 := llm.NewRouter(nil, llm.RouterConfig{})

	// Before loading, router2 should have defaults.
	fresh := router2.GetRoutingWeights()
	if fresh.Efficacy != defaults.Efficacy {
		t.Fatalf("fresh router efficacy = %f, want default %f", fresh.Efficacy, defaults.Efficacy)
	}

	// Load persisted weights into router2 (same mechanism as Service startup).
	router2.SetRoutingWeights(&llm.RoutingWeights{
		Efficacy:     resp.Efficacy,
		Cost:         resp.Cost,
		Availability: resp.Availability,
		Locality:     resp.Locality,
		Confidence:   resp.Confidence,
		Speed:        resp.Speed,
	})

	// GET via the new router should show the reloaded weights.
	getHandler2 := GetRoutingProfile(store, router2)
	getReq2 := httptest.NewRequest("GET", "/api/routing/profile", nil)
	getRec2 := httptest.NewRecorder()
	getHandler2.ServeHTTP(getRec2, getReq2)

	var resp2 routingProfileResponse
	if err := json.Unmarshal(getRec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal reload GET: %v", err)
	}

	if resp2.ActiveWeights == nil {
		t.Fatal("reload: active_weights is nil")
	}
	if resp2.ActiveWeights.Efficacy != resp.Efficacy {
		t.Errorf("reload: active efficacy = %f, want %f", resp2.ActiveWeights.Efficacy, resp.Efficacy)
	}
	if resp2.Efficacy != resp.Efficacy {
		t.Errorf("reload: persisted efficacy = %f, want %f", resp2.Efficacy, resp.Efficacy)
	}
}

// TestGetRoutingProfile_WithRouter_ReturnsActiveWeights verifies that
// GET without a prior PUT returns defaults for both persisted and active.
func TestGetRoutingProfile_WithRouter_ReturnsActiveWeights(t *testing.T) {
	store := testutil.TempStore(t)
	router := llm.NewRouter(nil, llm.RouterConfig{})

	handler := GetRoutingProfile(store, router)
	req := httptest.NewRequest("GET", "/api/routing/profile", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var resp routingProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Persisted should be defaults.
	if resp.Efficacy != 0.35 {
		t.Errorf("persisted efficacy = %f, want 0.35", resp.Efficacy)
	}

	// active_weights should be present and match defaults.
	if resp.ActiveWeights == nil {
		t.Fatal("active_weights nil when router provided")
	}
	if resp.ActiveWeights.Efficacy != 0.35 {
		t.Errorf("active efficacy = %f, want 0.35", resp.ActiveWeights.Efficacy)
	}
}

// TestGetRoutingProfile_WithoutRouter_OmitsActiveWeights verifies backward
// compatibility: when no router is passed, active_weights is omitted.
func TestGetRoutingProfile_WithoutRouter_OmitsActiveWeights(t *testing.T) {
	store := testutil.TempStore(t)

	handler := GetRoutingProfile(store)
	req := httptest.NewRequest("GET", "/api/routing/profile", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	// The response JSON should NOT contain "active_weights" at all.
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["active_weights"]; ok {
		t.Error("active_weights present in response without router")
	}
}
