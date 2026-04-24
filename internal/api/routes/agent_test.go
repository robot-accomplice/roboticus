package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
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

type captureRunner struct {
	input pipeline.Input
	ctx   context.Context
}

func (c *captureRunner) Run(ctx context.Context, _ pipeline.Config, input pipeline.Input) (*pipeline.Outcome, error) {
	c.ctx = ctx
	c.input = input
	return &pipeline.Outcome{
		SessionID: "s1",
		MessageID: "m1",
		Content:   "ok",
	}, nil
}

type errorRunner struct {
	err error
}

func (e *errorRunner) Run(_ context.Context, _ pipeline.Config, _ pipeline.Input) (*pipeline.Outcome, error) {
	return nil, e.err
}

func TestAgentMessage_PreservesNoEscalate(t *testing.T) {
	runner := &captureRunner{}
	handler := AgentMessage(runner, "Duncan")

	body, err := json.Marshal(map[string]any{
		"content":     "test",
		"no_cache":    true,
		"no_escalate": true,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/agent/message", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !runner.input.NoCache {
		t.Fatal("expected NoCache to reach pipeline input")
	}
	if !runner.input.NoEscalate {
		t.Fatal("expected NoEscalate to reach pipeline input")
	}
}

func TestAgentMessage_Returns404ForMissingSession(t *testing.T) {
	handler := AgentMessage(&errorRunner{err: core.NewError(core.ErrNotFound, "session not found")}, "Duncan")

	body, err := json.Marshal(map[string]any{
		"content":    "test",
		"session_id": "missing-session",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/agent/message", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAgentMessage_UsesDetachedExecutionTimeoutBudget(t *testing.T) {
	runner := &captureRunner{}
	handler := AgentMessage(runner, "Duncan")

	body, err := json.Marshal(map[string]any{
		"content":              "test",
		"execution_timeout_ms": 180000,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/agent/message", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if runner.ctx == nil {
		t.Fatal("expected runner context to be captured")
	}
	deadline, ok := runner.ctx.Deadline()
	if !ok {
		t.Fatal("expected detached execution timeout deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 175*time.Second || remaining > 180*time.Second {
		t.Fatalf("remaining deadline = %v, want approximately 180s", remaining)
	}
}

func TestAgentMessage_AttachesPerModelCallTimeout(t *testing.T) {
	runner := &captureRunner{}
	handler := AgentMessage(runner, "Duncan")

	body, err := json.Marshal(map[string]any{
		"content":               "test",
		"model_call_timeout_ms": 240000,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/agent/message", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := core.ModelCallTimeoutFromCtx(runner.ctx); got != 240*time.Second {
		t.Fatalf("model call timeout = %v, want 240s", got)
	}
	if _, ok := runner.ctx.Deadline(); ok {
		t.Fatal("model call timeout must not create a whole-turn deadline by itself")
	}
}
