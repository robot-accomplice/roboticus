package session

import (
	"encoding/json"
	"strings"
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

func TestAddToolResultWithMetadata_PreservesTypedToolMetadata(t *testing.T) {
	s := New("sess-3", "agent-1", "Duncan")
	meta := json.RawMessage(`{"proof_type":"artifact_write","artifact_kind":"workspace_file","path":"tmp/out.txt"}`)

	s.AddToolResultWithMetadata("call-1", "write_file", `{"proof_type":"artifact_write"}`, meta, false)

	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	if string(msgs[0].Metadata) != string(meta) {
		t.Fatalf("metadata = %s, want %s", msgs[0].Metadata, meta)
	}
}

func TestAddAssistantMessageWithPhase_TracksLatestAssistantProvenance(t *testing.T) {
	s := New("sess-3b", "agent-1", "Duncan")

	s.AddAssistantMessageWithPhase("draft answer", nil, "think")
	if got := s.LastAssistantPhase(); got != "think" {
		t.Fatalf("LastAssistantPhase after think = %q, want think", got)
	}

	s.AddAssistantMessageWithPhase("refined answer", nil, "reflect")
	if got := s.LastAssistantPhase(); got != "reflect" {
		t.Fatalf("LastAssistantPhase after reflect = %q, want reflect", got)
	}
}

func TestBuildTOTOF_UsesObservationWindowAndOpenIssues(t *testing.T) {
	s := New("sess-4", "agent-1", "Duncan")
	s.AddUserMessage("What is 2 + 2?")
	s.AddAssistantMessage("", []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "calculator",
			Arguments: `{"expression":"2+2"}`,
		},
	}})
	s.AddToolResult("call-1", "calculator", "4", false)
	s.AddSystemMessage("Your previous response failed verification: unsupported certainty on a derivable answer.")

	totof := s.BuildTOTOF("Finalize directly unless more execution is strictly required.")
	if got := totof.UserTask; got != "What is 2 + 2?" {
		t.Fatalf("UserTask = %q, want original user task", got)
	}
	if len(totof.AuthoritativeObservedResult) != 1 || totof.AuthoritativeObservedResult[0] != "4" {
		t.Fatalf("AuthoritativeObservedResult = %#v, want [\"4\"]", totof.AuthoritativeObservedResult)
	}
	if len(totof.ToolOutcomes) != 1 {
		t.Fatalf("ToolOutcomes = %d, want 1", len(totof.ToolOutcomes))
	}
	if totof.ToolOutcomes[0].ToolName != "calculator" || totof.ToolOutcomes[0].Status != "ok" {
		t.Fatalf("ToolOutcome = %#v", totof.ToolOutcomes[0])
	}
	if len(totof.OpenIssues) != 1 || !strings.Contains(totof.OpenIssues[0], "failed verification") {
		t.Fatalf("OpenIssues = %#v, want verifier issue", totof.OpenIssues)
	}
	msgs := totof.Messages()
	if len(msgs) != 2 {
		t.Fatalf("TOTOF messages = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("TOTOF message roles = %q, %q, want system/user", msgs[0].Role, msgs[1].Role)
	}
	if strings.Contains(msgs[1].Content, "tool_calls") {
		t.Fatalf("TOTOF payload should not replay raw tool-call transcript: %q", msgs[1].Content)
	}
	if !strings.Contains(msgs[1].Content, "TASK") || !strings.Contains(msgs[1].Content, "KEY TOOL OUTCOMES") {
		t.Fatalf("TOTOF payload missing required sections: %q", msgs[1].Content)
	}
}

func TestBuildContinuationArtifact_PreservesCanonicalObservationState(t *testing.T) {
	s := New("sess-5", "agent-1", "Duncan")
	s.AddUserMessage("Read the config file and summarize the model settings.")
	s.AddAssistantMessage("", []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      "list_directory",
			Arguments: `{"path":"."}`,
		},
	}})
	s.AddToolResult("call-1", "list_directory", "FIRMWARE.toml\nOS.toml", false)

	artifact := s.BuildContinuationArtifact("Need to read the main config file before summarizing it.", "Continue execution from canonical continuation state.")
	if artifact.Task != "Read the config file and summarize the model settings." {
		t.Fatalf("Task = %q, want original task", artifact.Task)
	}
	if len(artifact.AuthoritativeObservations) != 1 || !strings.Contains(artifact.AuthoritativeObservations[0], "FIRMWARE.toml") {
		t.Fatalf("AuthoritativeObservations = %#v, want directory listing", artifact.AuthoritativeObservations)
	}
	if len(artifact.ToolOutcomes) != 1 || artifact.ToolOutcomes[0].ToolName != "list_directory" {
		t.Fatalf("ToolOutcomes = %#v, want list_directory outcome", artifact.ToolOutcomes)
	}
	if artifact.RemainingWork != "Need to read the main config file before summarizing it." {
		t.Fatalf("RemainingWork = %q", artifact.RemainingWork)
	}
	msgs := artifact.Messages()
	if len(msgs) != 2 {
		t.Fatalf("continuation messages = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("continuation roles = %q, %q, want system/user", msgs[0].Role, msgs[1].Role)
	}
	if !strings.Contains(msgs[1].Content, "REMAINING WORK") {
		t.Fatalf("continuation payload missing REMAINING WORK: %q", msgs[1].Content)
	}
	if strings.Contains(msgs[1].Content, "tool_calls") {
		t.Fatalf("continuation payload should not replay raw tool-call transcript: %q", msgs[1].Content)
	}

	s.SetContinuationArtifact(&artifact)
	consumed := s.ConsumeContinuationArtifact()
	if consumed == nil || consumed.RemainingWork != artifact.RemainingWork {
		t.Fatalf("consumed continuation = %#v, want remaining work %q", consumed, artifact.RemainingWork)
	}
	if again := s.ConsumeContinuationArtifact(); again != nil {
		t.Fatalf("continuation artifact should be one-shot, got %#v", again)
	}
}
