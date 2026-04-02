package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goboticus/testutil"
)

func TestDiagnostics_ReturnsValidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	handler := Diagnostics(store)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var report DiagnosticReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func TestDiagnostics_StatusOK(t *testing.T) {
	store := testutil.TempStore(t)
	handler := Diagnostics(store)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var report DiagnosticReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if report.Status != "ok" {
		t.Errorf("status = %q, want %q", report.Status, "ok")
	}
	if report.Checks["database"] != "ok" {
		t.Errorf("database check = %q, want %q", report.Checks["database"], "ok")
	}
	if report.Checks["schema"] != "ok" {
		t.Errorf("schema check = %q, want %q", report.Checks["schema"], "ok")
	}
}

func TestDiagnostics_GoroutineCount(t *testing.T) {
	store := testutil.TempStore(t)
	handler := Diagnostics(store)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var report DiagnosticReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if report.NumGoroutine < 1 {
		t.Errorf("num_goroutine = %d, want >= 1", report.NumGoroutine)
	}
}

func TestDiagnostics_MemoryStats(t *testing.T) {
	store := testutil.TempStore(t)
	handler := Diagnostics(store)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var report DiagnosticReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if report.MemoryMB <= 0 {
		t.Errorf("memory_mb = %f, want > 0", report.MemoryMB)
	}
}

func TestDiagnostics_NilStore(t *testing.T) {
	handler := Diagnostics(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var report DiagnosticReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if report.Checks["database"] != "not configured" {
		t.Errorf("database check = %q, want %q", report.Checks["database"], "not configured")
	}
}

func TestDiagnostics_HasGoVersion(t *testing.T) {
	store := testutil.TempStore(t)
	handler := Diagnostics(store)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var report DiagnosticReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if report.GoVersion == "" {
		t.Error("go_version should not be empty")
	}
	if report.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
}
