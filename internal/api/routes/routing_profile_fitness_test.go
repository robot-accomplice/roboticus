package routes

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/testutil"
)

// ---------------------------------------------------------------------------
// 1. Weight normalization invariants
// ---------------------------------------------------------------------------

func TestWeightNormalization_SumsToOne(t *testing.T) {
	tests := []struct {
		name string
		body routingProfile
	}{
		{name: "equal weights", body: routingProfile{Efficacy: 1, Cost: 1, Speed: 1}},
		{name: "dominant correctness", body: routingProfile{Efficacy: 10, Cost: 1, Speed: 1}},
		{name: "dominant cost", body: routingProfile{Efficacy: 1, Cost: 10, Speed: 1}},
		{name: "dominant speed", body: routingProfile{Efficacy: 1, Cost: 1, Speed: 10}},
		{name: "single correctness", body: routingProfile{Efficacy: 1, Cost: 0, Speed: 0}},
		{name: "single cost", body: routingProfile{Efficacy: 0, Cost: 1, Speed: 0}},
		{name: "single speed", body: routingProfile{Efficacy: 0, Cost: 0, Speed: 1}},
		{name: "tiny equal", body: routingProfile{Efficacy: 0.001, Cost: 0.001, Speed: 0.001}},
		{name: "large equal", body: routingProfile{Efficacy: 100, Cost: 200, Speed: 300}},
		{name: "fractional", body: routingProfile{Efficacy: 0.7, Cost: 0.2, Speed: 0.1}},
		{name: "already normalized", body: routingProfile{Efficacy: 0.5, Cost: 0.3, Speed: 0.2}},
		{name: "unbalanced", body: routingProfile{Efficacy: 99.9, Cost: 0.05, Speed: 0.05}},
		{name: "large numbers", body: routingProfile{Efficacy: 1000, Cost: 2000, Speed: 3000}},
		{name: "decimal precision", body: routingProfile{Efficacy: 0.333, Cost: 0.333, Speed: 0.334}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := testutil.TempStore(t)
			handler := PutRoutingProfile(store)

			data, _ := json.Marshal(tc.body)
			req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewReader(data))
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
			if math.Abs(sum-1.0) > 1e-9 {
				t.Errorf("sum = %.15f, want 1.0 (correctness=%f cost=%f speed=%f)",
					sum, profile.Efficacy, profile.Cost, profile.Speed)
			}
		})
	}
}

func TestWeightNormalization_ResidualGoesToLargest(t *testing.T) {
	// With (3, 1, 1), correctness should be the largest and absorb rounding residual.
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	data := `{"efficacy": 3, "cost": 1, "speed": 1}`
	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var profile routingProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Efficacy = 3/5 = 0.6, Cost = 1/5 = 0.2, Speed = 1/5 = 0.2
	// After rounding to 3 decimals: 0.6 + 0.2 + 0.2 = 1.0 (no residual).
	// But for a case like (1, 1, 1): 0.333 + 0.333 + 0.333 = 0.999, residual = 0.001
	// The largest weight gets the residual.
	if profile.Efficacy < profile.Cost || profile.Efficacy < profile.Speed {
		t.Errorf("correctness should be largest: c=%f cost=%f speed=%f",
			profile.Efficacy, profile.Cost, profile.Speed)
	}

	sum := profile.Efficacy + profile.Cost + profile.Availability + profile.Locality + profile.Confidence + profile.Speed
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("sum = %.15f, want 1.0", sum)
	}
}

func TestWeightNormalization_ResidualDistribution_EqualWeights(t *testing.T) {
	// (1, 1, 1) -> each rounds to 0.333, sum = 0.999, residual = 0.001
	// All are equal, so correctness >= cost >= speed path gives it to correctness.
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	data := `{"efficacy": 1, "cost": 1, "availability": 1, "locality": 1, "confidence": 1, "speed": 1}`
	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var profile routingProfile
	_ = json.Unmarshal(rec.Body.Bytes(), &profile)

	// All equal → each ~0.167, residual absorbed by the largest (first tie-breaker).
	sum := profile.Efficacy + profile.Cost + profile.Availability + profile.Locality + profile.Confidence + profile.Speed
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("sum = %.15f, want 1.0", sum)
	}
}

func TestWeightNormalization_CostDominant_ResidualToCost(t *testing.T) {
	// When cost is largest, residual should go to cost.
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	data := `{"efficacy": 1, "cost": 5, "speed": 1}`
	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var profile routingProfile
	_ = json.Unmarshal(rec.Body.Bytes(), &profile)

	if profile.Cost < profile.Efficacy || profile.Cost < profile.Speed {
		t.Errorf("cost should be largest: c=%f cost=%f speed=%f",
			profile.Efficacy, profile.Cost, profile.Speed)
	}
	sum := profile.Efficacy + profile.Cost + profile.Availability + profile.Locality + profile.Confidence + profile.Speed
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("sum = %.15f, want 1.0", sum)
	}
}

// ---------------------------------------------------------------------------
// 2. Profile persistence round-trip
// ---------------------------------------------------------------------------

func TestProfilePersistence_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "balanced", body: `{"efficacy": 1, "cost": 1, "speed": 1}`},
		{name: "correctness heavy", body: `{"efficacy": 8, "cost": 1, "speed": 1}`},
		{name: "speed heavy", body: `{"efficacy": 1, "cost": 1, "speed": 8}`},
		{name: "already normalized", body: `{"efficacy": 0.5, "cost": 0.25, "speed": 0.25}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := testutil.TempStore(t)

			// PUT
			putHandler := PutRoutingProfile(store)
			putReq := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(tc.body))
			putReq.Header.Set("Content-Type", "application/json")
			putRec := httptest.NewRecorder()
			putHandler.ServeHTTP(putRec, putReq)
			if putRec.Code != http.StatusOK {
				t.Fatalf("PUT status = %d, body = %s", putRec.Code, putRec.Body.String())
			}

			var putProfile routingProfile
			_ = json.Unmarshal(putRec.Body.Bytes(), &putProfile)

			// GET
			getHandler := GetRoutingProfile(store)
			getReq := httptest.NewRequest("GET", "/api/routing/profile", nil)
			getRec := httptest.NewRecorder()
			getHandler.ServeHTTP(getRec, getReq)
			if getRec.Code != http.StatusOK {
				t.Fatalf("GET status = %d", getRec.Code)
			}

			var getProfile routingProfile
			_ = json.Unmarshal(getRec.Body.Bytes(), &getProfile)

			// Values must match exactly.
			if getProfile.Efficacy != putProfile.Efficacy {
				t.Errorf("correctness mismatch: put=%f get=%f", putProfile.Efficacy, getProfile.Efficacy)
			}
			if getProfile.Cost != putProfile.Cost {
				t.Errorf("cost mismatch: put=%f get=%f", putProfile.Cost, getProfile.Cost)
			}
			if getProfile.Speed != putProfile.Speed {
				t.Errorf("speed mismatch: put=%f get=%f", putProfile.Speed, getProfile.Speed)
			}

			// Verify sum.
			sum := getProfile.Efficacy + getProfile.Cost + getProfile.Speed
			if math.Abs(sum-1.0) > 1e-9 {
				t.Errorf("round-trip sum = %.15f, want 1.0", sum)
			}
		})
	}
}

func TestProfilePersistence_OverwritesPrevious(t *testing.T) {
	store := testutil.TempStore(t)
	putHandler := PutRoutingProfile(store)
	getHandler := GetRoutingProfile(store)

	// First PUT: correctness-heavy.
	body1 := `{"efficacy": 9, "cost": 0.5, "speed": 0.5}`
	putReq := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body1))
	putReq.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	putHandler.ServeHTTP(rec1, putReq)

	// Second PUT: speed-heavy.
	body2 := `{"efficacy": 0.5, "cost": 0.5, "speed": 9}`
	putReq2 := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body2))
	putReq2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	putHandler.ServeHTTP(rec2, putReq2)

	// GET should return speed-heavy.
	getReq := httptest.NewRequest("GET", "/api/routing/profile", nil)
	getRec := httptest.NewRecorder()
	getHandler.ServeHTTP(getRec, getReq)

	var profile routingProfile
	_ = json.Unmarshal(getRec.Body.Bytes(), &profile)

	if profile.Speed <= profile.Efficacy {
		t.Errorf("second PUT should overwrite: speed=%f correctness=%f",
			profile.Speed, profile.Efficacy)
	}
}

// ---------------------------------------------------------------------------
// 3. Validation enforcement
// ---------------------------------------------------------------------------

func TestValidation_NegativeWeightsRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "negative correctness", body: `{"efficacy": -1, "cost": 1, "speed": 1}`},
		{name: "negative cost", body: `{"efficacy": 1, "cost": -0.5, "speed": 1}`},
		{name: "negative speed", body: `{"efficacy": 1, "cost": 1, "speed": -0.001}`},
		{name: "all negative", body: `{"efficacy": -1, "cost": -1, "speed": -1}`},
		{name: "two negative", body: `{"efficacy": -1, "cost": -1, "speed": 1}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := testutil.TempStore(t)
			handler := PutRoutingProfile(store)

			req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestValidation_AllZeroRejected(t *testing.T) {
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	body := `{"efficacy": 0, "cost": 0, "speed": 0}`
	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for all-zero weights", rec.Code)
	}
}

func TestValidation_InvalidJSONRejected(t *testing.T) {
	store := testutil.TempStore(t)
	handler := PutRoutingProfile(store)

	req := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid JSON", rec.Code)
	}
}

func TestValidation_NegativeDoesNotPersist(t *testing.T) {
	store := testutil.TempStore(t)

	// PUT negative: should be rejected.
	putHandler := PutRoutingProfile(store)
	body := `{"efficacy": -5, "cost": 1, "speed": 1}`
	putReq := httptest.NewRequest("PUT", "/api/routing/profile", bytes.NewBufferString(body))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	putHandler.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusBadRequest {
		t.Fatalf("negative PUT should fail, got %d", putRec.Code)
	}

	// GET should still return defaults (nothing was persisted).
	getHandler := GetRoutingProfile(store)
	getReq := httptest.NewRequest("GET", "/api/routing/profile", nil)
	getRec := httptest.NewRecorder()
	getHandler.ServeHTTP(getRec, getReq)

	var profile routingProfile
	_ = json.Unmarshal(getRec.Body.Bytes(), &profile)

	if profile != defaultProfile {
		t.Errorf("rejected PUT should not change profile: got %+v, want %+v", profile, defaultProfile)
	}
}
