package agent

import (
	"context"
	"testing"

	"goboticus/internal/llm"
	"goboticus/testutil"
)

func TestLoop_Run_SimpleResponse(t *testing.T) {
	mockHandler := func(body map[string]any) (int, any) {
		return 200, map[string]any{
			"id": "test", "model": "mock",
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "The answer is 42."}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		}
	}
	mockServer := testutil.MockLLMServer(t, mockHandler)
	store := testutil.TempStore(t)

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{
			{Name: "mock", URL: mockServer.URL, Format: llm.FormatOpenAI, IsLocal: true},
		},
		Primary: "mock",
	}, store)
	if err != nil {
		t.Fatalf("llm: %v", err)
	}

	session := NewSession("s1", "a1", "TestBot")
	session.AddUserMessage("What is the meaning of life?")

	ctxBuilder := NewContextBuilder(DefaultContextConfig())
	ctxBuilder.SetSystemPrompt("You are a helpful assistant.")

	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     llmSvc,
		Tools:   NewToolRegistry(),
		Policy:  NewPolicyEngine(PolicyConfig{MaxTransferCents: 1000, RateLimitPerMinute: 30}),
		Context: ctxBuilder,
	})

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("loop run: %v", err)
	}
	if result == "" {
		t.Error("result should not be empty")
	}
}

func TestLoop_TurnCount(t *testing.T) {
	loop := NewLoop(LoopConfig{MaxTurns: 3}, LoopDeps{})
	if loop.TurnCount() != 0 {
		t.Errorf("initial turn count = %d", loop.TurnCount())
	}
}
