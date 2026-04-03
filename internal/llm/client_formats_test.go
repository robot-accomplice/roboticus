package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMarshalOpenAI(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOpenAI}}
	req := &Request{
		Model:     "gpt-4",
		Messages:  []Message{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
		Stream:    false,
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["model"] != "gpt-4" {
		t.Errorf("model = %v", raw["model"])
	}
	if raw["max_tokens"].(float64) != 100 {
		t.Errorf("max_tokens = %v", raw["max_tokens"])
	}
}

func TestMarshalAnthropic(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatAnthropic}}
	req := &Request{
		Model: "claude-3",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 200,
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["system"] != "You are helpful." {
		t.Errorf("system = %v", raw["system"])
	}
	msgs := raw["messages"].([]any)
	if len(msgs) != 1 {
		t.Errorf("messages count = %d (system should be extracted)", len(msgs))
	}
}

func TestMarshalOllama(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOllama}}
	temp := 0.7
	req := &Request{
		Model:       "llama3",
		Messages:    []Message{{Role: "user", Content: "hello"}},
		Temperature: &temp,
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	opts := raw["options"].(map[string]any)
	if opts["temperature"].(float64) != 0.7 {
		t.Errorf("temperature = %v", opts["temperature"])
	}
}

func TestMarshalGoogle(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatGoogle}}
	req := &Request{
		Model:     "gemini-pro",
		Messages:  []Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}},
		MaxTokens: 500,
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	contents := raw["contents"].([]any)
	if len(contents) != 2 {
		t.Errorf("contents count = %d, want 2", len(contents))
	}
	second := contents[1].(map[string]any)
	if second["role"] != "model" {
		t.Errorf("assistant role should be 'model', got %v", second["role"])
	}
}

func TestUnmarshalOpenAIResponse(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOpenAI}}
	body := `{
		"id": "chatcmpl-123",
		"model": "gpt-4",
		"choices": [{"message": {"role": "assistant", "content": "Hello!"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5}
	}`
	resp, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("content = %s", resp.Content)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("input tokens = %d", resp.Usage.InputTokens)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finish_reason = %s", resp.FinishReason)
	}
}

func TestUnmarshalAnthropicResponse(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatAnthropic}}
	body := `{
		"id": "msg-123",
		"model": "claude-3",
		"content": [{"type": "text", "text": "Hi there!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 8, "output_tokens": 3}
	}`
	resp, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content != "Hi there!" {
		t.Errorf("content = %s", resp.Content)
	}
	if resp.Usage.OutputTokens != 3 {
		t.Errorf("output tokens = %d", resp.Usage.OutputTokens)
	}
}

func TestUnmarshalOllamaResponse(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOllama}}
	body := `{"model": "llama3", "message": {"role": "assistant", "content": "Ollama says hi"}}`
	resp, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content != "Ollama says hi" {
		t.Errorf("content = %s", resp.Content)
	}
}

func TestUnmarshalGoogleResponse(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatGoogle}}
	body := `{
		"candidates": [{"content": {"parts": [{"text": "Google says hi"}]}, "finishReason": "STOP"}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3}
	}`
	resp, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content != "Google says hi" {
		t.Errorf("content = %s", resp.Content)
	}
	if resp.Usage.InputTokens != 5 {
		t.Errorf("input tokens = %d", resp.Usage.InputTokens)
	}
}

func TestUnmarshalOpenAIChunk(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOpenAI}}
	data := `{"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`
	chunk, err := c.unmarshalStreamChunk([]byte(data))
	if err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	if chunk.Delta != "Hello" {
		t.Errorf("delta = %s", chunk.Delta)
	}
}

func TestUnmarshalAnthropicChunk(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatAnthropic}}
	data := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"World"}}`
	chunk, err := c.unmarshalStreamChunk([]byte(data))
	if err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	if chunk.Delta != "World" {
		t.Errorf("delta = %s", chunk.Delta)
	}
}

func TestParseErrorResponse_429(t *testing.T) {
	c := &Client{provider: &Provider{Name: "test"}}
	resp := &http.Response{
		StatusCode: 429,
		Body:       io.NopCloser(strings.NewReader("rate limited")),
	}
	err := c.parseErrorResponse(resp)
	if err == nil {
		t.Fatal("should return error")
	}
	if !strings.Contains(err.Error(), "rate") {
		t.Errorf("error should mention rate, got: %v", err)
	}
}

func TestParseErrorResponse_401(t *testing.T) {
	c := &Client{provider: &Provider{Name: "test"}}
	resp := &http.Response{
		StatusCode: 401,
		Body:       io.NopCloser(strings.NewReader("unauthorized")),
	}
	err := c.parseErrorResponse(resp)
	if err == nil {
		t.Fatal("should return error")
	}
}
