package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"roboticus/internal/agent/policy"
	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// mockCompleter returns predetermined responses.
type mockCompleter struct {
	responses []*llm.Response
	callIdx   int
}

func (m *mockCompleter) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	if m.callIdx >= len(m.responses) {
		return &llm.Response{Content: "done"}, nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *mockCompleter) Stream(_ context.Context, _ *llm.Request) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	errs := make(chan error)
	close(ch)
	close(errs)
	return ch, errs
}

type requestCapturingCompleter struct {
	responses []*llm.Response
	requests  []*llm.Request
	callIdx   int
}

func (m *requestCapturingCompleter) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	reqCopy := *req
	reqCopy.Messages = append([]llm.Message(nil), req.Messages...)
	reqCopy.Tools = append([]llm.ToolDef(nil), req.Tools...)
	m.requests = append(m.requests, &reqCopy)
	if m.callIdx >= len(m.responses) {
		return &llm.Response{Content: "done"}, nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *requestCapturingCompleter) Stream(_ context.Context, _ *llm.Request) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	errs := make(chan error)
	close(ch)
	close(errs)
	return ch, errs
}

type observerEvent struct {
	eventType string
	status    string
	details   map[string]any
}

type recordingObserver struct {
	events  []observerEvent
	summary map[string]any
}

func (r *recordingObserver) RecordEvent(eventType, status, _, _ string, details map[string]any) string {
	r.events = append(r.events, observerEvent{eventType: eventType, status: status, details: details})
	return ""
}

func (r *recordingObserver) RecordTimedEvent(eventType, status, _, _ string, _ time.Time, _ string, details map[string]any) string {
	r.events = append(r.events, observerEvent{eventType: eventType, status: status, details: details})
	return ""
}

func (r *recordingObserver) SetSummaryField(key string, value any) {
	if r.summary == nil {
		r.summary = make(map[string]any)
	}
	r.summary[key] = value
}

func (r *recordingObserver) IncrementSummaryCounter(key string, delta int) {
	if r.summary == nil {
		r.summary = make(map[string]any)
	}
	current, _ := r.summary[key].(int)
	r.summary[key] = current + delta
}

type recordingToolCallRecorder struct {
	records []ToolExecutionRecord
}

func (r *recordingToolCallRecorder) RecordToolExecution(_ context.Context, rec ToolExecutionRecord) error {
	r.records = append(r.records, rec)
	return nil
}

type testTool struct{}

func (t *testTool) Name() string               { return "echo" }
func (t *testTool) Description() string        { return "echo test tool" }
func (t *testTool) Risk() agenttools.RiskLevel { return agenttools.RiskSafe }
func (t *testTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *testTool) Execute(_ context.Context, _ string, _ *agenttools.Context) (*agenttools.Result, error) {
	return &agenttools.Result{Output: "hello", Source: "builtin"}, nil
}

type replayProtectedTestTool struct {
	calls int
}

func (t *replayProtectedTestTool) Name() string               { return "obsidian_write" }
func (t *replayProtectedTestTool) Description() string        { return "replay protected test tool" }
func (t *replayProtectedTestTool) Risk() agenttools.RiskLevel { return agenttools.RiskSafe }
func (t *replayProtectedTestTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *replayProtectedTestTool) Execute(_ context.Context, _ string, _ *agenttools.Context) (*agenttools.Result, error) {
	t.calls++
	return &agenttools.Result{Output: "wrote note", Source: "builtin"}, nil
}

type readOnlySearchTool struct {
	calls int
}

func (t *readOnlySearchTool) Name() string               { return "search_memories" }
func (t *readOnlySearchTool) Description() string        { return "search memories" }
func (t *readOnlySearchTool) Risk() agenttools.RiskLevel { return agenttools.RiskSafe }
func (t *readOnlySearchTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *readOnlySearchTool) Execute(_ context.Context, _ string, _ *agenttools.Context) (*agenttools.Result, error) {
	t.calls++
	return &agenttools.Result{Output: "found prior notes", Source: "builtin"}, nil
}

type inspectionEvidenceTool struct {
	name   string
	calls  int
	count  int
	output string
}

func (t *inspectionEvidenceTool) Name() string               { return t.name }
func (t *inspectionEvidenceTool) Description() string        { return "inspection evidence tool" }
func (t *inspectionEvidenceTool) Risk() agenttools.RiskLevel { return agenttools.RiskSafe }
func (t *inspectionEvidenceTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *inspectionEvidenceTool) Execute(_ context.Context, _ string, _ *agenttools.Context) (*agenttools.Result, error) {
	t.calls++
	proof := agenttools.NewInspectionProof("file_glob", t.name, ".", t.count).WithPattern("*.md")
	return &agenttools.Result{Output: t.output, Metadata: proof.Metadata(), Source: "builtin"}, nil
}

type artifactReadEvidenceTool struct {
	name    string
	calls   int
	path    string
	content string
}

func (t *artifactReadEvidenceTool) Name() string               { return t.name }
func (t *artifactReadEvidenceTool) Description() string        { return "artifact read evidence tool" }
func (t *artifactReadEvidenceTool) Risk() agenttools.RiskLevel { return agenttools.RiskSafe }
func (t *artifactReadEvidenceTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *artifactReadEvidenceTool) Execute(_ context.Context, _ string, _ *agenttools.Context) (*agenttools.Result, error) {
	t.calls++
	proof := agenttools.NewArtifactReadProof("workspace_file", t.path, t.content)
	return &agenttools.Result{Output: t.content, Metadata: proof.Metadata(), Source: "builtin"}, nil
}

type structuredArgsTool struct {
	calls    int
	lastArgs string
}

func (t *structuredArgsTool) Name() string               { return "query_table" }
func (t *structuredArgsTool) Description() string        { return "structured args test tool" }
func (t *structuredArgsTool) Risk() agenttools.RiskLevel { return agenttools.RiskSafe }
func (t *structuredArgsTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *structuredArgsTool) Execute(_ context.Context, params string, _ *agenttools.Context) (*agenttools.Result, error) {
	t.calls++
	t.lastArgs = params
	var payload struct {
		Table string `json:"table"`
	}
	if err := json.Unmarshal([]byte(params), &payload); err != nil {
		return nil, err
	}
	return &agenttools.Result{Output: "queried " + payload.Table, Source: "builtin"}, nil
}

type storeAwareTool struct {
	sawStore bool
}

func (t *storeAwareTool) Name() string               { return "cron" }
func (t *storeAwareTool) Description() string        { return "store-aware cron test tool" }
func (t *storeAwareTool) Risk() agenttools.RiskLevel { return agenttools.RiskCaution }
func (t *storeAwareTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *storeAwareTool) Execute(_ context.Context, _ string, tctx *agenttools.Context) (*agenttools.Result, error) {
	t.sawStore = tctx != nil && tctx.Store != nil
	if !t.sawStore {
		return nil, errors.New("database store not available")
	}
	return &agenttools.Result{Output: "Created cron job \"quiet ticker\" (schedule=*/5 * * * *)", Source: "builtin"}, nil
}

type bashTestTool struct {
	calls int
}

func (t *bashTestTool) Name() string               { return "bash" }
func (t *bashTestTool) Description() string        { return "bash test tool" }
func (t *bashTestTool) Risk() agenttools.RiskLevel { return agenttools.RiskSafe }
func (t *bashTestTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *bashTestTool) Execute(_ context.Context, _ string, _ *agenttools.Context) (*agenttools.Result, error) {
	t.calls++
	return &agenttools.Result{Output: "ran bash", Source: "builtin"}, nil
}

func TestLoop_SimpleResponse(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{Content: "Hello! How can I help?"},
		},
	}

	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("hi")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
	if loop.State() != StateDone {
		t.Errorf("expected done state, got %v", loop.State())
	}
}

func TestLoop_MaxTurns(t *testing.T) {
	// Return tool calls to keep the loop going.
	toolCall := llm.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "echo",
			Arguments: `{"message":"test"}`,
		},
	}
	mock := &mockCompleter{
		responses: make([]*llm.Response, 100),
	}
	for i := range mock.responses {
		mock.responses[i] = &llm.Response{
			Content:   "thinking...",
			ToolCalls: []llm.ToolCall{toolCall},
		}
	}

	cfg := LoopConfig{
		MaxTurns:      3,
		IdleThreshold: 10,
		LoopWindow:    100, // high window to prevent loop detection from firing before max turns
	}

	reg := NewToolRegistry()
	// Don't register echo — calls will fail with "unknown tool" but loop continues.

	deps := LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(cfg, deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("do something")

	_, err := loop.Run(context.Background(), session)
	// Hard max turn enforcement: should return ErrMaxTurns.
	if err == nil {
		t.Fatal("expected error from max turns enforcement")
	}
	if !errors.Is(err, ErrMaxTurns) {
		t.Errorf("expected ErrMaxTurns, got: %v", err)
	}

	if loop.TurnCount() > cfg.MaxTurns+1 {
		t.Errorf("expected max %d turns, got %d", cfg.MaxTurns, loop.TurnCount())
	}
}

func TestLoop_DetectsRepeatedToolCalls(t *testing.T) {
	sameCall := llm.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "echo",
			Arguments: `{"message":"same"}`,
		},
	}

	mock := &mockCompleter{
		responses: make([]*llm.Response, 20),
	}
	for i := range mock.responses {
		mock.responses[i] = &llm.Response{
			Content:   "still going",
			ToolCalls: []llm.ToolCall{sameCall},
		}
	}

	cfg := LoopConfig{
		MaxTurns:      20,
		IdleThreshold: 5,
		LoopWindow:    3,
	}

	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(cfg, deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("loop forever")

	_, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reason := loop.DoneReason()
	if reason != "loop detected: repeated tool calls" {
		t.Errorf("expected loop detection, got reason: %q", reason)
	}
}

func TestLoop_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	mock := &mockCompleter{
		responses: []*llm.Response{{Content: "should not reach"}},
	}
	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("hi")

	_, err := loop.Run(ctx, session)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestLoop_TEORReflectDisablesToolsAfterObservation(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "The tool returned hello."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-teor-1", "agent-1", "TestBot")
	session.AddUserMessage("run the tool and tell me what happened")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "The tool returned hello." {
		t.Fatalf("result = %q, want final reflected answer", result)
	}
	if len(mock.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(mock.requests))
	}
	if len(mock.requests[0].Tools) == 0 {
		t.Fatal("initial think request unexpectedly had no tools")
	}
	if len(mock.requests[1].Tools) != 0 {
		t.Fatalf("reflect request tools = %d, want 0", len(mock.requests[1].Tools))
	}
	// Contract: reflect request layers the TOTOF brief as a TRAILING
	// SYSTEM OVERLAY on top of full conversation history rather than
	// replacing history with a synthetic 2-message scaffold. The brief
	// (reflect instruction + TOTOF.Render()) lands as system messages at
	// the END of the request so it stays prominent as the model's most
	// recent contextual instruction. See R-AGENT-195 / R-AGENT-199.
	foundReflectInstruction := false
	foundTOTOFTask := false
	foundOriginalUserTask := false
	for _, msg := range mock.requests[1].Messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "Post-observation reflection mode") {
			foundReflectInstruction = true
		}
		if msg.Role == "system" && strings.Contains(msg.Content, "TASK") && strings.Contains(msg.Content, "KEY TOOL OUTCOMES") {
			foundTOTOFTask = true
		}
		if msg.Role == "user" && strings.Contains(msg.Content, "run the tool and tell me what happened") {
			foundOriginalUserTask = true
		}
	}
	if !foundReflectInstruction {
		t.Fatal("reflect request missing reflection contract system message")
	}
	if !foundTOTOFTask {
		t.Fatal("reflect request missing TOTOF payload (must be a trailing system overlay, not a synthetic user message)")
	}
	if !foundOriginalUserTask {
		t.Fatal("reflect request missing original user message — history was replaced instead of layered (regression)")
	}
}

func TestLoop_TEORReflectEmptyFinalizesFromObservedToolEvidence(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: ""},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-teor-empty-reflect", "agent-1", "TestBot")
	session.AddUserMessage("run the tool and summarize the result")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result) != "hello" {
		t.Fatalf("result = %q, want observed tool evidence", result)
	}
	if session.LastAssistantContent() != "hello" {
		t.Fatalf("last assistant = %q, want observed tool evidence", session.LastAssistantContent())
	}
	if session.LastAssistantPhase() != "reflect" {
		t.Fatalf("last assistant phase = %q, want reflect", session.LastAssistantPhase())
	}
}

func TestLoop_TEORReflectMayExplicitlyReopenExecution(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "CONTINUE_EXECUTION\nNeed one more execution step to finish the task."},
			{Content: "Done after explicit continuation."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-teor-2", "agent-1", "TestBot")
	session.AddUserMessage("perform the task completely")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done after explicit continuation." {
		t.Fatalf("result = %q, want continued final answer", result)
	}
	if len(mock.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(mock.requests))
	}
	if len(mock.requests[1].Tools) != 0 {
		t.Fatalf("reflect request tools = %d, want 0", len(mock.requests[1].Tools))
	}
	if len(mock.requests[2].Tools) == 0 {
		t.Fatal("post-reflection think request unexpectedly had no tools")
	}
	// Contract: continuation think request layers the continuation
	// brief as a TRAILING SYSTEM OVERLAY on top of full conversation
	// history (including prior assistant tool_calls and tool results).
	// See R-AGENT-196 / R-AGENT-199.
	foundContinuationInstruction := false
	foundRemainingWork := false
	foundOriginalUserTask := false
	for _, msg := range mock.requests[2].Messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "Post-observation continuation mode") {
			foundContinuationInstruction = true
		}
		if msg.Role == "system" && strings.Contains(msg.Content, "REMAINING WORK") {
			foundRemainingWork = true
		}
		if msg.Role == "user" && strings.Contains(msg.Content, "perform the task completely") {
			foundOriginalUserTask = true
		}
	}
	if !foundContinuationInstruction {
		t.Fatal("post-reflection continuation request missing continuation contract system message")
	}
	if !foundRemainingWork {
		t.Fatal("post-reflection continuation request missing canonical continuation payload (must be a trailing system overlay)")
	}
	if !foundOriginalUserTask {
		t.Fatal("post-reflection continuation request missing original user message — history was replaced instead of layered (regression)")
	}
	if got := session.LastAssistantContent(); got != "Done after explicit continuation." {
		t.Fatalf("last assistant content = %q, want continued final answer", got)
	}
	// CONTINUE_EXECUTION sentinel must NEVER be persisted to session history.
	// With prefix-only detection, a model that emitted prose before the
	// sentinel would leak the entire response (sentinel and all) into chat.
	for _, msg := range session.Messages() {
		if strings.Contains(msg.Content, reflectContinuePrefix) {
			t.Fatalf("session message contained literal %q sentinel: %q (regression — sentinel must be consumed by framework, never persisted)", reflectContinuePrefix, msg.Content)
		}
	}
}

func TestLoop_SuppressesPlaceholderContentForToolCalls(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Content: "[assistant message]",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "Done."},
		},
	}

	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("check something")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Fatalf("result = %q, want %q", result, "Done.")
	}

	msgs := session.Messages()
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}
	if msgs[1].Role != "assistant" {
		t.Fatalf("message[1].Role = %q, want assistant", msgs[1].Role)
	}
	if msgs[1].Content != "" {
		t.Fatalf("placeholder assistant content leaked into history: %q", msgs[1].Content)
	}
	if len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool call count = %d, want 1", len(msgs[1].ToolCalls))
	}
}

func TestLoop_TerminatesSameRouteNoProgressChurn(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "[agent message]"},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "[agent message]"},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "[agent message]"},
		},
	}
	cfg := DefaultLoopConfig()
	cfg.MaxSameRouteNoProgress = 2
	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(cfg, deps)
	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("create a note")
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(context.Background(), obs)

	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected no fabricated termination content, got %q", result)
	}
	if got := loop.DoneReason(); got != "loop terminated: same-route no-progress churn" {
		t.Fatalf("done reason = %q", got)
	}
	if got := obs.summary["termination_cause"]; got != "same_route_no_progress" {
		t.Fatalf("termination_cause = %v, want same_route_no_progress", got)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "loop_terminated" {
			found = true
			if got := ev.details["reason_code"]; got != "same_route_no_progress" {
				t.Fatalf("reason_code = %v, want same_route_no_progress", got)
			}
			if got := ev.details["route"]; got != "moonshot/kimi-k2-turbo-preview" {
				t.Fatalf("route = %v", got)
			}
		}
	}
	if !found {
		t.Fatal("expected loop_terminated event")
	}
}

func TestLoop_PersistsToolExecutionAndIncrementsRCACounter(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"hello"}`,
					},
				}},
			},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "Done."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	recorder := &recordingToolCallRecorder{}
	deps := LoopDeps{
		LLM:      mock,
		Tools:    reg,
		Recorder: recorder,
		Context:  NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("say hello")
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(core.WithTurnID(context.Background(), "turn-1"), obs)

	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Fatalf("result = %q, want Done.", result)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %d, want 1", len(recorder.records))
	}
	if recorder.records[0].TurnID != "turn-1" {
		t.Fatalf("turn id = %q", recorder.records[0].TurnID)
	}
	if recorder.records[0].Status != "success" {
		t.Fatalf("status = %q, want success", recorder.records[0].Status)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_finished" {
			found = true
			if got := ev.details["tool_name"]; got != "echo" {
				t.Fatalf("tool_name = %v", got)
			}
		}
	}
	if !found {
		t.Fatal("expected tool_call_finished event")
	}
	if got := obs.summary["tool_call_count"]; got != 1 {
		t.Fatalf("tool_call_count = %v, want 1", got)
	}
}

func TestLoop_PassesDatabaseStoreIntoToolContext(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &storeAwareTool{}
	tools := NewToolRegistry()
	tools.Register(tool)

	completer := &mockCompleter{responses: []*llm.Response{
		{
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "cron",
					Arguments: `{"action":"create","name":"quiet ticker","schedule":"*/5 * * * *","task":"heartbeat"}`,
				},
			}},
			FinishReason: "tool_calls",
		},
		{
			Content:      "Created cron job \"quiet ticker\" (schedule=*/5 * * * *)",
			FinishReason: "stop",
		},
	}}

	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     completer,
		Tools:   tools,
		Store:   store,
		Context: NewContextBuilder(DefaultContextConfig()),
	})

	session := NewSession("sess", "agent", "Duncan")
	session.AddUserMessage("schedule quiet ticker")

	if _, err := loop.Run(context.Background(), session); err != nil {
		t.Fatalf("loop run failed: %v", err)
	}
	if !tool.sawStore {
		t.Fatal("tool context did not receive the authoritative database store")
	}
}

func TestLoop_NormalizesMalformedStructuredToolArgumentsBeforeExecution(t *testing.T) {
	toolCall := llm.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "query_table",
			Arguments: `{"table": "sessions, "filters": {}{"table": "sessions", "filters": {}}`,
		},
	}
	mock := &mockCompleter{
		responses: []*llm.Response{
			{Content: "", ToolCalls: []llm.ToolCall{toolCall}},
			{Content: "done"},
		},
	}
	recorder := &recordingToolCallRecorder{}
	obs := &recordingObserver{}
	reg := NewToolRegistry()
	tool := &structuredArgsTool{}
	reg.Register(tool)

	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:         mock,
		Tools:       reg,
		Recorder:    recorder,
		Context:     NewContextBuilder(DefaultContextConfig()),
		Normalizers: agenttools.NewNormalizationFactory(),
	})

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("query the sessions table")

	ctx := llm.WithInferenceObserver(context.Background(), obs)
	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Fatalf("result = %q, want done", result)
	}
	if tool.calls != 1 {
		t.Fatalf("tool calls = %d, want 1", tool.calls)
	}
	if tool.lastArgs != `{"table": "sessions", "filters": {}}` {
		t.Fatalf("normalized args = %q", tool.lastArgs)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_normalized" {
			found = true
			if got := ev.details["transformer"]; got != "embedded_json_object" {
				t.Fatalf("transformer = %v, want embedded_json_object", got)
			}
		}
	}
	if !found {
		t.Fatal("expected tool_call_normalized event")
	}
	if len(recorder.records) == 0 || recorder.records[0].Status != "success" {
		t.Fatalf("expected persisted success record, got %+v", recorder.records)
	}
}

func TestLoop_RejectsMalformedToolArgumentsWithoutQualifiedTransformer(t *testing.T) {
	toolCall := llm.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "query_table",
			Arguments: `table=sessions limit=10`,
		},
	}
	mock := &mockCompleter{
		responses: []*llm.Response{
			{Content: "", ToolCalls: []llm.ToolCall{toolCall}},
			{Content: "done"},
		},
	}
	recorder := &recordingToolCallRecorder{}
	obs := &recordingObserver{}
	reg := NewToolRegistry()
	tool := &structuredArgsTool{}
	reg.Register(tool)

	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:         mock,
		Tools:       reg,
		Recorder:    recorder,
		Context:     NewContextBuilder(DefaultContextConfig()),
		Normalizers: agenttools.NewNormalizationFactory(),
	})

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("query the sessions table")

	ctx := llm.WithInferenceObserver(context.Background(), obs)
	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Fatalf("result = %q, want done", result)
	}
	if tool.calls != 0 {
		t.Fatalf("tool should not have executed, calls=%d", tool.calls)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_normalization_failed" {
			found = true
			if got := ev.details["disposition"]; got != string(agenttools.NormalizationNoQualifiedTransformer) {
				t.Fatalf("disposition = %v", got)
			}
		}
	}
	if !found {
		t.Fatal("expected tool_call_normalization_failed event")
	}
	if len(recorder.records) == 0 || recorder.records[0].Status != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments record, got %+v", recorder.records)
	}
}

func TestLoop_SuppressesReplayOfSuccessfulSideEffectingToolCalls(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "obsidian_write",
						Arguments: `{"path":"note.md","content":"# test"}`,
					},
				}},
			},
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-2",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "obsidian_write",
						Arguments: `{"path":"note.md","content":"# test"}`,
					},
				}},
			},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "Done."},
		},
	}
	reg := NewToolRegistry()
	tool := &replayProtectedTestTool{}
	reg.Register(tool)
	recorder := &recordingToolCallRecorder{}
	deps := LoopDeps{
		LLM:      mock,
		Tools:    reg,
		Recorder: recorder,
		Context:  NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("write the note")
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(core.WithTurnID(context.Background(), "turn-replay"), obs)

	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Fatalf("result = %q, want Done.", result)
	}
	if tool.calls != 1 {
		t.Fatalf("tool Execute called %d times, want 1", tool.calls)
	}
	if len(recorder.records) != 2 {
		t.Fatalf("records = %d, want 2", len(recorder.records))
	}
	if recorder.records[0].Status != "success" {
		t.Fatalf("first record status = %q, want success", recorder.records[0].Status)
	}
	if recorder.records[1].Status != "suppressed_replay" {
		t.Fatalf("second record status = %q, want suppressed_replay", recorder.records[1].Status)
	}
	if got := obs.summary["replay_suppression_count"]; got != 1 {
		t.Fatalf("replay_suppression_count = %v, want 1", got)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_replay_suppressed" {
			found = true
			if got := ev.details["tool_name"]; got != "obsidian_write" {
				t.Fatalf("tool_name = %v", got)
			}
			if got := ev.details["prior_success_count"]; got != 1 {
				t.Fatalf("prior_success_count = %v, want 1", got)
			}
		}
	}
	if !found {
		t.Fatal("expected tool_call_replay_suppressed event")
	}
}

func TestLoop_RejectsToolCallOutsideSelectedToolSurface(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-bash",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "bash",
						Arguments: `{"command":"cat requirements.txt"}`,
					},
				}},
			},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "Done."},
		},
	}
	reg := NewToolRegistry()
	bash := &bashTestTool{}
	reg.Register(bash)
	recorder := &recordingToolCallRecorder{}
	deps := LoopDeps{
		LLM:      mock,
		Tools:    reg,
		Recorder: recorder,
		Context:  NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.SetSelectedToolDefs([]llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "read_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "write_file"}},
	})
	session.AddUserMessage("read the source file and write outputs")
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(core.WithTurnID(context.Background(), "turn-out-of-surface"), obs)

	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Fatalf("result = %q, want Done.", result)
	}
	if bash.calls != 0 {
		t.Fatalf("bash tool executed %d times, want 0", bash.calls)
	}
	if len(recorder.records) == 0 || recorder.records[0].Status != "out_of_surface" {
		t.Fatalf("expected out_of_surface record, got %+v", recorder.records)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_blocked" {
			found = true
			if got := ev.details["reason_code"]; got != "tool_not_selected" {
				t.Fatalf("reason_code = %v, want tool_not_selected", got)
			}
		}
	}
	if !found {
		t.Fatal("expected tool_call_blocked event")
	}
}

func TestLoop_SuppressesReplayOfSuccessfulSideEffectingToolCallsByProtectedResource(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "obsidian_write",
						Arguments: `{"path":"note.md","content":"# first"}`,
					},
				}},
			},
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-2",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "obsidian_write",
						Arguments: `{"path":"note.md","content":"# second"}`,
					},
				}},
			},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "Done."},
		},
	}
	reg := NewToolRegistry()
	tool := &replayProtectedTestTool{}
	reg.Register(tool)
	recorder := &recordingToolCallRecorder{}
	deps := LoopDeps{
		LLM:      mock,
		Tools:    reg,
		Recorder: recorder,
		Context:  NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("write and then rewrite the note")
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(core.WithTurnID(context.Background(), "turn-replay-resource"), obs)

	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Fatalf("result = %q, want Done.", result)
	}
	if tool.calls != 1 {
		t.Fatalf("tool Execute called %d times, want 1", tool.calls)
	}
	if len(recorder.records) != 2 {
		t.Fatalf("records = %d, want 2", len(recorder.records))
	}
	if recorder.records[1].Status != "suppressed_replay" {
		t.Fatalf("second record status = %q, want suppressed_replay", recorder.records[1].Status)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_replay_suppressed" {
			found = true
			if got := ev.details["protected_resource"]; got != "note.md" {
				t.Fatalf("protected_resource = %v, want note.md", got)
			}
		}
	}
	if !found {
		t.Fatal("expected tool_call_replay_suppressed event")
	}
}

func TestLoop_SuppressesReplayFromSessionHistoryAcrossFreshLoop(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-2",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "obsidian_write",
						Arguments: `{"path":"note.md","content":"# second"}`,
					},
				}},
			},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "Done."},
		},
	}
	reg := NewToolRegistry()
	tool := &replayProtectedTestTool{}
	reg.Register(tool)
	recorder := &recordingToolCallRecorder{}
	deps := LoopDeps{
		LLM:      mock,
		Tools:    reg,
		Recorder: recorder,
		Context:  NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("write the note")
	session.AddAssistantMessage("", []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "obsidian_write",
			Arguments: `{"path":"note.md","content":"# first"}`,
		},
	}})
	proof := agenttools.NewArtifactProof("obsidian_note", "note.md", "# first", false)
	session.AddToolResultWithMetadata("call-1", "obsidian_write", proof.Output(), proof.Metadata(), false)
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(core.WithTurnID(context.Background(), "turn-replay-history"), obs)

	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Fatalf("result = %q, want Done.", result)
	}
	if tool.calls != 0 {
		t.Fatalf("tool Execute called %d times, want 0 because replay should be suppressed from history", tool.calls)
	}
	if len(recorder.records) == 0 || recorder.records[0].Status != "suppressed_replay" {
		t.Fatalf("expected suppressed_replay record, got %+v", recorder.records)
	}
}

func TestLoop_TerminatesExploratoryToolChurnOnDirectExecutionTurns(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "search_memories",
						Arguments: `{"query":"deploy workflow"}`,
					},
				}},
			},
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-2",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "search_memories",
						Arguments: `{"query":"deploy rollback"}`,
					},
				}},
			},
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-3",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "search_memories",
						Arguments: `{"query":"deploy canary"}`,
					},
				}},
			},
		},
	}
	reg := NewToolRegistry()
	tool := &readOnlySearchTool{}
	reg.Register(tool)
	recorder := &recordingToolCallRecorder{}
	cfg := DefaultLoopConfig()
	cfg.MaxReadOnlyExploration = 2
	loop := NewLoop(cfg, LoopDeps{
		LLM:      mock,
		Tools:    reg,
		Recorder: recorder,
		Context:  NewContextBuilder(DefaultContextConfig()),
	})

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("create the deployment workflow file")
	session.SetTaskVerificationHints("task", "simple", "execute_directly", nil)
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(context.Background(), obs)

	result, err := loop.Run(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result), "gathering context without taking action") {
		t.Fatalf("result = %q, want exploratory churn explanation", result)
	}
	if got := loop.DoneReason(); got != "loop terminated: exploratory read-only tool churn" {
		t.Fatalf("done reason = %q", got)
	}
	if tool.calls != 2 {
		t.Fatalf("tool Execute called %d times, want 2 before suppression", tool.calls)
	}
	if len(recorder.records) != 2 {
		t.Fatalf("records = %d, want 2", len(recorder.records))
	}
	if got := obs.summary["termination_cause"]; got != "exploratory_tool_churn" {
		t.Fatalf("termination_cause = %v, want exploratory_tool_churn", got)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "loop_terminated" {
			found = true
			if got := ev.details["reason_code"]; got != "exploratory_tool_churn" {
				t.Fatalf("reason_code = %v, want exploratory_tool_churn", got)
			}
			if got := ev.details["exploration_streak"]; got != 2 {
				t.Fatalf("exploration_streak = %v, want 2", got)
			}
			if got := ev.details["tool_name"]; got != "search_memories" {
				t.Fatalf("tool_name = %v, want search_memories", got)
			}
		}
	}
	if !found {
		t.Fatal("expected loop_terminated event")
	}
}

func TestLoop_RetriesPlaceholderOnlyFinalResponse(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{Content: "[assistant message]"},
			{Content: "Actual answer."},
		},
	}

	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("what's new?")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Actual answer." {
		t.Fatalf("result = %q, want %q", result, "Actual answer.")
	}

	msgs := session.Messages()
	if len(msgs) != 2 {
		t.Fatalf("message count = %d, want 2", len(msgs))
	}
	if msgs[1].Content != "Actual answer." {
		t.Fatalf("assistant content = %q, want %q", msgs[1].Content, "Actual answer.")
	}
}

func TestLoop_RetriesAgentMessagePlaceholderOnlyFinalResponse(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{Content: "[agent message]"},
			{Content: "Actual answer."},
		},
	}

	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("create the note")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Actual answer." {
		t.Fatalf("result = %q, want %q", result, "Actual answer.")
	}
}

func TestLoop_RetriesPromissoryOnlyDirectExecutionResponse(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{Content: "Let me check what's inside the workspace right now."},
			{Content: "42"},
		},
	}

	deps := LoopDeps{
		LLM:     mock,
		Tools:   NewToolRegistry(),
		Context: NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("Count markdown files recursively in the workspace and return only the number.")
	session.SetTaskVerificationHints("task", "simple", "execute_directly", nil)
	session.SetTurnEnvelopePolicy("standard", "focused_inspection", "test")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Fatalf("result = %q, want %q", result, "42")
	}

	msgs := session.Messages()
	if len(msgs) != 2 {
		t.Fatalf("message count = %d, want 2", len(msgs))
	}
	if strings.Contains(strings.ToLower(msgs[1].Content), "let me check") {
		t.Fatalf("promissory filler leaked into history: %q", msgs[1].Content)
	}
}

func TestLoop_FocusedInspectionEvidenceDoesNotCountAsExploratoryChurn(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "gpt-4o-mini",
				Provider: "openai",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "glob_files",
						Arguments: `{"path":".","pattern":"*.md"}`,
					},
				}},
			},
			{
				Model:    "gpt-4o-mini",
				Provider: "openai",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-2",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "glob_files",
						Arguments: `{"path":".","pattern":"*.txt"}`,
					},
				}},
			},
			{Content: "2"},
		},
	}
	reg := NewToolRegistry()
	tool := &inspectionEvidenceTool{name: "glob_files", count: 2, output: "a.md\nb.md"}
	reg.Register(tool)

	cfg := DefaultLoopConfig()
	cfg.MaxReadOnlyExploration = 1
	loop := NewLoop(cfg, LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: NewContextBuilder(DefaultContextConfig()),
	})

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("Count markdown files recursively in the workspace and return only the number.")
	session.SetTaskVerificationHints("task", "simple", "execute_directly", nil)
	session.SetTurnEnvelopePolicy("standard", "focused_inspection", "test")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "2" {
		t.Fatalf("result = %q, want %q", result, "2")
	}
	if tool.calls != 2 {
		t.Fatalf("tool calls = %d, want 2", tool.calls)
	}
	if got := loop.DoneReason(); got != "" {
		t.Fatalf("done reason = %q, want empty", got)
	}
}

func TestLoop_FocusedAnalysisAuthoringEvidenceDoesNotCountAsExploratoryChurn(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "gpt-4o-mini",
				Provider: "openai",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "glob_files",
						Arguments: `{"path":".","pattern":"*.md"}`,
					},
				}},
			},
			{
				Model:    "gpt-4o-mini",
				Provider: "openai",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-2",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "read_file",
						Arguments: `{"path":"report.md"}`,
					},
				}},
			},
			{Content: "Wrote the requested summary from the inspected source evidence."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&inspectionEvidenceTool{name: "glob_files", count: 3, output: "report.md\nnotes.md\nplan.md"})
	reg.Register(&artifactReadEvidenceTool{name: "read_file", path: "report.md", content: "# Report\n- finding"})

	cfg := DefaultLoopConfig()
	cfg.MaxReadOnlyExploration = 1
	loop := NewLoop(cfg, LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: NewContextBuilder(DefaultContextConfig()),
	})

	session := NewSession("sess-1", "agent-1", "TestBot")
	session.AddUserMessage("Find the latest report inputs, read the most relevant file, and summarize it in a concise note.")
	session.SetTaskVerificationHints("task", "simple", "execute_directly", nil)
	session.SetTurnEnvelopePolicy("standard", "focused_analysis_authoring", "test")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "requested summary") {
		t.Fatalf("result = %q, want focused analysis authoring completion", result)
	}
	if got := loop.DoneReason(); got != "" {
		t.Fatalf("done reason = %q, want empty", got)
	}
}

func TestTurnProfileCountsReadOnlyEvidenceAsProgress_FocusedSourceCode(t *testing.T) {
	if !turnProfileCountsReadOnlyEvidenceAsProgress("focused_source_code") {
		t.Fatal("focused source code profile should count bounded read-only evidence as progress")
	}
}

// TestLoop_ApprovalManagerBlocksToolBeforeRegistry verifies the v1.0.8
// wiring: when ApprovalManager.ClassifyTool reports ToolBlocked, the
// loop must short-circuit BEFORE dispatching to the tool registry,
// emit a `tool_call_blocked_by_approval` observer event, and never
// invoke the underlying tool.
func TestLoop_ApprovalManagerBlocksToolBeforeRegistry(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-blocked",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "bash",
						Arguments: `{"command":"echo hi"}`,
					},
				}},
			},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "Halted."},
		},
	}
	reg := NewToolRegistry()
	bash := &bashTestTool{}
	reg.Register(bash)
	recorder := &recordingToolCallRecorder{}
	approvals := policy.NewApprovalManager(policy.ApprovalsConfig{
		Enabled:      true,
		BlockedTools: []string{"bash"},
	})
	deps := LoopDeps{
		LLM:       mock,
		Tools:     reg,
		Recorder:  recorder,
		Approvals: approvals,
		Context:   NewContextBuilder(DefaultContextConfig()),
	}
	loop := NewLoop(DefaultLoopConfig(), deps)

	sess := NewSession("sess-blocked", "agent-1", "TestBot")
	sess.SetSelectedToolDefs([]llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "bash"}},
	})
	sess.AddUserMessage("run a command")
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(core.WithTurnID(context.Background(), "turn-blocked"), obs)

	result, err := loop.Run(ctx, sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Halted." {
		t.Fatalf("result = %q, want Halted.", result)
	}
	if bash.calls != 0 {
		t.Fatalf("bash invoked %d times despite block, want 0", bash.calls)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_blocked_by_approval" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool_call_blocked_by_approval event, got %+v", obs.events)
	}
}

// TestLoop_ApprovalManagerGatedToolStillExecutes verifies the
// transitional gating contract: until the human-in-the-loop approval
// path is wired through the API layer, gated tools execute with a
// warning observer event so operators can see the call in audit logs.
func TestLoop_ApprovalManagerGatedToolStillExecutes(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				Model:    "kimi-k2-turbo-preview",
				Provider: "moonshot",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-gated",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "bash",
						Arguments: `{"command":"echo hi"}`,
					},
				}},
			},
			{Model: "kimi-k2-turbo-preview", Provider: "moonshot", Content: "ok"},
		},
	}
	reg := NewToolRegistry()
	bash := &bashTestTool{}
	reg.Register(bash)
	approvals := policy.NewApprovalManager(policy.ApprovalsConfig{
		Enabled:    true,
		GatedTools: []string{"bash"},
	})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:       mock,
		Tools:     reg,
		Approvals: approvals,
		Context:   NewContextBuilder(DefaultContextConfig()),
	})

	sess := NewSession("sess-gated", "agent-1", "TestBot")
	sess.SetSelectedToolDefs([]llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "bash"}},
	})
	sess.AddUserMessage("run a command")
	obs := &recordingObserver{}
	ctx := llm.WithInferenceObserver(core.WithTurnID(context.Background(), "turn-gated"), obs)

	if _, err := loop.Run(ctx, sess); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bash.calls != 1 {
		t.Fatalf("gated tool calls = %d, want 1", bash.calls)
	}
	found := false
	for _, ev := range obs.events {
		if ev.eventType == "tool_call_gated" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool_call_gated event, got %+v", obs.events)
	}
}

func TestLoop_ReflectPreservesChatHistory(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "Reflection finalized."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-reflect-history", "agent-1", "TestBot")
	session.AddUserMessage("run echo and reflect")

	_, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.requests) < 2 {
		t.Fatalf("requests = %d, want >= 2", len(mock.requests))
	}
	refReq := mock.requests[1]
	var sawUser, sawAssistantTool, sawTool bool
	for _, msg := range refReq.Messages {
		switch msg.Role {
		case "user":
			if strings.Contains(msg.Content, "run echo and reflect") {
				sawUser = true
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				sawAssistantTool = true
			}
		case "tool":
			if strings.Contains(msg.Content, "hello") {
				sawTool = true
			}
		}
	}
	if !sawUser {
		t.Fatal("reflect request missing original user turn (R-AGENT-195)")
	}
	if !sawAssistantTool {
		t.Fatal("reflect request missing assistant tool-call message from history (R-AGENT-195)")
	}
	if !sawTool {
		t.Fatal("reflect request missing tool result message from history (R-AGENT-195)")
	}
}

func TestLoop_ReflectPreservesWorkingMemoryContext(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetMemory("[Active Memory]\n\n[Working State]\n- durable task: audit skill use regression")
	contextBuilder.SetMemoryIndex("[Memory Index]\n- recall_memory(\"skill-regression\")")
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "Reflection finalized with memory intact."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-reflect-memory", "agent-1", "TestBot")
	session.AddUserMessage("run echo and keep our working memory")

	if _, err := loop.Run(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.requests) < 2 {
		t.Fatalf("requests = %d, want >= 2", len(mock.requests))
	}
	assertRequestContainsOrderedSystemContext(t, mock.requests[1], "[Working State]", "[Memory Index]", "FINALIZATION INSTRUCTION")
}

func TestLoop_ContinuationPreservesChatHistory(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "CONTINUE_EXECUTION\nNeed another step."},
			{Content: "Finished after continuation."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-continuation-history", "agent-1", "TestBot")
	session.AddUserMessage("perform the task")

	_, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.requests) < 3 {
		t.Fatalf("requests = %d, want >= 3", len(mock.requests))
	}
	contReq := mock.requests[2]
	var sawUser, sawAssistantTool, sawTool bool
	for _, msg := range contReq.Messages {
		switch msg.Role {
		case "user":
			if strings.Contains(msg.Content, "perform the task") {
				sawUser = true
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				sawAssistantTool = true
			}
		case "tool":
			if strings.Contains(msg.Content, "hello") {
				sawTool = true
			}
		}
	}
	if !sawUser || !sawAssistantTool || !sawTool {
		t.Fatalf("continuation request missing history: user=%v assistant_tool=%v tool=%v (R-AGENT-196)", sawUser, sawAssistantTool, sawTool)
	}
}

func TestLoop_ContinuationPreservesWorkingMemoryContext(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetMemory("[Active Memory]\n\n[Working State]\n- durable task: continue after reflection")
	contextBuilder.SetMemoryIndex("[Memory Index]\n- recall_memory(\"continuation-regression\")")
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "CONTINUE_EXECUTION\nNeed one more bounded pass."},
			{Content: "Finished after continuation with memory intact."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-continuation-memory", "agent-1", "TestBot")
	session.AddUserMessage("perform the task and preserve memory")

	if _, err := loop.Run(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.requests) < 3 {
		t.Fatalf("requests = %d, want >= 3", len(mock.requests))
	}
	assertRequestContainsOrderedSystemContext(t, mock.requests[2], "[Working State]", "[Memory Index]", "CONTINUATION INSTRUCTION")
}

func assertRequestContainsOrderedSystemContext(t *testing.T, req *llm.Request, memoryNeedle, indexNeedle, overlayNeedle string) {
	t.Helper()
	memoryIdx, indexIdx, overlayIdx := -1, -1, -1
	for i, msg := range req.Messages {
		if msg.Role != "system" {
			continue
		}
		if strings.Contains(msg.Content, memoryNeedle) {
			memoryIdx = i
		}
		if strings.Contains(msg.Content, indexNeedle) {
			indexIdx = i
		}
		if strings.Contains(msg.Content, overlayNeedle) {
			overlayIdx = i
		}
	}
	if memoryIdx < 0 {
		t.Fatalf("request missing working-memory context %q (R-AGENT-202)", memoryNeedle)
	}
	if indexIdx < 0 {
		t.Fatalf("request missing memory index %q (R-AGENT-202)", indexNeedle)
	}
	if overlayIdx < 0 {
		t.Fatalf("request missing trailing overlay %q", overlayNeedle)
	}
	if !(memoryIdx < overlayIdx && indexIdx < overlayIdx) {
		t.Fatalf("memory/index must precede trailing overlay: memory=%d index=%d overlay=%d", memoryIdx, indexIdx, overlayIdx)
	}
}

func assertReflectMidMessageContinueLeavesNoSentinelInSession(t *testing.T) {
	t.Helper()
	assertReflectContinueLeavesNoSentinelInSession(t, "sess-mid-sentinel", "The observation window looks incomplete.\nCONTINUE_EXECUTION need one more evidence pass")
}

func assertReflectContinueLeavesNoSentinelInSession(t *testing.T, sessionID, reflectContent string) {
	t.Helper()
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: reflectContent},
			{Content: "All evidence collected; done."},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession(sessionID, "agent-1", "TestBot")
	session.AddUserMessage("run tool then finish")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "All evidence collected; done." {
		t.Fatalf("result = %q", result)
	}
	if len(mock.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(mock.requests))
	}
	for _, msg := range session.Messages() {
		if strings.Contains(msg.Content, reflectContinuePrefix) {
			t.Fatalf("sentinel leaked into session: %q (R-AGENT-197)", msg.Content)
		}
	}
}

func TestLoop_ReflectDetectsMidMessageContinueExecution(t *testing.T) {
	assertReflectMidMessageContinueLeavesNoSentinelInSession(t)
}

func TestLoop_ReflectDetectsInlineContinueExecution(t *testing.T) {
	assertReflectContinueLeavesNoSentinelInSession(t, "sess-inline-sentinel", "The observation window looks incomplete. CONTINUE_EXECUTION need one more evidence pass")
}

func TestLoop_ReflectPromissoryExecutionContinuesInsteadOfFinalizing(t *testing.T) {
	assertReflectContinueLeavesNoSentinelInSession(t, "sess-promissory-reflect", "The first page did not contain the exact score.\n\nNext, I will continue by using the browser tool to extract the score.")
}

func TestLoop_ReflectEmbeddedPromissoryExecutionContinuesInsteadOfFinalizing(t *testing.T) {
	assertReflectContinueLeavesNoSentinelInSession(t, "sess-embedded-promissory-reflect", "The preliminary fetch did not provide the actual score. I will now proceed to use the browser tool to extract the relevant information.\n\nContinuing execution to extract the score.")
}

func TestLoop_ReflectNeverPersistsContinueExecutionSentinel(t *testing.T) {
	t.Run("mid_line_sentinel", func(t *testing.T) {
		assertReflectMidMessageContinueLeavesNoSentinelInSession(t)
	})
	t.Run("inline_sentinel", func(t *testing.T) {
		assertReflectContinueLeavesNoSentinelInSession(t, "sess-never-sentinel-inline", "I will continue. CONTINUE_EXECUTION parse the retrieved HTML")
	})
	t.Run("empty_reflect_with_tool_synth", func(t *testing.T) {
		contextBuilder := NewContextBuilder(DefaultContextConfig())
		contextBuilder.SetTools([]llm.ToolDef{{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        "echo",
				Description: "echo test tool",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		}})
		mock := &mockCompleter{
			responses: []*llm.Response{
				{
					ToolCalls: []llm.ToolCall{{
						ID:   "call-1",
						Type: "function",
						Function: llm.ToolCallFunc{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				{Content: ""},
			},
		}
		reg := NewToolRegistry()
		reg.Register(&testTool{})
		loop := NewLoop(DefaultLoopConfig(), LoopDeps{
			LLM:     mock,
			Tools:   reg,
			Context: contextBuilder,
		})
		session := NewSession("sess-never-sentinel-synth", "agent-1", "TestBot")
		session.AddUserMessage("run echo")
		if _, err := loop.Run(context.Background(), session); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, msg := range session.Messages() {
			if strings.Contains(msg.Content, reflectContinuePrefix) {
				t.Fatalf("sentinel in session after synth path: %q (R-AGENT-198)", msg.Content)
			}
		}
	})
}

func TestScrubControlSentinels_RemovesInlineSentinelText(t *testing.T) {
	got := scrubControlSentinels("Useful prefix. CONTINUE_EXECUTION parse the retrieved HTML")
	if got != "Useful prefix." {
		t.Fatalf("scrubControlSentinels() = %q, want prefix only", got)
	}
}

func TestLoop_ReflectBriefRendersFullTOTOFSections(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &requestCapturingCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: "ok"},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-totof-sections", "agent-1", "TestBot")
	session.AddUserMessage("invoke echo")

	if _, err := loop.Run(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	refReq := mock.requests[1]
	var brief string
	for i := len(refReq.Messages) - 1; i >= 0; i-- {
		m := refReq.Messages[i]
		if m.Role != "system" {
			continue
		}
		if strings.Contains(m.Content, "TASK\n") && strings.Contains(m.Content, "KEY TOOL OUTCOMES") {
			brief = m.Content
			break
		}
	}
	if brief == "" {
		t.Fatal("no trailing TOTOF system brief found (R-AGENT-199)")
	}
	for _, needle := range []string{
		"TASK\n",
		"AUTHORITATIVE OBSERVED RESULTS",
		"KEY TOOL OUTCOMES",
		"FINALIZATION INSTRUCTION",
	} {
		if !strings.Contains(brief, needle) {
			t.Fatalf("TOTOF brief missing %q\n---\n%s", needle, brief)
		}
	}
}

func TestLoop_ReflectStillUsesSynthesizeFallbackOnEmptyContent(t *testing.T) {
	contextBuilder := NewContextBuilder(DefaultContextConfig())
	contextBuilder.SetTools([]llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFuncDef{
			Name:        "echo",
			Description: "echo test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})

	mock := &mockCompleter{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "echo",
						Arguments: `{"message":"test"}`,
					},
				}},
			},
			{Content: ""},
		},
	}
	reg := NewToolRegistry()
	reg.Register(&testTool{})
	loop := NewLoop(DefaultLoopConfig(), LoopDeps{
		LLM:     mock,
		Tools:   reg,
		Context: contextBuilder,
	})

	session := NewSession("sess-synth-fallback-pin", "agent-1", "TestBot")
	session.AddUserMessage("run the tool and summarize the result")

	result, err := loop.Run(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result) != "hello" {
		t.Fatalf("result = %q, want observed tool evidence (R-AGENT-200)", result)
	}
	if session.LastAssistantPhase() != "reflect" {
		t.Fatalf("phase = %q, want reflect", session.LastAssistantPhase())
	}
}
