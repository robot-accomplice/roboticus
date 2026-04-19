package llm

import (
	"context"
	"testing"
)

func TestService_Complete_WithMockProvider(t *testing.T) {
	mock := &mockHTTP{
		statusCode: 200,
		body: `{
			"id": "test",
			"model": "gpt-4",
			"choices": [{"message": {"role": "assistant", "content": "mock response"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5}
		}`,
	}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "mock-provider", URL: "http://mock", Format: FormatOpenAI,
	}, mock)

	svc, _ := NewService(ServiceConfig{Primary: "mock-provider"}, nil)
	svc.providers["mock-provider"] = client

	resp, err := svc.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "mock response" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestService_Complete_CacheHit(t *testing.T) {
	mock := &mockHTTP{
		statusCode: 200,
		body: `{
			"id": "test",
			"model": "gpt-4",
			"choices": [{"message": {"role": "assistant", "content": "cached"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 2}
		}`,
	}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "cache-provider", URL: "http://mock", Format: FormatOpenAI,
	}, mock)

	svc, _ := NewService(ServiceConfig{Primary: "cache-provider"}, nil)
	svc.providers["cache-provider"] = client

	req := &Request{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "test cache"}}}

	// First call — should populate cache.
	resp1, _ := svc.Complete(context.Background(), req)
	if resp1 == nil {
		t.Fatal("first call should succeed")
	}

	// Second call — should hit cache.
	resp2, _ := svc.Complete(context.Background(), req)
	if resp2 == nil {
		t.Fatal("second call should succeed (cache)")
	}
	if resp2.Content != "cached" {
		t.Errorf("cached content = %q", resp2.Content)
	}
}

func TestService_Complete_Fallback(t *testing.T) {
	failMock := &mockHTTP{statusCode: 500, body: "error"}
	successMock := &mockHTTP{
		statusCode: 200,
		body:       `{"id":"fb","model":"fallback","choices":[{"message":{"content":"fallback response"},"finish_reason":"stop"}],"usage":{}}`,
	}

	failClient, _ := NewClientWithHTTP(&Provider{
		Name: "primary", URL: "http://fail", Format: FormatOpenAI,
	}, failMock)
	successClient, _ := NewClientWithHTTP(&Provider{
		Name: "fallback", URL: "http://ok", Format: FormatOpenAI,
	}, successMock)

	svc, _ := NewService(ServiceConfig{
		Primary:   "primary",
		Fallbacks: []string{"fallback"},
	}, nil)
	svc.providers["primary"] = failClient
	svc.providers["fallback"] = successClient

	resp, err := svc.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("should fallback: %v", err)
	}
	if resp.Content != "fallback response" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestService_Complete_NoEscalateSkipsFallbackChain(t *testing.T) {
	failMock := &mockHTTP{statusCode: 500, body: "error"}
	successMock := &mockHTTP{
		statusCode: 200,
		body:       `{"id":"fb","model":"fallback","choices":[{"message":{"content":"fallback response"},"finish_reason":"stop"}],"usage":{}}`,
	}

	failClient, _ := NewClientWithHTTP(&Provider{
		Name: "primary", URL: "http://fail", Format: FormatOpenAI,
	}, failMock)
	successClient, _ := NewClientWithHTTP(&Provider{
		Name: "fallback", URL: "http://ok", Format: FormatOpenAI,
	}, successMock)

	svc, _ := NewService(ServiceConfig{
		Primary:   "primary",
		Fallbacks: []string{"fallback"},
	}, nil)
	svc.providers["primary"] = failClient
	svc.providers["fallback"] = successClient

	_, err := svc.Complete(context.Background(), &Request{
		Model:      "gpt-4",
		Messages:   []Message{{Role: "user", Content: "test"}},
		NoEscalate: true,
	})
	if err == nil {
		t.Fatal("expected primary failure without fallback when NoEscalate is set")
	}
}

func TestService_Complete_AllProvidersFail(t *testing.T) {
	failMock := &mockHTTP{statusCode: 500, body: "error"}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "only", URL: "http://fail", Format: FormatOpenAI,
	}, failMock)

	svc, _ := NewService(ServiceConfig{Primary: "only"}, nil)
	svc.providers["only"] = client

	_, err := svc.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err == nil {
		t.Error("should fail when all providers fail")
	}
}
