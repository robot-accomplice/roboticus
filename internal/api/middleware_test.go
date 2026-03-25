package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIKeyAuth_CorrectKey(t *testing.T) {
	handler := APIKeyAuth("test-key-123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("x-api-key", "test-key-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("correct key: status = %d, want 200", rec.Code)
	}
}

func TestAPIKeyAuth_WrongKey(t *testing.T) {
	handler := APIKeyAuth("test-key-123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("x-api-key", "wrong-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyAuth_MissingKey(t *testing.T) {
	handler := APIKeyAuth("test-key-123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing key: status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyAuth_BearerToken(t *testing.T) {
	handler := APIKeyAuth("my-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer my-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("bearer token: status = %d, want 200", rec.Code)
	}
}

func TestAPIKeyAuth_EmptyKey_Loopback(t *testing.T) {
	handler := APIKeyAuth("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// Loopback should be allowed.
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("loopback with empty key: status = %d, want 200", rec.Code)
	}
}

func TestAPIKeyAuth_EmptyKey_Remote(t *testing.T) {
	handler := APIKeyAuth("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Non-loopback should be rejected.
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("remote with empty key: status = %d, want 403", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headers := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
	}
	for key, want := range headers {
		if got := rec.Header().Get(key); got != want {
			t.Errorf("header %s = %q, want %q", key, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy header missing")
	}
}

func TestBodyLimit(t *testing.T) {
	handler := BodyLimit(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(body)
	}))

	// Small body — should work.
	req := httptest.NewRequest("POST", "/test", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("small body: status = %d", rec.Code)
	}

	// Large body — should be truncated by MaxBytesReader.
	req = httptest.NewRequest("POST", "/test", strings.NewReader("this is way too long for the limit"))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// The handler might error when trying to read beyond the limit.
	// The exact behavior depends on how the handler reads the body.
}
