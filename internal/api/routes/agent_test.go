package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

func TestAgentStatus(t *testing.T) {
	// Create a minimal LLM service for status.
	svc, err := llm.NewService(llm.ServiceConfig{}, nil)
	if err != nil {
		t.Skipf("no LLM service: %v", err)
	}
	cfg := core.DefaultConfig()
	handler := AgentStatus(svc, &cfg)
	req := httptest.NewRequest("GET", "/api/agent/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestAgentCard(t *testing.T) {
	handler := AgentCard()
	req := httptest.NewRequest("GET", "/.well-known/agent.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["name"] != "roboticus" {
		t.Errorf("name = %v", body["name"])
	}
}

func TestHealth(t *testing.T) {
	handler := Health(nil, nil)
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestHealth_WithStore(t *testing.T) {
	store := testutil.TempStore(t)
	handler := Health(store, nil)
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}
