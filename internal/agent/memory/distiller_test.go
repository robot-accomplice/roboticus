package memory

import (
	"context"
	"fmt"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

func TestServiceDistiller_IntegrationWithMockLLM(t *testing.T) {
	store := testutil.TempStore(t)
	expected := "Nginx deployments require file ownership checks on config directories."

	mockLLM := testutil.MockLLMServer(t, func(body map[string]any) (int, any) {
		return 200, map[string]any{
			"id":    "mock-1",
			"model": "test-model",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": expected,
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 50, "completion_tokens": 20, "total_tokens": 70},
		}
	})

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{{
			Name: "mock", URL: mockLLM.URL, Format: llm.FormatOpenAI,
			IsLocal: true, ChatPath: "/v1/chat/completions", AuthHeader: "Bearer",
		}},
		Primary: "mock/test-model",
	}, store)
	if err != nil {
		t.Fatalf("create llm service: %v", err)
	}

	distiller := &ServiceDistiller{LLMSvc: llmSvc}
	result, err := distiller.Distill(context.Background(), []string{
		"deployment failed — permission denied on /etc/nginx",
		"sudo chown fixed the nginx config ownership",
		"deployment succeeded after fixing permissions",
	})
	if err != nil {
		t.Fatalf("Distill() error: %v", err)
	}
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestServiceDistiller_LLMError_ReturnsError(t *testing.T) {
	store := testutil.TempStore(t)

	mockLLM := testutil.MockLLMServer(t, func(body map[string]any) (int, any) {
		return 500, map[string]any{"error": "internal server error"}
	})

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{{
			Name: "mock", URL: mockLLM.URL, Format: llm.FormatOpenAI,
			IsLocal: true, ChatPath: "/v1/chat/completions", AuthHeader: "Bearer",
		}},
		Primary: "mock/test-model",
	}, store)
	if err != nil {
		t.Fatalf("create llm service: %v", err)
	}

	distiller := &ServiceDistiller{LLMSvc: llmSvc}
	_, err = distiller.Distill(context.Background(), []string{"event 1", "event 2", "event 3"})
	if err == nil {
		t.Error("expected error from failing LLM, got nil")
	}
}

func TestServiceDistiller_NilService_ReturnsError(t *testing.T) {
	distiller := &ServiceDistiller{LLMSvc: nil}
	_, err := distiller.Distill(context.Background(), []string{"event"})
	if err == nil {
		t.Error("expected error for nil LLM service")
	}
}

func TestConsolidation_FullPipeline_WithServiceDistiller(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	distilled := "Server config permission issues require chown before deployment."

	mockLLM := testutil.MockLLMServer(t, func(body map[string]any) (int, any) {
		return 200, map[string]any{
			"id": "mock-1", "model": "test",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": distilled},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		}
	})

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{{
			Name: "mock", URL: mockLLM.URL, Format: llm.FormatOpenAI,
			IsLocal: true, ChatPath: "/v1/chat/completions", AuthHeader: "Bearer",
		}},
		Primary: "mock/test-model",
	}, store)
	if err != nil {
		t.Fatalf("create llm service: %v", err)
	}

	// Seed similar-but-not-identical episodic entries (Jaccard > 0.5 but < 0.85).
	variants := []string{
		"deployment failed because of permission error on nginx config file in production environment",
		"deployment failed because of ownership error on apache config file in staging environment",
		"deployment failed because of access error on haproxy config file in development environment",
	}
	for _, v := range variants {
		store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance) VALUES (?, 'tool_event', ?, 5)`,
			db.NewID(), v)
	}

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.Distiller = &ServiceDistiller{LLMSvc: llmSvc}
	report := pipe.Run(ctx, store)

	if report.Promoted < 1 {
		t.Errorf("expected at least 1 promotion, got %d", report.Promoted)
	}

	// Verify the semantic value is the LLM-distilled output.
	var value string
	err = store.QueryRowContext(ctx,
		`SELECT value FROM semantic_memory WHERE state_reason = 'promoted from episodic'`).Scan(&value)
	if err != nil {
		t.Fatalf("query promoted semantic: %v", err)
	}
	if value != distilled {
		t.Errorf("expected distilled value %q, got %q", distilled, value)
	}
	t.Logf("✓ End-to-end: episodic group → LLM distillation → semantic fact: %q", value)
}

func TestServiceDistiller_PromptContainsAllEntries(t *testing.T) {
	store := testutil.TempStore(t)
	var receivedPrompt string

	mockLLM := testutil.MockLLMServer(t, func(body map[string]any) (int, any) {
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if msg, ok := msgs[0].(map[string]any); ok {
				receivedPrompt, _ = msg["content"].(string)
			}
		}
		return 200, map[string]any{
			"id": "mock-1", "model": "test",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "distilled fact"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		}
	})

	llmSvc, _ := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{{
			Name: "mock", URL: mockLLM.URL, Format: llm.FormatOpenAI,
			IsLocal: true, ChatPath: "/v1/chat/completions", AuthHeader: "Bearer",
		}},
		Primary: "mock/test-model",
	}, store)

	distiller := &ServiceDistiller{LLMSvc: llmSvc}
	entries := []string{"event alpha", "event beta", "event gamma"}
	_, _ = distiller.Distill(context.Background(), entries)

	for i, e := range entries {
		expected := fmt.Sprintf("%d. %s", i+1, e)
		if !containsSubstr(receivedPrompt, expected) {
			t.Errorf("prompt should contain %q, got: %s", expected, receivedPrompt[:200])
		}
	}
}
