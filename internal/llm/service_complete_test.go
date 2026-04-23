package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingHTTP struct {
	statusCode int
	body       string
	requests   []*http.Request
}

func (m *recordingHTTP) Do(r *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, r.Clone(r.Context()))
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     make(http.Header),
	}, nil
}

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

func TestService_Complete_RoutedOpenRouterNamespacePreservesOuterProvider(t *testing.T) {
	openRouterHTTP := &recordingHTTP{
		statusCode: 200,
		body:       `{"id":"or","model":"openai/gpt-4o-mini","choices":[{"message":{"content":"openrouter response"},"finish_reason":"stop"}],"usage":{}}`,
	}
	openAIHTTP := &recordingHTTP{
		statusCode: 401,
		body:       `{"error":{"message":"missing key"}}`,
	}

	openRouterClient, _ := NewClientWithHTTP(&Provider{
		Name: "openrouter", URL: "http://openrouter", Format: FormatOpenAI,
	}, openRouterHTTP)
	openAIClient, _ := NewClientWithHTTP(&Provider{
		Name: "openai", URL: "http://openai", Format: FormatOpenAI,
	}, openAIHTTP)

	svc, _ := NewService(ServiceConfig{
		Primary: "openrouter/openai/gpt-4o-mini",
		Providers: []Provider{
			{Name: "openrouter", URL: "http://openrouter", Format: FormatOpenAI},
			{Name: "openai", URL: "http://openai", Format: FormatOpenAI},
		},
	}, nil)
	svc.providers["openrouter"] = openRouterClient
	svc.providers["openai"] = openAIClient

	resp, err := svc.Complete(context.Background(), &Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "openrouter response" {
		t.Fatalf("resp.Content = %q, want openrouter response", resp.Content)
	}
	if len(openRouterHTTP.requests) != 1 {
		t.Fatalf("openrouter requests = %d, want 1", len(openRouterHTTP.requests))
	}
	if len(openAIHTTP.requests) != 0 {
		t.Fatalf("openai requests = %d, want 0", len(openAIHTTP.requests))
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
