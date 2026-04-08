package llm

import (
	"testing"
)

func TestParseToolCallsFromText_SingleCall(t *testing.T) {
	content := `I'll check the file for you.
{"tool_call": {"name": "bash", "params": {"command": "ls -la"}}}
Let me know if you need more.`

	calls := ParseToolCallsFromText(content)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Errorf("name = %q, want bash", calls[0].Function.Name)
	}
}

func TestParseToolCallsFromText_MultipleCalls(t *testing.T) {
	content := `I need to check two things:
{"tool_call": {"name": "bash", "params": {"command": "cat file.txt"}}}
And also:
{"tool_call": {"name": "read_file", "params": {"path": "/tmp/test"}}}
Done.`

	calls := ParseToolCallsFromText(content)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Errorf("calls[0].name = %q, want bash", calls[0].Function.Name)
	}
	if calls[1].Function.Name != "read_file" {
		t.Errorf("calls[1].name = %q, want read_file", calls[1].Function.Name)
	}
}

func TestParseToolCallsFromText_TruncatedJSON(t *testing.T) {
	// Simulates a response that was cut off mid-JSON.
	content := `{"tool_call": {"name": "bash", "params": {"command": "echo hello"`

	calls := ParseToolCallsFromText(content)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (truncation recovery)", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Errorf("name = %q, want bash", calls[0].Function.Name)
	}
}

func TestParseToolCallsFromText_NoToolCalls(t *testing.T) {
	content := "Just a regular response with no tool calls."
	calls := ParseToolCallsFromText(content)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0", len(calls))
	}
}

func TestParseToolCallsFromText_FlexibleFieldNames(t *testing.T) {
	// Alternative field names: "arguments" instead of "params"
	content := `{"tool_call": {"tool_name": "write_file", "arguments": {"path": "/tmp/x", "content": "hi"}}}`

	calls := ParseToolCallsFromText(content)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "write_file" {
		t.Errorf("name = %q, want write_file", calls[0].Function.Name)
	}
}

func TestParseToolCallsFromText_ShorthandName(t *testing.T) {
	// Shorthand: {"tool_call": "bash", "params": {"command": "ls"}}
	content := `{"tool_call": "bash", "params": {"command": "ls"}}`

	calls := ParseToolCallsFromText(content)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Errorf("name = %q, want bash", calls[0].Function.Name)
	}
}

func TestUnmarshalAnthropicResponse_ToolUse(t *testing.T) {
	data := []byte(`{
		"id": "msg_01",
		"model": "claude-3",
		"content": [
			{"type": "text", "text": "Let me check that."},
			{"type": "tool_use", "id": "toolu_01", "name": "bash", "input": {"command": "ls"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)

	client := &Client{provider: &Provider{Format: FormatAnthropic}}
	resp, err := client.unmarshalAnthropicResponse(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content != "Let me check that." {
		t.Errorf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("tool name = %q, want bash", resp.ToolCalls[0].Function.Name)
	}
	if resp.ToolCalls[0].ID != "toolu_01" {
		t.Errorf("tool ID = %q, want toolu_01", resp.ToolCalls[0].ID)
	}
}

func TestUnmarshalGoogleResponse_FunctionCall(t *testing.T) {
	data := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "I'll look that up."},
					{"functionCall": {"name": "search", "args": {"query": "roboticus"}}}
				]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5}
	}`)

	client := &Client{provider: &Provider{Format: FormatGoogle}}
	resp, err := client.unmarshalGoogleResponse(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content != "I'll look that up." {
		t.Errorf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "search" {
		t.Errorf("tool name = %q, want search", resp.ToolCalls[0].Function.Name)
	}
}
