package llm

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/core"
)

func TestServiceComplete_RecordsModelSelectionFromActualRequest(t *testing.T) {
	store := tempStore(t)
	client, _ := NewClientWithHTTP(&Provider{
		Name: "route-p", URL: "http://route", Format: FormatOpenAI,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"route","model":"route-model","choices":[{"message":{"content":"routed response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
	})

	svc, err := NewService(ServiceConfig{
		Primary: "route-p/route-model",
		Providers: []Provider{
			{Name: "route-p", URL: "http://route", Format: FormatOpenAI},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { svc.Drain(2 * time.Second) })
	svc.providers["route-p"] = client

	ctx := context.Background()
	ctx = core.WithSessionID(ctx, "sess-1")
	ctx = core.WithTurnID(ctx, "turn-1")
	ctx = core.WithChannelLabel(ctx, "chat")

	req := &Request{
		Messages: []Message{
			{Role: "system", Content: "system prompt"},
			{Role: "assistant", Content: "prior turn"},
			{Role: "user", Content: "please analyze the full request shape"},
		},
		Tools: []ToolDef{
			{Type: "function", Function: ToolFuncDef{Name: "echo", Description: "Echo"}},
		},
	}
	if _, err := svc.Complete(ctx, req); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var (
		selectedModel string
		channel       string
		userExcerpt   string
	)
	err = store.QueryRowContext(ctx,
		`SELECT selected_model, channel, user_excerpt
		   FROM model_selection_events
		  WHERE turn_id = 'turn-1'`).Scan(&selectedModel, &channel, &userExcerpt)
	if err != nil {
		t.Fatalf("query model_selection_events: %v", err)
	}
	if selectedModel != "route-p/route-model" {
		t.Fatalf("selected_model = %q want route-p/route-model", selectedModel)
	}
	if channel != "chat" {
		t.Fatalf("channel = %q want chat", channel)
	}
	if userExcerpt != "please analyze the full request shape" {
		t.Fatalf("user_excerpt = %q", userExcerpt)
	}
}

func TestRecordModelSelection_IsIdempotentPerTurn(t *testing.T) {
	store := tempStore(t)

	svc, err := NewService(ServiceConfig{}, store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { svc.Drain(2 * time.Second) })

	ctx := context.Background()
	ctx = core.WithSessionID(ctx, "sess-1")
	ctx = core.WithTurnID(ctx, "turn-1")
	ctx = core.WithChannelLabel(ctx, "chat")

	svc.RecordModelSelection(ctx, "turn-1", "sess-1", "", "chat", "ollama/qwen2.5:32b", "routed", "first excerpt", []string{"ollama/qwen2.5:32b"}, "", "")
	svc.RecordModelSelection(ctx, "turn-1", "sess-1", "", "chat", "moonshot/kimi-k2-turbo-preview", "fallback", "second excerpt", []string{"moonshot/kimi-k2-turbo-preview"}, "", "")

	var (
		count         int
		selectedModel string
		strategy      string
		userExcerpt   string
	)
	if err := store.QueryRowContext(ctx,
		`SELECT COUNT(*), selected_model, strategy, user_excerpt
		   FROM model_selection_events
		  WHERE turn_id = 'turn-1'`,
	).Scan(&count, &selectedModel, &strategy, &userExcerpt); err != nil {
		t.Fatalf("query model_selection_events: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d want 1", count)
	}
	if selectedModel != "moonshot/kimi-k2-turbo-preview" {
		t.Fatalf("selected_model = %q", selectedModel)
	}
	if strategy != "fallback" {
		t.Fatalf("strategy = %q", strategy)
	}
	if userExcerpt != "second excerpt" {
		t.Fatalf("user_excerpt = %q", userExcerpt)
	}
}
