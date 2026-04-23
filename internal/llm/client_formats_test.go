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

func TestMarshalOllama_UsesOllamaToolMessageContract(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOllama}}
	temp := 0.7
	req := &Request{
		Model: "llama3",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID: "call_1", Type: "function",
				Function: ToolCallFunc{Name: "search", Arguments: `{"query":"hello"}`},
			}}},
			{Role: "tool", ToolCallID: "call_1", Name: "search", Content: "done"},
		},
		Temperature: &temp,
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if raw["temperature"].(float64) != 0.7 {
		t.Errorf("temperature = %v, want 0.7", raw["temperature"])
	}
	if _, hasOptions := raw["options"]; hasOptions {
		t.Error("should not have 'options' field — using OpenAI format")
	}
	msgs := raw["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	toolCalls := assistant["tool_calls"].([]any)
	function := toolCalls[0].(map[string]any)["function"].(map[string]any)
	if _, ok := function["arguments"].(map[string]any); !ok {
		t.Fatalf("ollama tool call arguments = %T, want object", function["arguments"])
	}
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["tool_name"] != "search" {
		t.Fatalf("tool_name = %v, want search", toolMsg["tool_name"])
	}
	if _, exists := toolMsg["tool_call_id"]; exists {
		t.Fatalf("tool_call_id should not be present for ollama tool messages: %v", toolMsg["tool_call_id"])
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

func TestMarshalGoogle_SystemInstruction(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatGoogle}}
	req := &Request{
		Model: "gemini-pro",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 500,
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)

	// System messages should be extracted to systemInstruction.
	si, ok := raw["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatal("systemInstruction missing from payload")
	}
	parts := si["parts"].([]any)
	text := parts[0].(map[string]any)["text"].(string)
	if text != "You are helpful.\nBe concise." {
		t.Errorf("systemInstruction text = %q", text)
	}

	// Contents should not include system messages.
	contents := raw["contents"].([]any)
	if len(contents) != 1 {
		t.Errorf("contents count = %d, want 1 (system excluded)", len(contents))
	}
}

func TestMarshalGoogle_FunctionDeclarations(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatGoogle}}
	req := &Request{
		Model:    "gemini-pro",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools: []ToolDef{{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "get_weather",
				Description: "Get the weather",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		}},
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)

	tools, ok := raw["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("tools missing from payload")
	}
	toolObj := tools[0].(map[string]any)
	decls, ok := toolObj["functionDeclarations"].([]any)
	if !ok || len(decls) == 0 {
		t.Fatal("functionDeclarations missing")
	}
	decl := decls[0].(map[string]any)
	if decl["name"] != "get_weather" {
		t.Errorf("name = %v", decl["name"])
	}
	if decl["description"] != "Get the weather" {
		t.Errorf("description = %v", decl["description"])
	}
	if decl["parameters"] == nil {
		t.Error("parameters should be present")
	}
}

func TestUnmarshalGoogleResponse_ModelField(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatGoogle}}
	body := `{
		"modelVersion": "gemini-1.5-pro-001",
		"candidates": [{"content": {"parts": [{"text": "hi"}]}, "finishReason": "STOP"}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3}
	}`
	resp, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Model != "gemini-1.5-pro-001" {
		t.Errorf("Model = %q, want gemini-1.5-pro-001", resp.Model)
	}

	// Fallback to "model" field when modelVersion is empty.
	body2 := `{
		"model": "gemini-pro",
		"candidates": [{"content": {"parts": [{"text": "hi"}]}, "finishReason": "STOP"}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3}
	}`
	resp2, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body2)))
	if err != nil {
		t.Fatalf("unmarshal fallback: %v", err)
	}
	if resp2.Model != "gemini-pro" {
		t.Errorf("Model fallback = %q, want gemini-pro", resp2.Model)
	}
}

func TestUnmarshalGoogleResponse_FunctionCallParsing(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatGoogle}}
	body := `{
		"modelVersion": "gemini-pro",
		"candidates": [{"content": {"parts": [{"functionCall": {"name": "get_weather", "args": {"city": "SF"}}}]}, "finishReason": "STOP"}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3}
	}`
	resp, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool name = %q", resp.ToolCalls[0].Function.Name)
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

func TestUnmarshalOllamaResponse_UsesOpenAIFormat(t *testing.T) {
	// FormatOllama now routes to OpenAI-compatible response parsing (Rust parity).
	// Ollama's /v1/chat/completions returns OpenAI-format responses.
	c := &Client{provider: &Provider{Format: FormatOllama}}
	body := `{"id":"chatcmpl-1","model":"llama3","choices":[{"message":{"role":"assistant","content":"Ollama says hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`
	resp, err := c.unmarshalResponse(io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content != "Ollama says hi" {
		t.Errorf("content = %s", resp.Content)
	}
	if resp.Usage.OutputTokens != 3 {
		t.Errorf("output tokens = %d, want 3", resp.Usage.OutputTokens)
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

// Regression: assistant messages with tool_calls must serialize "content" as empty string,
// not omit the field (which Go's omitempty does for empty strings).
// Before fix: providers rejected the second ReAct turn because tool_call_id couldn't
// correlate to the previous assistant message (malformed serialization).

func TestMarshalOpenAI_AssistantToolCallsContentPresent(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOpenAI}}
	req := &Request{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "user", Content: "search for palm"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				{ID: "call_123", Type: "function", Function: ToolCallFunc{Name: "search_memories", Arguments: `{"query":"palm"}`}},
			}},
			{Role: "tool", Content: `{"results": []}`, ToolCallID: "call_123", Name: "search_memories"},
		},
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	msgs := raw["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("message count = %d, want 3", len(msgs))
	}

	// Assistant message must have "content" field present (not omitted).
	assistantMsg := msgs[1].(map[string]any)
	if _, hasContent := assistantMsg["content"]; !hasContent {
		t.Error("assistant message with tool_calls must include 'content' field — regression: was omitted by omitempty")
	}
	// Content should be empty string, not null.
	content := assistantMsg["content"]
	if content != "" {
		t.Errorf("assistant content should be empty string, got %v", content)
	}

	// Must have tool_calls.
	if _, hasTC := assistantMsg["tool_calls"]; !hasTC {
		t.Error("assistant message should have tool_calls")
	}

	// Tool result message must have tool_call_id.
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["tool_call_id"] != "call_123" {
		t.Errorf("tool_call_id = %v, want call_123", toolMsg["tool_call_id"])
	}
}

func TestMarshalOpenAI_ToolResultContentPresent(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOpenAI}}
	req := &Request{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "tool", Content: "result data", ToolCallID: "call_abc", Name: "recall_memory"},
		},
	}
	data, err := c.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	msgs := raw["messages"].([]any)
	toolMsg := msgs[0].(map[string]any)

	if toolMsg["role"] != "tool" {
		t.Errorf("role = %v", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_abc" {
		t.Errorf("tool_call_id = %v", toolMsg["tool_call_id"])
	}
	if toolMsg["content"] != "result data" {
		t.Errorf("content = %v", toolMsg["content"])
	}
	if toolMsg["name"] != "recall_memory" {
		t.Errorf("name = %v", toolMsg["name"])
	}
}

func TestUnmarshalResponse_StripsParsedToolCallJSONFromContent(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOpenAI}}
	body := strings.NewReader(`{
		"id":"resp_1",
		"model":"gpt-4",
		"choices":[{
			"message":{
				"role":"assistant",
				"content":"I'll count the files.\n{\"tool_call\": {\"name\": \"bash\", \"params\": {\"command\": \"find . -type f | wc -l\"}}}\nThen I'll answer with just the number."
			},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":10,"completion_tokens":5}
	}`)

	resp, err := c.unmarshalResponse(body)
	if err != nil {
		t.Fatalf("unmarshalResponse: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(resp.ToolCalls))
	}
	if strings.Contains(resp.Content, `"tool_call"`) {
		t.Fatalf("content still contains tool call JSON: %q", resp.Content)
	}
	if resp.Content != "I'll count the files.\nThen I'll answer with just the number." {
		t.Fatalf("unexpected content: %q", resp.Content)
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
