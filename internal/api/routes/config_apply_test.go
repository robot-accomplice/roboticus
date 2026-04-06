package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roboticus/internal/core"
	"roboticus/testutil"
)

func TestConfigApply_ValidSection(t *testing.T) {
	cfg := core.DefaultConfig()
	store := testutil.TempStore(t)
	handler := ConfigApply(&cfg, store)

	sections := []string{"agent", "server", "models", "memory", "cache", "skills", "channels", "wallet"}
	for _, section := range sections {
		t.Run(section, func(t *testing.T) {
			body := strings.NewReader(`{"section":"` + section + `","values":{}}`)
			req := httptest.NewRequest(http.MethodPost, "/api/config/apply", body)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			resp := jsonBody(t, rec)
			if resp["applied"] != true {
				t.Errorf("applied = %v, want true", resp["applied"])
			}
			if resp["section"] != section {
				t.Errorf("section = %v, want %q", resp["section"], section)
			}
		})
	}
}

func TestConfigApply_UnknownSection(t *testing.T) {
	cfg := core.DefaultConfig()
	store := testutil.TempStore(t)
	handler := ConfigApply(&cfg, store)

	body := strings.NewReader(`{"section":"unknown_section","values":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config/apply", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	resp := jsonBody(t, rec)
	if resp["applied"] != false {
		t.Errorf("applied = %v, want false", resp["applied"])
	}
	if resp["message"] != "unknown config section" {
		t.Errorf("message = %v", resp["message"])
	}
}

func TestConfigApply_EmptySection(t *testing.T) {
	cfg := core.DefaultConfig()
	store := testutil.TempStore(t)
	handler := ConfigApply(&cfg, store)

	body := strings.NewReader(`{"section":"","values":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config/apply", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestConfigApply_InvalidJSON(t *testing.T) {
	cfg := core.DefaultConfig()
	store := testutil.TempStore(t)
	handler := ConfigApply(&cfg, store)

	body := strings.NewReader(`not valid json`)
	req := httptest.NewRequest(http.MethodPost, "/api/config/apply", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestConfigApply_MethodNotAllowed(t *testing.T) {
	cfg := core.DefaultConfig()
	store := testutil.TempStore(t)
	handler := ConfigApply(&cfg, store)

	req := httptest.NewRequest(http.MethodGet, "/api/config/apply", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
