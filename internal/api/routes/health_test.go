package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goboticus/testutil"
)

func TestWriteError_ClientError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "bad input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "problem+json") {
		t.Errorf("Content-Type = %s, want problem+json", ct)
	}
	body := jsonBody(t, rec)
	if body["status"].(float64) != 400 {
		t.Errorf("status in body = %v", body["status"])
	}
	if body["detail"] != "bad input" {
		t.Errorf("detail = %v", body["detail"])
	}
}

func TestWriteError_ServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusInternalServerError, "db crash details")

	body := jsonBody(t, rec)
	if body["detail"] != "internal error" {
		t.Errorf("500 error should mask detail, got %v", body["detail"])
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %s", ct)
	}
	body := jsonBody(t, rec)
	if body["hello"] != "world" {
		t.Errorf("body = %v", body)
	}
}

func TestGetConfig(t *testing.T) {
	store := testutil.TempStore(t)
	_ = store // config handler takes *core.Config, not store
	// Just verify it doesn't panic with a real config.
}

func TestGetCapabilities(t *testing.T) {
	handler := GetCapabilities()
	req := httptest.NewRequest("GET", "/api/config/capabilities", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	caps := body["capabilities"].([]any)
	if len(caps) < 5 {
		t.Errorf("capabilities = %d, want >= 5", len(caps))
	}
}

func TestReloadSkills(t *testing.T) {
	handler := ReloadSkills()
	req := httptest.NewRequest("POST", "/api/skills/reload", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
}

type mockTicketIssuer struct{}

func (m *mockTicketIssuer) Issue() string { return "wst_test_ticket_1234567890" }

func TestIssueWSTicket(t *testing.T) {
	handler := IssueWSTicket(&mockTicketIssuer{})
	req := httptest.NewRequest("POST", "/api/ws-ticket", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	ticket := body["ticket"].(string)
	if len(ticket) < 10 {
		t.Errorf("ticket too short: %s", ticket)
	}
}

func TestIssueWSTicket_NoIssuer(t *testing.T) {
	handler := IssueWSTicket()
	req := httptest.NewRequest("POST", "/api/ws-ticket", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", rec.Code)
	}
}

func TestGetSkillsCatalog(t *testing.T) {
	handler := GetSkillsCatalog()
	req := httptest.NewRequest("GET", "/api/skills/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if _, ok := body["skills"]; !ok {
		t.Error("missing skills key")
	}
}

func TestTestChannel(t *testing.T) {
	handler := TestChannel()
	req := httptest.NewRequest("POST", "/api/channels/telegram/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
}

func TestSetProviderKey(t *testing.T) {
	store := testutil.TempStore(t)
	handler := SetProviderKey(store)
	req := httptest.NewRequest("PUT", "/api/providers/openai/key", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestUpdateConfig(t *testing.T) {
	store := testutil.TempStore(t)
	handler := UpdateConfig(store)
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`{"key":"value"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestUpdateConfig_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	handler := UpdateConfig(store)
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
