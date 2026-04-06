package agent

import (
	"context"
	"testing"

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
		IdleThreshold: 3,
		LoopWindow:    3,
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
