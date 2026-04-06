package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
