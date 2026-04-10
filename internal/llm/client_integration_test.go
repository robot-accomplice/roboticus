package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// mockHTTP returns canned HTTP responses.
type mockHTTP struct {
	statusCode int
	body       string
	err        error
}

func (m *mockHTTP) Do(_ *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     make(http.Header),
	}, nil
}

func TestClient_Complete_OpenAI(t *testing.T) {
	mock := &mockHTTP{
		statusCode: 200,
		body: `{
			"id": "chatcmpl-test",
			"model": "gpt-4",
			"choices": [{"message": {"role": "assistant", "content": "Hello from mock!"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5}
		}`,
	}
	client, _ := NewClientWithHTTP(&Provider{
		Name:   "test-openai",
		URL:    "http://mock.test",
		Format: FormatOpenAI,
	}, mock)

	resp, err := client.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "Hello from mock!" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("input tokens = %d", resp.Usage.InputTokens)
	}
}

func TestClient_Complete_Anthropic(t *testing.T) {
	mock := &mockHTTP{
		statusCode: 200,
		body: `{
			"id": "msg-test",
			"model": "claude-3",
			"content": [{"type": "text", "text": "Anthropic response"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 8, "output_tokens": 3}
		}`,
	}
	client, _ := NewClientWithHTTP(&Provider{
		Name:   "test-anthropic",
		URL:    "http://mock.test",
		Format: FormatAnthropic,
	}, mock)

	resp, err := client.Complete(context.Background(), &Request{
		Model:    "claude-3",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "Anthropic response" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestClient_Complete_Ollama(t *testing.T) {
	// FormatOllama now uses OpenAI-compatible /v1/chat/completions format (Rust parity).
	mock := &mockHTTP{
		statusCode: 200,
		body:       `{"id":"1","model":"llama3","choices":[{"message":{"role":"assistant","content":"Ollama response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
	}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "test-ollama", URL: "http://mock.test", Format: FormatOllama, IsLocal: true,
	}, mock)

	resp, err := client.Complete(context.Background(), &Request{
		Model: "llama3", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "Ollama response" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestClient_Complete_Google(t *testing.T) {
	mock := &mockHTTP{
		statusCode: 200,
		body: `{
			"candidates": [{"content": {"parts": [{"text": "Google response"}]}, "finishReason": "STOP"}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3}
		}`,
	}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "test-google", URL: "http://mock.test", Format: FormatGoogle,
	}, mock)

	resp, err := client.Complete(context.Background(), &Request{
		Model: "gemini-pro", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "Google response" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestClient_Complete_Error429(t *testing.T) {
	mock := &mockHTTP{statusCode: 429, body: "rate limited"}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, mock)

	_, err := client.Complete(context.Background(), &Request{
		Model: "gpt-4", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("should return error for 429")
	}
}

func TestClient_Complete_Error401(t *testing.T) {
	mock := &mockHTTP{statusCode: 401, body: "unauthorized"}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, mock)

	_, err := client.Complete(context.Background(), &Request{
		Model: "gpt-4", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("should return error for 401")
	}
}

func TestClient_Complete_NetworkError(t *testing.T) {
	mock := &mockHTTP{err: context.DeadlineExceeded}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, mock)

	_, err := client.Complete(context.Background(), &Request{
		Model: "gpt-4", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("should return error for network failure")
	}
}

func TestClient_ChatURL(t *testing.T) {
	tests := []struct {
		format APIFormat
		want   string
	}{
		{FormatOpenAI, "/v1/chat/completions"},
		{FormatAnthropic, "/v1/messages"},
		{FormatOllama, "/v1/chat/completions"}, // Now routes to OpenAI-compatible (Rust parity)
		{FormatGoogle, "/v1/models/"},
	}
	for _, tt := range tests {
		client := &Client{provider: &Provider{URL: "http://test.com", Format: tt.format}}
		url := client.chatURL()
		if !strings.HasSuffix(url, tt.want) {
			t.Errorf("format %v: url = %s, want suffix %s", tt.format, url, tt.want)
		}
	}
}

func TestClient_SetHeaders_OpenAI(t *testing.T) {
	client := &Client{
		provider: &Provider{Format: FormatOpenAI},
		apiKey:   "sk-test",
	}
	req, _ := http.NewRequest("POST", "http://test.com", nil)
	client.setHeaders(req)
	auth := req.Header.Get("Authorization")
	if auth != "Bearer sk-test" {
		t.Errorf("auth = %s", auth)
	}
}

func TestClient_SetHeaders_Anthropic(t *testing.T) {
	client := &Client{
		provider: &Provider{Format: FormatAnthropic},
		apiKey:   "sk-ant-test",
	}
	req, _ := http.NewRequest("POST", "http://test.com", nil)
	client.setHeaders(req)
	key := req.Header.Get("x-api-key")
	if key != "sk-ant-test" {
		t.Errorf("x-api-key = %s", key)
	}
	ver := req.Header.Get("anthropic-version")
	if ver != "2023-06-01" {
		t.Errorf("anthropic-version = %s", ver)
	}
}
