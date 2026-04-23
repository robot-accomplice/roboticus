package pipeline

import (
	"strings"
	"testing"

	"roboticus/internal/llm"
)

func TestCompactContext_Stage1_Verbatim(t *testing.T) {
	// Small history should pass through unchanged.
	msgs := []llm.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	result := CompactContext(msgs, 10000)
	if len(result) != len(msgs) {
		t.Errorf("verbatim stage: got %d messages, want %d", len(result), len(msgs))
	}
}

func TestCompactContext_Stage2_SelectiveTrim(t *testing.T) {
	// Build a history with many tool messages and duplicate system messages.
	// The old tool result and old system message add significant tokens.
	msgs := []llm.Message{
		{Role: "system", Content: strings.Repeat("old system prompt ", 40)},
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "Let me check."},
		{Role: "tool", Content: strings.Repeat("tool output data ", 100)}, // old tool result (big)
		{Role: "system", Content: "Updated system context"},
		{Role: "user", Content: "Second question"},
		{Role: "assistant", Content: "Here is the answer."},
	}

	verbatimTokens := estimateTokens(msgs)
	trimmedTokens := estimateTokens(selectiveTrim(msgs))

	// Set budget between trimmed and verbatim so stage 2 kicks in.
	budget := (verbatimTokens + trimmedTokens) / 2
	result := CompactContext(msgs, budget)

	// Should have fewer messages than original (old system + old tool dropped).
	if len(result) >= len(msgs) {
		t.Errorf("selective trim should reduce messages: got %d, original %d (budget=%d, verbatim=%d, trimmed=%d)",
			len(result), len(msgs), budget, verbatimTokens, trimmedTokens)
	}

	// The last system message should be preserved.
	hasUpdatedSystem := false
	for _, m := range result {
		if m.Role == "system" && m.Content == "Updated system context" {
			hasUpdatedSystem = true
		}
	}
	if !hasUpdatedSystem {
		t.Error("selective trim should preserve the most recent system message")
	}
}

func TestSelectiveTrim_DropsOldToolExchangeAtomically(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: "old system"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID:   "call_old",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "schedule_cron",
				Arguments: `{"name":"quiet ticker"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "call_old", Name: "schedule_cron", Content: `{"status":"ok"}`},
		{Role: "system", Content: "new system"},
		{Role: "user", Content: "latest question"},
		{Role: "assistant", Content: "latest answer"},
	}

	result := selectiveTrim(msgs)
	assertNoOrphanedToolCallHistory(t, result)
	for _, m := range result {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "call_old" {
			t.Fatal("old tool-call exchange should have been dropped together")
		}
	}
}

func TestSkeleton_DoesNotOrphanRecentToolExchange(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: "older user"},
		{Role: "assistant", Content: "older assistant"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID:   "call_recent",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "create_cron_job",
				Arguments: `{"name":"quiet ticker"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "call_recent", Name: "create_cron_job", Content: `{"status":"ok"}`},
		{Role: "user", Content: "new user"},
		{Role: "assistant", Content: "new assistant"},
	}

	result := skeleton(msgs)
	assertNoOrphanedToolCallHistory(t, result)
}

func TestCompactContext_SemanticCompressPreservesToolExchangeAtomicity(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: strings.Repeat("system prompt ", 20)},
		{Role: "user", Content: strings.Repeat("older user message ", 80)},
		{Role: "assistant", Content: strings.Repeat("older assistant response ", 80)},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID:   "call_mid",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "create_cron_job",
				Arguments: `{"name":"quiet ticker"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "call_mid", Name: "create_cron_job", Content: strings.Repeat(`{"status":"ok"} `, 20)},
		{Role: "user", Content: strings.Repeat("recent user message ", 30)},
		{Role: "assistant", Content: strings.Repeat("recent assistant response ", 30)},
		{Role: "user", Content: "final user"},
		{Role: "assistant", Content: "final assistant"},
	}

	verbatimTokens := estimateTokens(msgs)
	trimmedTokens := estimateTokens(selectiveTrim(msgs))
	if trimmedTokens >= verbatimTokens {
		t.Fatalf("expected selective trim to reduce tokens: verbatim=%d trimmed=%d", verbatimTokens, trimmedTokens)
	}
	// Force stage 3: below selective trim, above semantic compress.
	result := CompactContext(msgs, trimmedTokens-1)
	assertNoOrphanedToolCallHistory(t, result)
}

func TestCompactContext_Stage5_Skeleton(t *testing.T) {
	// Build a very long history.
	var msgs []llm.Message
	msgs = append(msgs, llm.Message{Role: "system", Content: "System prompt"})
	for i := 0; i < 50; i++ {
		msgs = append(msgs, llm.Message{Role: "user", Content: strings.Repeat("user message ", 100)})
		msgs = append(msgs, llm.Message{Role: "assistant", Content: strings.Repeat("assistant response ", 100)})
	}

	// Very tight budget forces skeleton.
	result := CompactContext(msgs, 200)

	// Skeleton should have system + last 2 pairs (up to 5 messages).
	if len(result) > 5 {
		t.Errorf("skeleton: got %d messages, want <= 5", len(result))
	}

	// First message should be system.
	if len(result) > 0 && result[0].Role != "system" {
		t.Errorf("skeleton should start with system message, got %q", result[0].Role)
	}
}

func TestCompactContext_EmptyMessages(t *testing.T) {
	result := CompactContext(nil, 1000)
	if result != nil {
		t.Errorf("empty input should return nil, got %v", result)
	}
}

func TestCompactContext_ZeroBudget(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "hello"}}
	result := CompactContext(msgs, 0)
	if len(result) != 1 {
		t.Errorf("zero budget should return original, got %d messages", len(result))
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: strings.Repeat("a", 400)}, // 400/4 + 4 = 104 tokens
	}
	tokens := EstimateMessageTokens(msgs)
	if tokens != 104 {
		t.Errorf("estimated %d tokens, want 104", tokens)
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"Hello world. How are you? Fine!", 3},
		{"No punctuation", 1},
		{"", 0},
		{"One. Two. Three.", 3},
	}
	for _, tc := range tests {
		got := splitSentences(tc.input)
		if len(got) != tc.want {
			t.Errorf("splitSentences(%q) = %d sentences, want %d: %v", tc.input, len(got), tc.want, got)
		}
	}
}

func TestSelectiveTrim_KeepsLastSystem(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: "old system"},
		{Role: "system", Content: "new system"},
		{Role: "user", Content: "hi"},
	}
	result := selectiveTrim(msgs)

	systemCount := 0
	for _, m := range result {
		if m.Role == "system" {
			systemCount++
			if m.Content != "new system" {
				t.Errorf("should keep latest system, got %q", m.Content)
			}
		}
	}
	if systemCount != 1 {
		t.Errorf("should have 1 system message, got %d", systemCount)
	}
}

func TestSkeleton_PreservesRecentTurns(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: "old user"},
		{Role: "assistant", Content: "old assistant"},
		{Role: "user", Content: "new user"},
		{Role: "assistant", Content: "new assistant"},
	}
	result := skeleton(msgs)

	// Should have system + last 4 user/assistant messages.
	if len(result) < 3 {
		t.Fatalf("skeleton too short: %d messages", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("first message should be system, got %q", result[0].Role)
	}

	// Last message should be the newest assistant response.
	last := result[len(result)-1]
	if last.Content != "new assistant" {
		t.Errorf("last message = %q, want %q", last.Content, "new assistant")
	}
}

func TestExtractKeyPoints(t *testing.T) {
	text := "The deployment was successful. Everything worked as expected. However, there was an important note about rate limits. Nothing else happened."
	points := extractKeyPoints(text)
	if points == "" {
		t.Fatal("extractKeyPoints returned empty string")
	}
	if !strings.Contains(points, "successful") {
		t.Error("should include first sentence")
	}
	if !strings.Contains(points, "important") {
		t.Error("should include sentence with 'important' marker")
	}
}

func TestSemanticCompress_ShortHistory(t *testing.T) {
	// With <= 4 messages, should return as-is.
	msgs := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	result := semanticCompress(msgs)
	if len(result) != len(msgs) {
		t.Errorf("short history should pass through, got %d want %d", len(result), len(msgs))
	}
}

func assertNoOrphanedToolCallHistory(t *testing.T, msgs []llm.Message) {
	t.Helper()
	for i, m := range msgs {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		callIDs := make(map[string]struct{}, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			callIDs[tc.ID] = struct{}{}
		}
		seen := map[string]struct{}{}
		for j := i + 1; j < len(msgs); j++ {
			next := msgs[j]
			if next.Role != "tool" {
				break
			}
			if _, ok := callIDs[next.ToolCallID]; ok {
				seen[next.ToolCallID] = struct{}{}
			}
		}
		for _, tc := range m.ToolCalls {
			if _, ok := seen[tc.ID]; !ok {
				t.Fatalf("assistant tool_call %q missing matching tool result in compacted history", tc.ID)
			}
		}
	}
}
