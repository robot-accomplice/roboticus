package routes

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/testutil"
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

func TestReloadSkillsInvokesRuntimeReload(t *testing.T) {
	called := false
	handler := ReloadSkills(func() error {
		called = true
		return nil
	})
	req := httptest.NewRequest("POST", "/api/skills/reload", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Error("reload callback was not called")
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
	store := testutil.TempStore(t)
	handler := GetSkillsCatalog(store, nil, nil)
	req := httptest.NewRequest("GET", "/api/skills/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if _, ok := body["skills"]; !ok {
		t.Error("missing skills key")
	}
	if _, ok := body["plugins"]; !ok {
		t.Error("missing plugins key")
	}
	if _, ok := body["themes"]; !ok {
		t.Error("missing themes key")
	}
}

func TestTestChannel(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Channels.Telegram = &core.TelegramConfig{Enabled: true}
	handler := TestChannel(&cfg)

	// Use chi router to set URL param.
	r := chi.NewRouter()
	r.Post("/api/channels/{name}/test", handler)

	req := httptest.NewRequest("POST", "/api/channels/telegram/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["platform"] != "telegram" {
		t.Errorf("platform = %v, want telegram", body["platform"])
	}
	if body["configured"] != true {
		t.Errorf("configured = %v, want true", body["configured"])
	}
}

func TestSetProviderKey(t *testing.T) {
	t.Setenv("ROBOTICUS_MASTER_KEY", "test-key-for-unit-tests")
	ks, err := core.OpenKeystore(core.KeystoreConfig{Path: filepath.Join(t.TempDir(), "test.enc")})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Put("/api/providers/{provider}/key", SetProviderKey(ks))
	req := httptest.NewRequest("PUT", "/api/providers/openai/key", strings.NewReader(`{"key":"sk-test"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got, err := ks.Get("openai_api_key")
	if err != nil {
		t.Fatalf("expected conventional provider key to be stored: %v", err)
	}
	if got != "sk-test" {
		t.Fatalf("stored key = %q, want sk-test", got)
	}
}

func TestUpdateConfig(t *testing.T) {
	store := testutil.TempStore(t)
	// Use a temp dir for config file to avoid writing to real home dir.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := core.DefaultConfig()
	handler := UpdateConfig(&cfg, store)
	// Patch a valid field so validation passes.
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`{"server":{"port":9090,"bind":"localhost","cron_max_concurrency":4}}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	body := jsonBody(t, rec)
	if body["status"] != "patched" {
		t.Errorf("status = %v, want patched", body["status"])
	}
}

func TestUpdateConfig_InvalidJSON(t *testing.T) {
	store := testutil.TempStore(t)
	cfg := core.DefaultConfig()
	handler := UpdateConfig(&cfg, store)
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestUpdateConfig_AuditTrailFailureIsBestEffort(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE identity`); err != nil {
		t.Fatalf("drop identity: %v", err)
	}

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := core.DefaultConfig()
	handler := UpdateConfig(&cfg, store)
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`{"server":{"port":9090,"bind":"localhost","cron_max_concurrency":4}}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Audit trail is best-effort (core.ApplyConfigPatch silently handles DB errors).
	// The config write itself should succeed even if the audit table is missing.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
}
