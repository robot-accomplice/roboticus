package llm

import (
	"encoding/json"
	"testing"
)

func TestToolMessageNormalizationFactory_OpenAICompatible_NoTransform(t *testing.T) {
	result := NewToolMessageNormalizationFactory().NormalizeProviderMessages(ProviderMessageNormalizationInput{
		Format: FormatOpenAI,
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: ToolCallFunc{
					Name:      "search",
					Arguments: `{"query":"vault"}`,
				},
			}}},
			{Role: "tool", ToolCallID: "call_1", Name: "search", Content: "ok"},
		},
	})
	if result.Disposition != ToolMessageNoTransformNeeded {
		t.Fatalf("disposition = %q, want %q", result.Disposition, ToolMessageNoTransformNeeded)
	}
	if result.Fidelity != ToolMessageExact {
		t.Fatalf("fidelity = %q, want exact", result.Fidelity)
	}
}

func TestToolMessageNormalizationFactory_Ollama_RewritesToolMessages(t *testing.T) {
	result := NewToolMessageNormalizationFactory().NormalizeProviderMessages(ProviderMessageNormalizationInput{
		Format: FormatOllama,
		Messages: []Message{
			{Role: "user", Content: "Write the note."},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: ToolCallFunc{
					Name:      "obsidian_write",
					Arguments: `{"path":"note.md","content":"# Note"}`,
				},
			}}},
			{Role: "tool", ToolCallID: "call_1", Name: "obsidian_write", Content: "wrote 7 bytes"},
		},
	})
	if result.Disposition != ToolMessageQualifiedTransform {
		t.Fatalf("disposition = %q, want %q", result.Disposition, ToolMessageQualifiedTransform)
	}
	if result.Fidelity != ToolMessageRepaired {
		t.Fatalf("fidelity = %q, want repaired", result.Fidelity)
	}
	if result.Transformer != "ollama_tool_messages" {
		t.Fatalf("transformer = %q", result.Transformer)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(result.Messages))
	}
	assistant := result.Messages[1]
	toolCalls, ok := assistant["tool_calls"].([]map[string]any)
	if !ok {
		raw, ok := assistant["tool_calls"].([]any)
		if !ok || len(raw) != 1 {
			t.Fatalf("assistant tool_calls = %T %v", assistant["tool_calls"], assistant["tool_calls"])
		}
		var cast bool
		toolCalls = make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			var m map[string]any
			m, cast = item.(map[string]any)
			if !cast {
				t.Fatalf("tool call item = %T", item)
			}
			toolCalls = append(toolCalls, m)
		}
	}
	function := toolCalls[0]["function"].(map[string]any)
	if _, ok := function["arguments"].(map[string]any); !ok {
		t.Fatalf("ollama arguments = %T, want object", function["arguments"])
	}
	tool := result.Messages[2]
	if tool["tool_name"] != "obsidian_write" {
		t.Fatalf("tool_name = %v, want obsidian_write", tool["tool_name"])
	}
	if _, exists := tool["tool_call_id"]; exists {
		t.Fatalf("tool_call_id should not be sent to ollama tool result, got %v", tool["tool_call_id"])
	}
}

func TestToolMessageNormalizationFactory_Ollama_InvalidArgumentsFail(t *testing.T) {
	result := NewToolMessageNormalizationFactory().NormalizeProviderMessages(ProviderMessageNormalizationInput{
		Format: FormatOllama,
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{
				Function: ToolCallFunc{
					Name:      "obsidian_write",
					Arguments: `{"path":"note.md"`,
				},
			}}},
		},
	})
	if result.Disposition != ToolMessageTransformFailed {
		t.Fatalf("disposition = %q, want transform_failed", result.Disposition)
	}
}

func TestMarshalOpenAICompatibleWithMessages_RoundTripJSON(t *testing.T) {
	c := &Client{provider: &Provider{Format: FormatOllama}}
	msgs := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "tool", "tool_name": "search", "content": "ok"},
	}
	data, err := c.marshalOpenAICompatibleWithMessages(&Request{Model: "gemma4"}, msgs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["model"] != "gemma4" {
		t.Fatalf("model = %v", raw["model"])
	}
}
