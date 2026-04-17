package llm

import (
	"context"
	"testing"

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
