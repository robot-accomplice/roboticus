package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/core"
	"roboticus/internal/llm"
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
	if result == "" {
		t.Fatal("expected synthesized termination content")
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
