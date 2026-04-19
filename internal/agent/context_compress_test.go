package agent

import (
	"strings"
	"testing"

	"roboticus/internal/llm"
)

func TestBuildRequest_PromptCompressionPreservesLastUserMessage(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 100000
	cfg.PromptCompression = true
	cfg.CompressionTargetRatio = 0.25

	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt(strings.Repeat("system important context ", 30))
	cb.SetMemory(strings.Repeat("memory important context ", 30))
	cb.AppendSystemNote(strings.Repeat("hippocampus table summary ", 20))

	sess := NewSession("s1", "agent-id", "Test")
	sess.AddUserMessage(strings.Repeat("older user context ", 20))
	sess.AddAssistantMessage(strings.Repeat("older assistant context ", 20), nil)
	const finalPrompt = "FINAL_USER_PROMPT should remain verbatim with all details intact"
	sess.AddUserMessage(finalPrompt)

	req := cb.BuildRequest(sess)

	var finalUser string
	for _, msg := range req.Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "FINAL_USER_PROMPT") {
			finalUser = msg.Content
		}
	}
	if finalUser != finalPrompt {
		t.Fatalf("last user message was compressed or altered: got %q want %q", finalUser, finalPrompt)
	}
}

func TestBuildRequest_PromptCompressionPreservesSystemAndMemory(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 100000
	cfg.PromptCompression = true
	cfg.CompressionTargetRatio = 0.25

	cb := NewContextBuilder(cfg)
	longSystem := strings.Repeat("system context token ", 40)
	longMemory := strings.Repeat("memory context token ", 40)
	cb.SetSystemPrompt(longSystem)
	cb.SetMemory(longMemory)

	sess := NewSession("s1", "agent-id", "Test")
	longAssistant := strings.Repeat("assistant context token ", 40)
	sess.AddAssistantMessage(longAssistant, nil)
	sess.AddUserMessage("short current question")

	req := cb.BuildRequest(sess)
	if len(req.Messages) < 4 {
		t.Fatalf("unexpected request shape: got %d messages want >= 4", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Fatalf("message[0] role = %q want system", req.Messages[0].Role)
	}
	if req.Messages[0].Content != longSystem {
		t.Fatalf("system prompt should remain verbatim under prompt compression")
	}
	if req.Messages[1].Role != "system" {
		t.Fatalf("message[1] role = %q want system", req.Messages[1].Role)
	}
	if req.Messages[1].Content != longMemory {
		t.Fatalf("memory block should remain verbatim under prompt compression")
	}
}

func TestBuildRequest_PromptCompressionCompressesEarlierHistory(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 100000
	cfg.PromptCompression = true
	cfg.CompressionTargetRatio = 0.25

	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt(strings.Repeat("system context token ", 40))
	cb.SetMemory(strings.Repeat("memory context token ", 40))

	sess := NewSession("s1", "agent-id", "Test")
	longOlderUser := strings.Repeat("older user context token ", 40)
	longAssistant := strings.Repeat("assistant context token ", 40)
	sess.AddUserMessage(longOlderUser)
	sess.AddAssistantMessage(longAssistant, nil)
	sess.AddUserMessage("short current question")

	req := cb.BuildRequest(sess)
	var assistantSeen bool
	var olderUserSeen bool
	for _, msg := range req.Messages {
		switch msg.Role {
		case "assistant":
			if msg.Content == longAssistant || len(msg.Content) >= len(longAssistant) {
				t.Fatalf("assistant history was not compressed: len=%d want <%d", len(msg.Content), len(longAssistant))
			}
			assistantSeen = true
		case "user":
			if strings.Contains(msg.Content, "older user context token") {
				if msg.Content == longOlderUser || len(msg.Content) >= len(longOlderUser) {
					t.Fatalf("older user history was not compressed: len=%d want <%d", len(msg.Content), len(longOlderUser))
				}
				olderUserSeen = true
			}
		}
	}
	if !assistantSeen {
		t.Fatal("did not observe assistant history in request")
	}
	if !olderUserSeen {
		t.Fatal("did not observe compressed older user history in request")
	}
}

func TestCompressContextMessages_NoUserMessageCompressesAssistantButNotSystem(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: strings.Repeat("system token ", 30)},
		{Role: "assistant", Content: strings.Repeat("assistant token ", 30)},
	}
	before0 := msgs[0].Content
	before1 := msgs[1].Content

	CompressContextMessages(msgs, 0.25)

	if msgs[0].Content != before0 {
		t.Fatalf("system message should remain verbatim without user message")
	}
	if len(msgs[1].Content) >= len(before1) {
		t.Fatalf("expected assistant message compression without user message")
	}
}

func TestCompressContextMessages_LeavesToolMessagesUntouched(t *testing.T) {
	msgs := []llm.Message{
		{Role: "tool", Content: strings.Repeat("tool output token ", 30), ToolCallID: "call-1"},
		{Role: "assistant", Content: strings.Repeat("assistant token ", 30)},
	}
	toolBefore := msgs[0].Content
	assistantBefore := msgs[1].Content

	CompressContextMessages(msgs, 0.25)

	if msgs[0].Content != toolBefore {
		t.Fatal("tool message should remain verbatim under prompt compression")
	}
	if len(msgs[1].Content) >= len(assistantBefore) {
		t.Fatal("assistant message should still be compressed")
	}
}
