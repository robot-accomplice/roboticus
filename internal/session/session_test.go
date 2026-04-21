package session

import (
	"testing"

	"roboticus/internal/llm"
)

func TestAddAssistantMessage_SeparatesHistoryFromPendingToolCalls(t *testing.T) {
	s := New("sess-1", "agent-1", "Duncan")
	toolCalls := []llm.ToolCall{
		{
			ID:   "call_obsidian",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "obsidian_write",
				Arguments: `{"path":"note-a.md","content":"# A"}`,
			},
		},
		{
			ID:   "call_search",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "search_memories",
				Arguments: `{"query":"checkpoint note","limit":5}`,
			},
		},
	}

	s.AddAssistantMessage("", toolCalls)
	s.AddToolResult("call_obsidian", "obsidian_write", "wrote note-a.md", false)
	s.AddToolResult("call_search", "search_memories", "no results", false)

	msgs := s.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected assistant message history")
	}
	if len(msgs[0].ToolCalls) != 2 {
		t.Fatalf("assistant history tool call count = %d, want 2", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].ID != "call_obsidian" {
		t.Fatalf("assistant history tool call[0] id = %q, want call_obsidian", msgs[0].ToolCalls[0].ID)
	}
	if msgs[0].ToolCalls[1].ID != "call_search" {
		t.Fatalf("assistant history tool call[1] id = %q, want call_search", msgs[0].ToolCalls[1].ID)
	}
	if msgs[0].ToolCalls[0].Function.Name != "obsidian_write" {
		t.Fatalf("assistant history tool call[0] name = %q, want obsidian_write", msgs[0].ToolCalls[0].Function.Name)
	}
	if msgs[0].ToolCalls[1].Function.Name != "search_memories" {
		t.Fatalf("assistant history tool call[1] name = %q, want search_memories", msgs[0].ToolCalls[1].Function.Name)
	}
	if len(s.PendingToolCalls()) != 0 {
		t.Fatalf("pending tool calls = %d, want 0", len(s.PendingToolCalls()))
	}
}

func TestPendingToolCalls_ReturnsDefensiveCopy(t *testing.T) {
	s := New("sess-2", "agent-1", "Duncan")
	s.AddAssistantMessage("", []llm.ToolCall{{
		ID:   "call_ctx",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "get_runtime_context",
			Arguments: `{}`,
		},
	}})

	pending := s.PendingToolCalls()
	pending[0].Function.Name = "mutated"

	again := s.PendingToolCalls()
	if again[0].Function.Name != "get_runtime_context" {
		t.Fatalf("pending tool call name = %q, want get_runtime_context", again[0].Function.Name)
	}
}
