package pipeline

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"roboticus/internal/agent"
	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/core"
	"roboticus/internal/db"
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

type falseCapabilityDenialExecutor struct {
	calls int
}

func (e *falseCapabilityDenialExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.calls++
	session.AddToolResult("repo-list", "list_directory", "ARCHITECTURE.md\narchitecture_rules.md\ndocs", false)
	content := "The architecture documentation was located, but because tools are now disabled, I can't directly read or compare its content with the code implementation."
	session.AddAssistantMessage(content, nil)
	return content, 1, nil
}

type executionNoteCaptureExecutor struct {
	responses []string
	notes     []string
	users     []string
	calls     int
}

func (e *executionNoteCaptureExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.notes = append(e.notes, session.TurnExecutionNote())
	for i := len(session.Messages()) - 1; i >= 0; i-- {
		if session.Messages()[i].Role == "user" {
			e.users = append(e.users, session.Messages()[i].Content)
			break
		}
	}
	var content string
	if e.calls < len(e.responses) {
		content = e.responses[e.calls]
	}
	if content == "" {
		content = "stub response"
	}
	e.calls++
	session.AddAssistantMessage(content, nil)
	return content, 1, nil
}

type memoryContextCaptureExecutor struct {
	memoryContexts []string
}

func (e *memoryContextCaptureExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.memoryContexts = append(e.memoryContexts, session.MemoryContext())
	session.AddAssistantMessage("The answer is 4.", nil)
	return "The answer is 4.", 1, nil
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

type extraArtifactRetryExecutor struct {
	calls int
}

func (e *extraArtifactRetryExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.calls++
	switch e.calls {
	case 1:
		alpha := agenttools.NewArtifactProof("workspace_file", "tmp/check/alpha.txt", "ALPHA", false)
		beta := agenttools.NewArtifactProof("workspace_file", "tmp/check/beta.txt", "BETA", false)
		session.AddToolResultWithMetadata("call-1", "write_file", alpha.Output(), alpha.Metadata(), false)
		session.AddToolResultWithMetadata("call-2", "write_file", beta.Output(), beta.Metadata(), false)
		session.AddAssistantMessage("I created alpha.txt, beta.txt, and gamma.txt.", nil)
		return "I created alpha.txt, beta.txt, and gamma.txt.", 1, nil
	default:
		session.AddAssistantMessage("I created alpha.txt, beta.txt, and gamma.txt.", nil)
		return "I created alpha.txt, beta.txt, and gamma.txt.", 1, nil
	}
}

type unexpectedArtifactWriteRetryExecutor struct {
	calls int
}

func (e *unexpectedArtifactWriteRetryExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.calls++
	switch e.calls {
	case 1:
		alpha := agenttools.NewArtifactProof("workspace_file", "tmp/check/alpha.txt", "ALPHA", false)
		beta := agenttools.NewArtifactProof("workspace_file", "tmp/check/beta.txt", "BETA", false)
		gamma := agenttools.NewArtifactProof("workspace_file", "tmp/check/gamma.txt", "GAMMA", false)
		session.AddToolResultWithMetadata("call-1", "write_file", alpha.Output(), alpha.Metadata(), false)
		session.AddToolResultWithMetadata("call-2", "write_file", beta.Output(), beta.Metadata(), false)
		session.AddToolResultWithMetadata("call-3", "write_file", gamma.Output(), gamma.Metadata(), false)
		session.AddAssistantMessage("I created alpha.txt, beta.txt, and gamma.txt.", nil)
		return "I created alpha.txt, beta.txt, and gamma.txt.", 1, nil
	default:
		session.AddAssistantMessage("I created alpha.txt, beta.txt, and gamma.txt.", nil)
		return "I created alpha.txt, beta.txt, and gamma.txt.", 1, nil
	}
}

type reflectFinalizationExecutor struct {
	calls int
}

func (e *reflectFinalizationExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.calls++
	proof := agenttools.NewInspectionProof("file_glob", "glob_files", ".", 1).WithPattern("*.yml")
	session.AddToolResultWithMetadata("call-1", "glob_files", "tmp/config.yml", proof.Metadata(), false)
	content := "The active model settings are configured in tmp/config.yml."
	session.AddAssistantMessageWithPhase(content, nil, "reflect")
	return content, 1, nil
}

type emptyAfterToolProgressExecutor struct{}

func (e *emptyAfterToolProgressExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	session.AddToolResult("call-1", "get_runtime_context", "runtime context captured", false)
	return "", 1, nil
}

// stubRetriever is a minimal MemoryRetriever for pipeline tests.
type stubRetriever struct {
	result string
	active string
}

func (s *stubRetriever) Retrieve(_ context.Context, _, _ string, _ int) string {
	return s.result
}

func (s *stubRetriever) RetrieveActiveContext(_ context.Context, _ string, _ int) string {
	return s.active
}

func TestStageMemoryRetrieval_AllowRetrievalFalseSuppressesMemoryHandles(t *testing.T) {
	pipe := New(PipelineDeps{Retriever: &stubRetriever{
		result: "[Retrieved Evidence]\n- stale prior failure",
		active: "[Working State]\n- stale active note",
	}})
	sess := NewSession("sess-no-retrieval", "agent-1", "Test")
	pc := &pipelineContext{
		session: sess,
		content: "tell me about the tools you can use, pick one at random, and use it",
		tr:      NewTraceRecorder(),
		synthesis: TaskSynthesis{
			Intent:          "question",
			Complexity:      "simple",
			RetrievalNeeded: true,
		},
		policy: TurnEnvelopePolicy{
			ToolProfile:    ToolProfileFocusedToolDemonstration,
			AllowRetrieval: false,
		},
	}

	pipe.stageMemoryRetrieval(context.Background(), pc)

	if got := sess.MemoryIndex(); got != "" {
		t.Fatalf("memory index = %q, want empty when AllowRetrieval=false", got)
	}
	if got := sess.MemoryContext(); got != "" {
		t.Fatalf("memory context = %q, want empty when AllowRetrieval=false", got)
	}
}

func TestStageToolExecutionNote_FocusedToolDemonstration(t *testing.T) {
	pipe := New(PipelineDeps{})
	pipe.pruner = &countingPolicyPruner{
		fn: func(_ context.Context, _ *Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
			return []llm.ToolDef{
				{Type: "function", Function: llm.ToolFuncDef{Name: "list_directory"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
			}, agenttools.ToolSearchStats{CandidatesSelected: 2, EmbeddingStatus: "ok"}, nil
		},
	}
	sess := NewSession("sess-tool-demo-note", "agent-1", "Test")
	pc := &pipelineContext{
		session: sess,
		tr:      NewTraceRecorder(),
		policy: TurnEnvelopePolicy{
			ToolProfile:    ToolProfileFocusedToolDemonstration,
			MaxTools:       4,
			AllowRetrieval: false,
		},
	}

	pipe.stageToolPruning(context.Background(), pc)

	note := sess.TurnExecutionNote()
	if !strings.Contains(note, "focused tool-demonstration turn") {
		t.Fatalf("turn execution note = %q", note)
	}
	if !strings.Contains(note, "observed result") {
		t.Fatalf("turn execution note should bind finalization to observed results, got %q", note)
	}
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

func TestPipeline_Run_UsesCallerSuppliedTurnID(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{},
		Guards:   DefaultGuardChain(),
	})

	const turnID = "turn-caller-supplied"
	outcome, err := pipe.Run(context.Background(), PresetAPI(), Input{
		Content: "Hello with caller identity",
		TurnID:  turnID,
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome.TurnID != turnID {
		t.Fatalf("outcome turn id = %q, want %q", outcome.TurnID, turnID)
	}

	var stored string
	if err := store.QueryRowContext(context.Background(), `SELECT id FROM turns WHERE id = ?`, turnID).Scan(&stored); err != nil {
		t.Fatalf("query caller-supplied turn: %v", err)
	}
}

func TestPipeline_Run_HardGuardExhaustionDoesNotReturnFalseCapabilityDenial(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &falseCapabilityDenialExecutor{}
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: exec,
		Guards:   DefaultGuardChain(),
	})

	outcome, err := pipe.Run(context.Background(), PresetAPI(), Input{
		Content: "Please review all of the subdirectories associated with the project at ~/code/roboticus and try to locate the architecture documentation.",
		AgentID: "test-agent",
	})
	if err == nil {
		t.Fatalf("expected hard guard exhaustion error, got outcome content: %q", outcome.Content)
	}
	if exec.calls < 3 {
		t.Fatalf("executor calls = %d, want at least 3 bounded attempts", exec.calls)
	}
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
		Retriever: &stubRetriever{result: "[Active Memory]\n\n[Gaps]\n- No evidence retrieved from any tier"},
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
	if outcome.Content != "I wrote tmp/out.txt containing exactly hello." {
		t.Fatalf("expected best available retry content to be preserved, got %q", outcome.Content)
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

func TestPipeline_Run_VerifierRetryRechecksExtraArtifactClaims(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &extraArtifactRetryExecutor{}

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
		Content: "Create two files in tmp/check/ exactly as follows. File 1: alpha.txt with content:\nALPHA\nFile 2: beta.txt with content:\nBETA\nAfter writing them, tell me that you created three files including gamma.txt.",
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.calls != 2 {
		t.Fatalf("expected verifier to trigger exactly one retry, got %d calls", exec.calls)
	}
	if outcome.Content != "I created alpha.txt, beta.txt, and gamma.txt." {
		t.Fatalf("expected best available retry content to be preserved, got %q", outcome.Content)
	}

	var turnID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&turnID); err != nil {
		t.Fatalf("query turn id: %v", err)
	}

	var details sql.NullString
	if err := store.QueryRowContext(context.Background(),
		`SELECT details_json FROM turn_diagnostic_events WHERE turn_id = ? AND event_type = ?`,
		turnID, "verifier_retry_rechecked",
	).Scan(&details); err != nil {
		t.Fatalf("query verifier recheck details: %v", err)
	}
	if !strings.Contains(details.String, "artifact_set_overclaim") {
		t.Fatalf("verifier recheck details = %q, want artifact_set_overclaim", details.String)
	}
}

func TestPipeline_Run_VerifierRetryRechecksUnexpectedArtifactWrites(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &unexpectedArtifactWriteRetryExecutor{}

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
		Content: "Create two files in tmp/check/ exactly as follows. File 1: alpha.txt with content:\nALPHA\nFile 2: beta.txt with content:\nBETA\nAfter writing them, tell me you created alpha.txt, beta.txt, and gamma.txt.",
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.calls != 2 {
		t.Fatalf("expected verifier to trigger exactly one retry, got %d calls", exec.calls)
	}
	if outcome.Content != "I created alpha.txt, beta.txt, and gamma.txt." {
		t.Fatalf("expected best available retry content to be preserved, got %q", outcome.Content)
	}

	var turnID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&turnID); err != nil {
		t.Fatalf("query turn id: %v", err)
	}

	var details sql.NullString
	if err := store.QueryRowContext(context.Background(),
		`SELECT details_json FROM turn_diagnostic_events WHERE turn_id = ? AND event_type = ?`,
		turnID, "verifier_retry_rechecked",
	).Scan(&details); err != nil {
		t.Fatalf("query verifier recheck details: %v", err)
	}
	if !strings.Contains(details.String, "artifact_unexpected_write") {
		t.Fatalf("verifier recheck details = %q, want artifact_unexpected_write", details.String)
	}
}

func TestPipeline_Run_SuppressesGenericVerifierRetryAfterReflectiveFinalization(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &reflectFinalizationExecutor{}

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
		Content: "Read tmp/config.yml and summarize the model settings.",
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected reflective finalization to suppress generic verifier retry, got %d calls", exec.calls)
	}
	if outcome.Content != "The active model settings are configured in tmp/config.yml." {
		t.Fatalf("unexpected outcome content: %q", outcome.Content)
	}

	var turnID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&turnID); err != nil {
		t.Fatalf("query turn id: %v", err)
	}

	var details sql.NullString
	if err := store.QueryRowContext(context.Background(),
		`SELECT details_json FROM turn_diagnostic_events WHERE turn_id = ? AND event_type = ?`,
		turnID, "verifier_retry_suppressed",
	).Scan(&details); err != nil {
		t.Fatalf("query verifier suppression details: %v", err)
	}
	if !strings.Contains(details.String, "R-TEOR-R boundary") {
		t.Fatalf("verifier suppression details = %q, want R-TEOR-R boundary reason", details.String)
	}
}

func TestPipeline_Run_SuppressedVerifierRetryDoesNotStoreBlankAfterToolProgress(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &emptyAfterToolProgressExecutor{}

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
		Content: "Create a scheduled task that runs a health check every hour and stores the results.",
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(outcome.Content) == "" {
		t.Fatal("outcome content is blank after tool-backed progress")
	}
	if !strings.Contains(outcome.Content, "runtime context captured") {
		t.Fatalf("outcome content = %q, want observed tool evidence", outcome.Content)
	}

	var stored string
	if err := store.QueryRowContext(context.Background(),
		`SELECT content FROM session_messages WHERE session_id = ? AND role = 'assistant' ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&stored); err != nil {
		t.Fatalf("query stored assistant message: %v", err)
	}
	if strings.TrimSpace(stored) == "" {
		t.Fatal("stored assistant message is blank after tool-backed progress")
	}

	var synthesized int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM turn_diagnostic_events WHERE event_type = 'response_synthesized_from_observation'`,
	).Scan(&synthesized); err != nil {
		t.Fatalf("query synthesized event: %v", err)
	}
	if synthesized != 1 {
		t.Fatalf("response_synthesized_from_observation events = %d, want 1", synthesized)
	}
}

func TestPipeline_Run_Shortcut(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "normal inference"},
	})
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
		t.Fatal("run should produce outcome")
	}
	if outcome.Content != "normal inference" {
		t.Fatalf("expected acknowledgement to flow through normal inference, got %q", outcome.Content)
	}
}

func TestRunPipeline_Wrapper(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "wrapper inference"},
	})
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
	if outcome.Content != "wrapper inference" {
		t.Fatalf("expected disabled acknowledgement shortcut to fall through to executor, got %q", outcome.Content)
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

func TestPipeline_ShortFollowupExpansionDoesNotRewriteStoredUserMessage(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &executionNoteCaptureExecutor{responses: []string{
		"The next step is to inspect page HTML/body content for score elements. Please confirm if I should proceed.",
		"I proceeded with the Metacritic score extraction.",
	}}
	pipe := New(PipelineDeps{Store: store, Executor: exec})
	cfg := PresetAPI()
	cfg.ShortFollowupExpansion = true

	first, err := pipe.Run(context.Background(), cfg, Input{
		Content: "Get the Metacritic score for Vampire Crawlers",
		AgentID: "duncan",
	})
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	_, err = pipe.Run(context.Background(), cfg, Input{
		SessionID: first.SessionID,
		Content:   "Please do",
		AgentID:   "duncan",
	})
	if err != nil {
		t.Fatalf("follow-up run failed: %v", err)
	}

	if len(exec.users) < 2 {
		t.Fatalf("executor captured %d user turns, want at least 2", len(exec.users))
	}
	if exec.users[1] != "Please do" {
		t.Fatalf("durable session user message = %q, want original follow-up", exec.users[1])
	}
	if len(exec.notes) < 2 || !strings.Contains(strings.ToLower(exec.notes[1]), "pending action confirmed") {
		t.Fatalf("execution note did not receive resolved continuation context: %#v", exec.notes)
	}

	rows, err := store.QueryContext(context.Background(),
		`SELECT content FROM session_messages WHERE session_id = ? AND role = 'user' ORDER BY created_at ASC`,
		first.SessionID,
	)
	if err != nil {
		t.Fatalf("query stored user messages: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var stored []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			t.Fatalf("scan stored user message: %v", err)
		}
		stored = append(stored, content)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stored user messages: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored user message count = %d, want 2: %#v", len(stored), stored)
	}
	if stored[1] != "Please do" {
		t.Fatalf("stored follow-up = %q, want original operator text", stored[1])
	}
	joined := strings.ToLower(strings.Join(stored, "\n"))
	for _, marker := range []string{
		"pending action confirmed",
		"user follow-up is a pending-action continuation",
		"previous assistant reply excerpt",
	} {
		if strings.Contains(joined, marker) {
			t.Fatalf("stored user transcript contains framework scaffold %q: %#v", marker, stored)
		}
	}
}

func TestPipeline_LoadSessionUsesRecentTailForPendingActionContinuity(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &executionNoteCaptureExecutor{responses: []string{
		"I reviewed the architecture documentation against the code implementation.",
	}}
	pipe := New(PipelineDeps{Store: store, Executor: exec})
	ctx := context.Background()
	cfg := PresetAPI()
	cfg.ShortFollowupExpansion = true

	sessID := db.NewID()
	if _, err := store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key, status) VALUES (?, ?, ?, 'active')`,
		sessID, "duncan", "test:"+sessID,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	for i := 0; i < 55; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := store.ExecContext(ctx,
			`INSERT INTO session_messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, datetime('now', ? || ' seconds'))`,
			db.NewID(), sessID, role, "old filler message", -120+i,
		); err != nil {
			t.Fatalf("insert filler message %d: %v", i, err)
		}
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, created_at) VALUES (?, ?, 'user', ?, datetime('now', '-2 seconds'))`,
		db.NewID(), sessID, "Please review all subdirectories and compare architecture docs with code.",
	); err != nil {
		t.Fatalf("insert user task: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, created_at) VALUES (?, ?, 'assistant', ?, datetime('now', '-1 seconds'))`,
		db.NewID(), sessID, `I located ARCHITECTURE.md and architecture_rules.md.

Next Steps:
1. Read ARCHITECTURE.md and architecture_rules.md.
2. Review the implementation under cmd/ and internal/.
3. Summarize alignment between the documentation and code.

If you'd like me to proceed with reviewing the architecture documentation in detail and comparing it to the implementation in the code, please confirm!`,
	); err != nil {
		t.Fatalf("insert pending assistant action: %v", err)
	}

	_, err := pipe.Run(ctx, cfg, Input{
		SessionID: sessID,
		Content:   "confirme",
		AgentID:   "duncan",
		AgentName: "Duncan",
	})
	if err != nil {
		t.Fatalf("follow-up run failed: %v", err)
	}

	if len(exec.notes) < 1 {
		t.Fatalf("executor notes = %d, want at least 1: %#v", len(exec.notes), exec.notes)
	}
	note := strings.ToLower(exec.notes[0])
	for _, want := range []string{
		"pending action confirmed",
		"read architecture.md",
		"review the implementation under cmd/ and internal/",
		"user confirmation: confirme",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("recent pending-action note missing %q: %q", want, exec.notes[0])
		}
	}
}

func TestPipeline_LightweightTurnStillReceivesActiveWorkingMemory(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &memoryContextCaptureExecutor{}
	pipe := New(PipelineDeps{
		Store:     store,
		Executor:  exec,
		Retriever: &stubRetriever{active: "[Active Memory]\n\n[Working State]\n- Current topic: arithmetic validation"},
	})
	cfg := PresetAPI()

	_, err := pipe.Run(context.Background(), cfg, Input{
		Content: "What is 2+2?",
		AgentID: "duncan",
	})
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if len(exec.memoryContexts) != 1 {
		t.Fatalf("captured %d memory contexts, want 1", len(exec.memoryContexts))
	}
	if !strings.Contains(exec.memoryContexts[0], "Current topic: arithmetic validation") {
		t.Fatalf("lightweight turn did not receive active working memory: %q", exec.memoryContexts[0])
	}
}
