package llm

import "testing"

func TestDropEmptyMessages_DropsBlankPlainMessages(t *testing.T) {
	in := []Message{
		{Role: "system", Content: "system prompt"},
		{Role: "assistant", Content: "   "},
		{Role: "user", Content: ""},
		{Role: "user", Content: "real prompt"},
	}

	got := dropEmptyMessages(in, "test")
	if len(got) != 2 {
		t.Fatalf("len(dropEmptyMessages) = %d, want 2", len(got))
	}
	if got[0].Content != "system prompt" || got[1].Content != "real prompt" {
		t.Fatalf("unexpected surviving messages: %+v", got)
	}
}

func TestDropEmptyMessages_PreservesStructuredPayloads(t *testing.T) {
	in := []Message{
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: ToolCallFunc{
					Name:      "recall_memory",
					Arguments: "{}",
				},
			}},
		},
		{
			Role:       "tool",
			Content:    "",
			ToolCallID: "call-1",
		},
		{
			Role:         "user",
			Content:      "",
			ContentParts: []ContentPart{{Type: "text", Text: "multimodal text"}},
		},
	}

	got := dropEmptyMessages(in, "test")
	if len(got) != len(in) {
		t.Fatalf("structured messages should survive unchanged; got %d want %d", len(got), len(in))
	}
}
