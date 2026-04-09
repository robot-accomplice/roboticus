package llm

import (
	"encoding/json"
	"testing"
)

func TestToolCallFunc_UnmarshalJSON_StringArguments(t *testing.T) {
	// OpenAI format: arguments is a JSON string.
	data := `{"name": "get_time", "arguments": "{\"timezone\": \"UTC\"}"}`
	var f ToolCallFunc
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Name != "get_time" {
		t.Errorf("name = %q, want get_time", f.Name)
	}
	if f.Arguments != `{"timezone": "UTC"}` {
		t.Errorf("arguments = %q, want JSON string", f.Arguments)
	}
}

func TestToolCallFunc_UnmarshalJSON_ObjectArguments(t *testing.T) {
	// Ollama native format: arguments is a JSON object.
	data := `{"name": "get_time", "arguments": {"timezone": "UTC"}}`
	var f ToolCallFunc
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Name != "get_time" {
		t.Errorf("name = %q, want get_time", f.Name)
	}
	// RawMessage preserves original whitespace from the JSON source.
	var parsed map[string]string
	if err := json.Unmarshal([]byte(f.Arguments), &parsed); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if parsed["timezone"] != "UTC" {
		t.Errorf("arguments timezone = %q, want UTC", parsed["timezone"])
	}
}

func TestToolCallFunc_UnmarshalJSON_EmptyObjectArguments(t *testing.T) {
	// Ollama native format: empty arguments object.
	data := `{"name": "get_time", "arguments": {}}`
	var f ToolCallFunc
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Name != "get_time" {
		t.Errorf("name = %q, want get_time", f.Name)
	}
	if f.Arguments != "{}" {
		t.Errorf("arguments = %q, want {}", f.Arguments)
	}
}

func TestToolCall_UnmarshalJSON_OllamaFullResponse(t *testing.T) {
	// Full Ollama tool call response fragment.
	data := `{
		"id": "call_abc123",
		"type": "function",
		"function": {"name": "recall_memory", "arguments": {"query": "career"}}
	}`
	var tc ToolCall
	if err := json.Unmarshal([]byte(data), &tc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tc.Function.Name != "recall_memory" {
		t.Errorf("name = %q", tc.Function.Name)
	}
	var parsed2 map[string]string
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed2); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if parsed2["query"] != "career" {
		t.Errorf("arguments query = %q, want career", parsed2["query"])
	}
}
