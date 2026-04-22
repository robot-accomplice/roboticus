package pipeline

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"roboticus/internal/agent"
	agenttools "roboticus/internal/agent/tools"
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

type mismatchedArtifactRetryExecutor struct {
	calls int
}

func (e *mismatchedArtifactRetryExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.calls++
	switch e.calls {
	case 1:
		proof := agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "goodbye", false)
		session.AddToolResultWithMetadata("call-1", "write_file", proof.Output(), proof.Metadata(), false)
		session.AddAssistantMessage("I wrote tmp/out.txt.", nil)
		return "I wrote tmp/out.txt.", 1, nil
	default:
		proof := agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "still wrong", false)
		session.AddToolResultWithMetadata("call-2", "write_file", proof.Output(), proof.Metadata(), false)
		session.AddAssistantMessage("I wrote tmp/out.txt containing exactly hello.", nil)
		return "I wrote tmp/out.txt containing exactly hello.", 1, nil
	}
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
	cfg.DecompositionGate = false
	cfg.DelegatedExecution = false
	cfg.TaskOperatingState = "test"
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
	cfg.DecompositionGate = false
	cfg.DelegatedExecution = false
	cfg.TaskOperatingState = "test"
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
		Store:     store,
		Executor:  exec,
		Guards:    DefaultGuardChain(),
		Retriever: &stubRetriever{result: "[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.90] Billing service cache invalidation failed after deploy\n\n[Gaps]\n- No relationship/entity data found"},
	})

	cfg := PresetAPI()
	cfg.GuardSet = GuardSetNone
	cfg.DecompositionGate = false
	cfg.DelegatedExecution = false
	cfg.TaskOperatingState = "test"
	input := Input{
		Content: "Create a report that explains the root cause and identifies which systems were affected.",
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

func TestPipeline_Run_VerifierRetryRechecksFinalArtifactClaims(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &mismatchedArtifactRetryExecutor{}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: exec,
		Guards:   DefaultGuardChain(),
	})

	cfg := PresetAPI()
	cfg.GuardSet = GuardSetNone
	cfg.DecompositionGate = false
	cfg.DelegatedExecution = false
	cfg.PostTurnIngest = false
	cfg.NicknameRefinement = false
	cfg.TaskOperatingState = "test"

	outcome, err := pipe.Run(context.Background(), cfg, Input{
		Content: "Create tmp/out.txt containing exactly: hello",
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.calls != 2 {
		t.Fatalf("expected verifier to trigger exactly one retry, got %d calls", exec.calls)
	}
	if strings.Contains(strings.ToLower(outcome.Content), "containing exactly hello") {
		t.Fatalf("final content overclaimed exact success: %q", outcome.Content)
	}
	if !strings.Contains(strings.ToLower(outcome.Content), "final verification still failed") {
		t.Fatalf("expected verification-grounded fallback response, got %q", outcome.Content)
	}
	if !strings.Contains(strings.ToLower(outcome.Content), "tmp/out.txt") {
		t.Fatalf("expected fallback response to preserve failing artifact path, got %q", outcome.Content)
	}

	var turnID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&turnID); err != nil {
		t.Fatalf("query turn id: %v", err)
	}

	var rechecked int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM turn_diagnostic_events WHERE turn_id = ? AND event_type = ?`,
		turnID, "verifier_retry_rechecked",
	).Scan(&rechecked); err != nil {
		t.Fatalf("query verifier recheck event: %v", err)
	}
	if rechecked != 1 {
		t.Fatalf("verifier_retry_rechecked events = %d, want 1", rechecked)
	}

	var details sql.NullString
	if err := store.QueryRowContext(context.Background(),
		`SELECT details_json FROM turn_diagnostic_events WHERE turn_id = ? AND event_type = ?`,
		turnID, "verifier_retry_rechecked",
	).Scan(&details); err != nil {
		t.Fatalf("query verifier recheck details: %v", err)
	}
	if !strings.Contains(details.String, "artifact_content_mismatch") {
		t.Fatalf("verifier recheck details = %q, want artifact_content_mismatch", details.String)
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
