package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardHandler_StatusOK(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestDashboardHandler_ContentType(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
}

func TestDashboardHandler_CSPHeader(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("CSP header missing")
	}
	if !strings.Contains(csp, "nonce-") {
		t.Error("CSP should contain a nonce")
	}
	if !strings.Contains(csp, "script-src") {
		t.Error("CSP should contain script-src directive")
	}
	if !strings.Contains(csp, "fonts.googleapis.com") {
		t.Error("CSP should allow Google Fonts")
	}
}

func TestDashboardHandler_NonceInScriptTags(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `<script nonce="`) {
		t.Error("script tags should have nonce attribute")
	}
	// Bare <script> without nonce should not exist.
	if strings.Contains(body, "<script>") {
		t.Error("found <script> without nonce")
	}
}

func TestDashboardHandler_ContainsRoboticus(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Roboticus") {
		t.Error("dashboard should contain 'Roboticus' branding")
	}
}

func TestDashboardHandler_SecurityHeaders(t *testing.T) {
	handler := DashboardHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}

func TestDashboardHandler_UniqueNoncePerRequest(t *testing.T) {
	handler := DashboardHandler()

	// Two requests should get different nonces.
	req1 := httptest.NewRequest("GET", "/", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "/", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	csp1 := rec1.Header().Get("Content-Security-Policy")
	csp2 := rec2.Header().Get("Content-Security-Policy")
	if csp1 == csp2 {
		t.Error("two requests should have different nonces")
	}
}

func TestGenerateNonce(t *testing.T) {
	n1 := generateNonce()
	n2 := generateNonce()

	if len(n1) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("nonce length = %d, want 32", len(n1))
	}
	if n1 == n2 {
		t.Error("two nonces should differ")
	}
}
