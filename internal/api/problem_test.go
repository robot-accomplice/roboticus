package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteProblem(rec, http.StatusBadRequest, "invalid input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %s, want application/problem+json", ct)
	}

	var pd ProblemDetails
	if err := json.NewDecoder(rec.Body).Decode(&pd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pd.Type != ProblemTypeBlank {
		t.Errorf("type = %s, want about:blank", pd.Type)
	}
	if pd.Status != 400 {
		t.Errorf("status in body = %d, want 400", pd.Status)
	}
	if pd.Detail != "invalid input" {
		t.Errorf("detail = %s", pd.Detail)
	}
	if pd.Title != "Bad Request" {
		t.Errorf("title = %s, want Bad Request", pd.Title)
	}
}

func TestWriteProblemWithType(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteProblemWithType(rec, http.StatusForbidden, ProblemTypeInjection, "blocked")

	var pd ProblemDetails
	_ = json.NewDecoder(rec.Body).Decode(&pd)
	if pd.Type != ProblemTypeInjection {
		t.Errorf("type = %s", pd.Type)
	}
	if pd.Status != 403 {
		t.Errorf("status = %d", pd.Status)
	}
}
