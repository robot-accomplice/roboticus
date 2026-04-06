package agent

import (
	"context"

	"roboticus/internal/llm"
)

// Summarizer produces a summary of a message sequence.
type Summarizer interface {
	Summarize(ctx context.Context, messages []llm.Message) (string, error)
}

// Compactor compresses conversation history to fit within a token budget.
type Compactor struct {
	maxTokens  int
	summarizer Summarizer
}

// NewCompactor creates a compactor with the given budget and summarizer.
func NewCompactor(maxTokens int, summarizer Summarizer) *Compactor {
	return &Compactor{maxTokens: maxTokens, summarizer: summarizer}
}

// Compact reduces message history to fit within budget.
// Strategy: keep the most recent messages (60% of budget), summarize older ones (40%).
func (c *Compactor) Compact(ctx context.Context, messages []llm.Message, budget int) ([]llm.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	// Estimate total tokens.
	total := estimateTokens(messages)
	if total <= budget {
		return messages, nil
	}

	// Find the split point: keep recent messages that fit in 60% of budget.
	recentBudget := budget * 60 / 100
	splitIdx := len(messages)
	recentTokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := len(messages[i].Content) / 4 // ~4 chars per token
		if recentTokens+msgTokens > recentBudget {
			splitIdx = i + 1
			break
		}
		recentTokens += msgTokens
	}

	// If we can't even fit one message, keep just the last one.
	if splitIdx >= len(messages) {
		splitIdx = len(messages) - 1
	}

	// Summarize older messages.
	older := messages[:splitIdx]
	if len(older) == 0 {
		return messages[splitIdx:], nil
	}

	summary, err := c.summarizer.Summarize(ctx, older)
	if err != nil {
		return nil, err
	}

	// Build result: summary + recent messages.
	result := make([]llm.Message, 0, 1+len(messages)-splitIdx)
	result = append(result, llm.Message{
		Role:    "system",
		Content: "[Context summary] " + summary,
	})
	result = append(result, messages[splitIdx:]...)

	return result, nil
}

// estimateTokens returns a rough token estimate for a message sequence.
func estimateTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4 // ~4 chars per token heuristic
	}
	return total
}
