package agent

import (
	"context"

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
		return "", nil
	}
	if dg.summarizer == nil {
		return "", nil
	}
	return dg.summarizer.Summarize(ctx, messages)
}

func (dg *DigestGenerator) ShouldDigest(messageCount int, intervalMessages int) bool {
	if intervalMessages <= 0 {
		intervalMessages = 20
	}
	return messageCount > 0 && messageCount%intervalMessages == 0
}
