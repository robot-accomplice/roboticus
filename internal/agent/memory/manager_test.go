package memory

import (
	"strings"
	"testing"

	"roboticus/internal/llm"
)

func TestClassifyTurn_ToolUse(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "Read the file"},
		{Role: "assistant", Content: "I'll read the file."},
		{Role: "tool", Name: "read_file", Content: "file contents here"},
	}
	if got := classifyTurn(msgs); got != TurnToolUse {
		t.Errorf("got %v, want TurnToolUse", got)
	}
}

func TestClassifyTurn_Financial(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "Please transfer my balance from the wallet to send funds"},
	}
	if got := classifyTurn(msgs); got != TurnFinancial {
		t.Errorf("got %v, want TurnFinancial", got)
	}
}

func TestClassifyTurn_Creative(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "Create a poem about autumn"},
	}
	if got := classifyTurn(msgs); got != TurnCreative {
		t.Errorf("got %v, want TurnCreative", got)
	}
}

func TestClassifyTurn_Reasoning(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "What is the meaning of life?"},
	}
	if got := classifyTurn(msgs); got != TurnReasoning {
		t.Errorf("got %v, want TurnReasoning", got)
	}
}

func TestClassifyTurn_EmptyMessages(t *testing.T) {
	if got := classifyTurn(nil); got != TurnReasoning {
		t.Errorf("empty: got %v, want TurnReasoning", got)
	}
}

func TestExtractEntities(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"Hello @alice and @bob", 2},
		{"@alice @alice @alice", 1}, // dedup
		{"No mentions here", 0},
		{"@user. @other!", 2}, // punctuation stripped
		{"@", 0},              // bare @ ignored
		{"", 0},
	}

	for _, tt := range tests {
		entities := extractEntities(tt.input)
		if len(entities) != tt.count {
			t.Errorf("extractEntities(%q) = %v (len %d), want %d", tt.input, entities, len(entities), tt.count)
		}
	}
}

func TestIsToolFailure(t *testing.T) {
	failures := []string{
		"error: file not found",
		"Error: permission denied",
		"failed: connection refused",
		"fatal: segfault",
		`{"error": "bad request"}`,
		`{"err": "timeout"}`,
	}
	for _, f := range failures {
		if !isToolFailure(f) {
			t.Errorf("isToolFailure(%q) = false, want true", f)
		}
	}

	successes := []string{
		"file contents here",
		"operation completed successfully",
		"[]",
		"{}",
	}
	for _, s := range successes {
		if isToolFailure(s) {
			t.Errorf("isToolFailure(%q) = true, want false", s)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short: got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncated: got %q", got)
	}
	if got := truncate("exact", 5); got != "exact" {
		t.Errorf("exact: got %q", got)
	}
}

func TestSummarizeToolOutput_JSONArray(t *testing.T) {
	got := summarizeToolOutput("web_search", `[{"title":"a"},{"title":"b"},{"title":"c"}]`)
	if got != "web_search: 3 items returned" {
		t.Errorf("array: got %q", got)
	}
}

func TestSummarizeToolOutput_EmptyArray(t *testing.T) {
	got := summarizeToolOutput("query_table", `[]`)
	if got != "query_table: 0 items returned" {
		t.Errorf("empty array: got %q", got)
	}
}

func TestSummarizeToolOutput_JSONError(t *testing.T) {
	got := summarizeToolOutput("query_table", `{"error":"table not found"}`)
	want := "query_table: error — table not found"
	if got != want {
		t.Errorf("error: got %q, want %q", got, want)
	}
}

func TestSummarizeToolOutput_JSONStatus(t *testing.T) {
	got := summarizeToolOutput("deploy", `{"status":"complete","id":"abc123"}`)
	if got != "deploy: status=complete" {
		t.Errorf("status: got %q", got)
	}
}

func TestSummarizeToolOutput_JSONKeys(t *testing.T) {
	got := summarizeToolOutput("api_call", `{"count":5,"name":"test","value":42}`)
	if got != "api_call: {count, name, value}" {
		t.Errorf("keys: got %q", got)
	}
}

func TestSummarizeToolOutput_PlainText(t *testing.T) {
	got := summarizeToolOutput("read_file", "hello world this is plain text")
	if got != "read_file: hello world this is plain text" {
		t.Errorf("plain: got %q", got)
	}
}

func TestSummarizeToolOutput_InvalidJSON(t *testing.T) {
	// Truncated JSON should NOT be stored as-is; fallback to plain truncation.
	content := `{"data": [1,2,3,`
	got := summarizeToolOutput("some_tool", content)
	want := "some_tool: " + content
	if got != want {
		t.Errorf("invalid json: got %q, want %q", got, want)
	}
}

func TestSummarizeToolOutput_LongContent(t *testing.T) {
	// Ensure output is capped at 150 chars.
	longErr := strings.Repeat("x", 200)
	got := summarizeToolOutput("tool", `{"error":"`+longErr+`"}`)
	if len(got) > 150 {
		t.Errorf("too long: len=%d, got %q", len(got), got)
	}
	if !strings.HasPrefix(got, "tool: error — ") {
		t.Errorf("wrong prefix: got %q", got)
	}
}

func TestExtractFirstSentence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello world. More text.", "Hello world"},
		{"Question? Answer.", "Question"},
		{"Short", "Short"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractFirstSentence(tt.input)
		if got != tt.want {
			t.Errorf("extractFirstSentence(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
