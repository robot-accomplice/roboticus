package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIHandler(t *testing.T) {
	handler := OpenAPIHandler()
	req := httptest.NewRequest("GET", "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	// The embedded spec should contain standard OpenAPI markers.
	body := rec.Body.String()
	if !strings.Contains(body, "openapi") {
		t.Error("response should contain 'openapi' keyword")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ACAO = %q, want *", got)
	}
}

func TestOpenAPIHandler_NonEmptyBody(t *testing.T) {
	handler := OpenAPIHandler()
	req := httptest.NewRequest("GET", "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.Len() == 0 {
		t.Error("response body should not be empty")
	}
}

func TestDocsHandler(t *testing.T) {
	handler := DocsHandler()
	req := httptest.NewRequest("GET", "/api/docs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/html" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "swagger-ui") {
		t.Error("docs page should contain swagger-ui reference")
	}
	if !strings.Contains(body, "SwaggerUIBundle") {
		t.Error("docs page should contain SwaggerUIBundle initialization")
	}
	if !strings.Contains(body, "/openapi.yaml") {
		t.Error("docs page should reference /openapi.yaml")
	}
}
