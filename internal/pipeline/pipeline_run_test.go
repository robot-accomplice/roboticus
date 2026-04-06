package pipeline

import (
	"context"
	"testing"

	"roboticus/internal/agent"
	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// stubExecutor is a minimal ToolExecutor for pipeline tests.
// Returns a canned response without calling LLM, since these tests exercise
// pipeline orchestration (injection, shortcuts, guards) not inference.
type stubExecutor struct {
	response string
}

func (s *stubExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	content := s.response
	if content == "" {
		content = "stub response"
	}
	session.AddAssistantMessage(content, nil)
	return content, 1, nil
}

func TestPipeline_Run_SimpleMessage(t *testing.T) {
	store := testutil.TempStore(t)

	// Create a mock LLM that returns a canned response.
	mockHandler := func(body map[string]any) (int, any) {
		return 200, map[string]any{
			"id":    "test",
			"model": "mock",
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "Mock response"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		}
	}
	mockServer := testutil.MockLLMServer(t, mockHandler)

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{
			{Name: "mock", URL: mockServer.URL, Format: llm.FormatOpenAI, IsLocal: true},
		},
		Primary: "mock",
	}, store)
	if err != nil {
		t.Fatalf("llm: %v", err)
	}

	injection := agent.NewInjectionDetector()
	guards := DefaultGuardChain()

	pipe := New(PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: injection,
		Executor:  &stubExecutor{},
		Guards:    guards,
	})

	cfg := PresetAPI()
	input := Input{
		Content: "Hello, world!",
		AgentID: "test-agent",
	}

	outcome, err := pipe.Run(context.Background(), cfg, input)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome == nil {
		t.Fatal("outcome should not be nil")
	}
	if outcome.SessionID == "" {
		t.Error("session ID should be set")
	}
	if outcome.Content == "" {
		t.Error("content should not be empty")
	}

	// Trace storage is best-effort (FK may prevent it if no turns row).
	// Just verify the pipeline completed successfully.
}

func TestPipeline_Run_EmptyInput(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	cfg := PresetAPI()

	_, err := pipe.Run(context.Background(), cfg, Input{Content: ""})
	if err == nil {
		t.Error("empty content should fail validation")
	}
}

func TestPipeline_Run_WithInjectionDefense(t *testing.T) {
	store := testutil.TempStore(t)
	injection := agent.NewInjectionDetector()

	mockHandler := func(body map[string]any) (int, any) {
		return 200, map[string]any{
			"id": "test", "model": "mock",
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "OK"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 2},
		}
	}
	mockServer := testutil.MockLLMServer(t, mockHandler)
	llmSvc, _ := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{{Name: "mock", URL: mockServer.URL, Format: llm.FormatOpenAI, IsLocal: true}},
		Primary:   "mock",
	}, store)

	pipe := New(PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: injection,
		Executor:  &stubExecutor{},
		Guards:    DefaultGuardChain(),
	})

	cfg := PresetAPI()
	cfg.InjectionDefense = true

	// Normal message should pass injection defense.
	outcome, err := pipe.Run(context.Background(), cfg, Input{
		Content: "What is 2+2?",
		AgentID: "test",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome == nil {
		t.Fatal("should produce outcome")
	}
}

func TestPipeline_Run_Shortcut(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	cfg := PresetAPI()

	input := Input{
		Content: "ok",
		AgentID: "test",
	}
	outcome, err := pipe.Run(context.Background(), cfg, input)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome == nil {
		t.Fatal("shortcut should produce outcome")
	}
	if outcome.Content == "" {
		t.Error("shortcut should have content")
	}
}

func TestRunPipeline_Wrapper(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	cfg := PresetAPI()

	outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content: "thanks",
		AgentID: "test",
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if outcome == nil {
		t.Fatal("should produce outcome")
	}
}

func TestPipeline_Run_MaxBytes(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	cfg := PresetAPI()

	// Construct a message exceeding MaxUserMessageBytes.
	huge := make([]byte, core.MaxUserMessageBytes+1)
	for i := range huge {
		huge[i] = 'a'
	}

	_, err := pipe.Run(context.Background(), cfg, Input{
		Content: string(huge),
		AgentID: "test",
	})
	if err == nil {
		t.Error("oversized message should fail")
	}
}

// Verify that shortcut responses store the user message.
func TestPipeline_StoresUserMessage(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	cfg := PresetAPI()

	// "thanks" triggers shortcut, which stores user message then returns.
	_, _ = pipe.Run(context.Background(), cfg, Input{
		Content: "thanks",
		AgentID: "test",
	})

	var msgCount int
	row := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM session_messages`)
	_ = row.Scan(&msgCount)
	if msgCount < 1 {
		t.Error("user message should have been stored")
	}
}
