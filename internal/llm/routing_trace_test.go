package llm

import (
	"context"
	"testing"
)

type traceCapture struct {
	values map[string]any
}

func (t *traceCapture) Annotate(key string, value any) {
	if t.values == nil {
		t.values = map[string]any{}
	}
	t.values[key] = value
}

func TestServiceComplete_AnnotatesRoutingFromActualRequest(t *testing.T) {
	client, _ := NewClientWithHTTP(&Provider{
		Name: "cloud-model", URL: "http://cloud", Format: FormatOpenAI,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"cloud","model":"cloud-model","choices":[{"message":{"content":"cloud response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":30}}`,
	})

	svc, err := NewService(ServiceConfig{
		Primary: "cloud-model/cloud-model",
		Providers: []Provider{
			{Name: "cloud-model", URL: "http://cloud", Format: FormatOpenAI},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.providers["cloud-model"] = client

	tr := &traceCapture{}
	ctx := WithRoutingTracer(context.Background(), tr)
	req := &Request{
		Messages: []Message{
			{Role: "system", Content: "system context"},
			{Role: "assistant", Content: "prior assistant turn"},
			{Role: "user", Content: "please analyze this request"},
		},
		Tools: []ToolDef{
			{Type: "function", Function: ToolFuncDef{Name: "echo", Description: "Echo"}},
		},
	}

	if _, err := svc.Complete(ctx, req); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if got := tr.values["inference.routing.trace_source"]; got != "actual_request" {
		t.Fatalf("routing.trace_source = %#v want actual_request", got)
	}
	if got := tr.values["inference.routing.request_message_count"]; got != 3 {
		t.Fatalf("routing.request_message_count = %#v want 3", got)
	}
	if got := tr.values["inference.routing.request_tool_count"]; got != 1 {
		t.Fatalf("routing.request_tool_count = %#v want 1", got)
	}
	if got := tr.values["inference.routing.winner"]; got != "cloud-model/cloud-model" {
		t.Fatalf("routing.winner = %#v want cloud-model/cloud-model", got)
	}
}

func TestAnnotateRoutingDecision_NoTracerNoop(t *testing.T) {
	router := NewRouter([]RouteTarget{{Model: "m1", Provider: "p1"}}, RouterConfig{})
	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	target := router.Select(req)

	annotateRoutingDecision(context.Background(), router, req, target)
}
