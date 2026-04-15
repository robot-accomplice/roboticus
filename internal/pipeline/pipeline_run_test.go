package pipeline

import (
	"context"
	"strings"
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

type sequencedExecutor struct {
	responses []string
	calls     int
}

func (s *sequencedExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	var content string
	if s.calls < len(s.responses) {
		content = s.responses[s.calls]
	}
	if content == "" {
		content = "stub response"
	}
	s.calls++
	session.AddAssistantMessage(content, nil)
	return content, 1, nil
}

// stubRetriever is a minimal MemoryRetriever for pipeline tests.
type stubRetriever struct {
	result string
}

func (s *stubRetriever) Retrieve(_ context.Context, _, _ string, _ int) string {
	return s.result
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

func TestPipeline_Run_VerifierRequestsRevisionOnEvidenceGaps(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &sequencedExecutor{responses: []string{
		"The deployment failed because the canary rollout was misconfigured.",
		"Based on the available evidence, I'm not certain yet. We need deployment logs to confirm the root cause.",
	}}

	pipe := New(PipelineDeps{
		Store:     store,
		Executor:  exec,
		Guards:    DefaultGuardChain(),
		Retriever: &stubRetriever{result: "[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.90] deployment policy\n\n[Gaps]\n- No past experiences found for this query"},
	})

	cfg := PresetAPI()
	input := Input{
		Content: "Why did the deployment fail?",
		AgentID: "test-agent",
	}

	outcome, err := pipe.Run(context.Background(), cfg, input)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.calls != 2 {
		t.Fatalf("expected verifier to trigger a second inference, got %d calls", exec.calls)
	}
	if !strings.Contains(strings.ToLower(outcome.Content), "not certain") {
		t.Fatalf("expected revised content to acknowledge uncertainty, got %q", outcome.Content)
	}
}

func TestPipeline_Run_VerifierRequestsRevisionForMissingActionPlan(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &sequencedExecutor{responses: []string{
		"The root cause was a stale cache entry in billing.",
		"The root cause was a stale cache entry in billing. Recommended fix: invalidate the cache on deploy and add a consistency check before invoice generation.",
	}}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: exec,
		Guards:   DefaultGuardChain(),
	})

	cfg := PresetAPI()
	cfg.GuardSet = GuardSetNone
	input := Input{
		Content: "Explain the root cause and propose a remediation plan",
		AgentID: "test-agent",
	}

	outcome, err := pipe.Run(context.Background(), cfg, input)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.calls != 2 {
		t.Fatalf("expected verifier to trigger a second inference, got %d calls", exec.calls)
	}
	if !strings.Contains(strings.ToLower(outcome.Content), "recommended fix") {
		t.Fatalf("expected revised content to include an action plan, got %q", outcome.Content)
	}
}

func TestPipeline_Run_VerifierRequestsRevisionForUnsupportedSubgoalEvidence(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &sequencedExecutor{responses: []string{
		"The root cause was a stale billing cache, and the affected systems were billing and ledger.",
		"The root cause was a stale billing cache. The available evidence confirms impact to billing, but ledger still needs verification.",
	}}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: exec,
		Guards:   DefaultGuardChain(),
		Retriever: &stubRetriever{result: "[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.90] Billing service cache invalidation failed after deploy\n\n[Gaps]\n- No relationship/entity data found"},
	})

	cfg := PresetAPI()
	cfg.GuardSet = GuardSetNone
	input := Input{
		Content: "What was the root cause, and which systems were affected?",
		AgentID: "test-agent",
	}

	outcome, err := pipe.Run(context.Background(), cfg, input)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.calls != 2 {
		t.Fatalf("expected verifier to trigger a second inference, got %d calls", exec.calls)
	}
	if !strings.Contains(strings.ToLower(outcome.Content), "needs verification") {
		t.Fatalf("expected revised content to acknowledge unsupported affected-system claim, got %q", outcome.Content)
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
