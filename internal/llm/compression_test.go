package llm

import (
	"strings"
	"testing"
)

func TestPromptCompressor_UnderBudget(t *testing.T) {
	pc := NewPromptCompressor(StrategyTruncate)
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	result := pc.Compress(msgs, 1000)
	if len(result) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(result))
	}
}

func TestPromptCompressor_EmptyMessages(t *testing.T) {
	pc := NewPromptCompressor(StrategyTruncate)
	result := pc.Compress(nil, 100)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
	result = pc.Compress([]Message{}, 100)
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestPromptCompressor_OverBudgetTruncation(t *testing.T) {
	pc := NewPromptCompressor(StrategyTruncate)

	// Each message is 40 chars → ~10 tokens each. Budget = 15 → only 1 fits.
	content := strings.Repeat("a", 40) // 40 chars / 4 = 10 tokens
	msgs := []Message{
		{Role: "user", Content: content},
		{Role: "assistant", Content: content},
		{Role: "user", Content: content},
	}

	result := pc.Compress(msgs, 15)
	// Should keep only the last message(s) that fit.
	if len(result) == 0 {
		t.Error("expected at least one message")
	}
	if len(result) >= len(msgs) {
		t.Errorf("expected fewer than %d messages after compression, got %d", len(msgs), len(result))
	}
	// The kept messages should be from the end.
	if result[len(result)-1].Content != content {
		t.Error("last message should be preserved")
	}
}

func TestPromptCompressor_KeepsAtLeastOne(t *testing.T) {
	pc := NewPromptCompressor(StrategyTruncate)

	// Budget so small that even one message doesn't fit (0 tokens).
	content := strings.Repeat("z", 100) // 100/4 = 25 tokens
	msgs := []Message{
		{Role: "user", Content: content},
		{Role: "assistant", Content: content},
	}

	result := pc.Compress(msgs, 0)
	// Should fall back to at least the last message.
	if len(result) == 0 {
		t.Error("should keep at least one message even when budget=0")
	}
}

func TestPromptCompressor_DropLowRelevance(t *testing.T) {
	// Currently aliases to truncateOldest — just verify it doesn't panic.
	pc := NewPromptCompressor(StrategyDropLowRelevance)
	msgs := []Message{
		{Role: "user", Content: strings.Repeat("x", 200)},
		{Role: "assistant", Content: strings.Repeat("y", 200)},
		{Role: "user", Content: strings.Repeat("z", 200)},
	}
	result := pc.Compress(msgs, 20)
	if len(result) == 0 {
		t.Error("expected at least one message")
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "abcd"},          // 4 chars = 1 token
		{Role: "assistant", Content: "abcdefgh"}, // 8 chars = 2 tokens
	}
	total := estimateMessageTokens(msgs)
	if total != 3 {
		t.Errorf("expected 3 tokens, got %d", total)
	}
}
