package llm

// CompressionStrategy defines how to compress messages.
type CompressionStrategy int

const (
	StrategyTruncate         CompressionStrategy = iota // Drop oldest messages
	StrategyDropLowRelevance                            // Drop least relevant
)

// PromptCompressor reduces token count before inference.
type PromptCompressor struct {
	strategy CompressionStrategy
}

// NewPromptCompressor creates a compressor with the given strategy.
func NewPromptCompressor(strategy CompressionStrategy) *PromptCompressor {
	return &PromptCompressor{strategy: strategy}
}

// Compress reduces messages to fit within tokenBudget.
func (pc *PromptCompressor) Compress(messages []Message, tokenBudget int) []Message {
	if len(messages) == 0 {
		return nil
	}

	total := estimateMessageTokens(messages)
	if total <= tokenBudget {
		return messages
	}

	switch pc.strategy {
	case StrategyTruncate:
		return pc.truncateOldest(messages, tokenBudget)
	case StrategyDropLowRelevance:
		return pc.truncateOldest(messages, tokenBudget) // same for now, upgradeable
	default:
		return pc.truncateOldest(messages, tokenBudget)
	}
}

func (pc *PromptCompressor) truncateOldest(messages []Message, budget int) []Message {
	// Keep messages from the end until budget is hit.
	var result []Message
	tokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := len(messages[i].Content) / 4
		if tokens+msgTokens > budget {
			break
		}
		result = append([]Message{messages[i]}, result...)
		tokens += msgTokens
	}
	if len(result) == 0 && len(messages) > 0 {
		result = messages[len(messages)-1:]
	}
	return result
}

func estimateMessageTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / 4
	}
	return total
}
