package agent

import (
	"context"
	"testing"

	"goboticus/internal/llm"
)

type mockSummarizer struct {
	summarizeFunc func(ctx context.Context, messages []llm.Message) (string, error)
}

func (m *mockSummarizer) Summarize(ctx context.Context, messages []llm.Message) (string, error) {
	if m.summarizeFunc != nil {
		return m.summarizeFunc(ctx, messages)
	}
	return "Summary of conversation.", nil
}

func TestCompactor_UnderBudget(t *testing.T) {
	c := NewCompactor(10000, &mockSummarizer{})
	msgs := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	result, err := c.Compact(context.Background(), msgs, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("under-budget should pass through: got %d, want 2", len(result))
	}
}

func TestCompactor_OverBudget(t *testing.T) {
	summarizer := &mockSummarizer{
		summarizeFunc: func(_ context.Context, msgs []llm.Message) (string, error) {
			return "Summarized older context.", nil
		},
	}
	c := NewCompactor(100, summarizer) // very small budget

	msgs := make([]llm.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = llm.Message{Role: role, Content: "This is message number " + string(rune('A'+i))}
	}

	result, err := c.Compact(context.Background(), msgs, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) >= 20 {
		t.Error("over-budget should compact messages")
	}
	// First message should be the summary
	if result[0].Role != "system" {
		t.Errorf("first message should be system summary, got role %q", result[0].Role)
	}
}

func TestCompactor_EmptyMessages(t *testing.T) {
	c := NewCompactor(1000, &mockSummarizer{})
	result, err := c.Compact(context.Background(), nil, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("empty input should return empty: got %d", len(result))
	}
}

func TestCompactor_SummarizerError(t *testing.T) {
	summarizer := &mockSummarizer{
		summarizeFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "", context.DeadlineExceeded
		},
	}
	c := NewCompactor(10, summarizer) // tiny budget forces compaction

	msgs := []llm.Message{
		{Role: "user", Content: "a long message that exceeds budget"},
		{Role: "assistant", Content: "a long response that also exceeds"},
	}

	_, err := c.Compact(context.Background(), msgs, 10)
	if err == nil {
		t.Error("expected error from summarizer")
	}
}
