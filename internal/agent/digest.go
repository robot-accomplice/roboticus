package agent

import (
	"context"
	"fmt"

	"roboticus/internal/llm"
)

type DigestGenerator struct {
	summarizer Summarizer // from compaction.go — same interface
}

func NewDigestGenerator(summarizer Summarizer) *DigestGenerator {
	return &DigestGenerator{summarizer: summarizer}
}

func (dg *DigestGenerator) GenerateDigest(ctx context.Context, messages []llm.Message) (string, error) {
	if len(messages) == 0 {
		return "No messages to summarize.", nil
	}
	if dg.summarizer == nil {
		return dg.simpleFallback(messages), nil
	}
	return dg.summarizer.Summarize(ctx, messages)
}

func (dg *DigestGenerator) simpleFallback(messages []llm.Message) string {
	userCount := 0
	assistantCount := 0
	toolCount := 0
	for _, m := range messages {
		switch m.Role {
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		case "tool":
			toolCount++
		}
	}
	return fmt.Sprintf("Session digest: %d user messages, %d assistant responses, %d tool calls.",
		userCount, assistantCount, toolCount)
}

func (dg *DigestGenerator) ShouldDigest(messageCount int, intervalMessages int) bool {
	if intervalMessages <= 0 {
		intervalMessages = 20
	}
	return messageCount > 0 && messageCount%intervalMessages == 0
}
