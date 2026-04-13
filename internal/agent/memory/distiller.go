package memory

import (
	"context"
	"fmt"
	"strings"

	"roboticus/internal/llm"
)

// ServiceDistiller implements LLMDistiller by calling the LLM service to
// distill episodic memory groups into semantic facts during consolidation.
// This is the production adapter that backs the LLMDistiller interface.
type ServiceDistiller struct {
	LLMSvc *llm.Service
}

// Distill sends the episodic entries to the LLM with a distillation prompt
// and returns the model's single-sentence knowledge extraction.
func (d *ServiceDistiller) Distill(ctx context.Context, entries []string) (string, error) {
	if d.LLMSvc == nil {
		return "", fmt.Errorf("LLM service not available")
	}

	var prompt strings.Builder
	prompt.WriteString("You are a memory consolidation system. Given these related events from an agent's history, ")
	prompt.WriteString("distill the single most important lesson or fact in one clear sentence. ")
	prompt.WriteString("Output ONLY the distilled fact, nothing else.\n\nEvents:\n")
	for i, e := range entries {
		fmt.Fprintf(&prompt, "%d. %s\n", i+1, e)
	}

	resp, err := d.LLMSvc.Complete(ctx, &llm.Request{
		Messages:  []llm.Message{{Role: "user", Content: prompt.String()}},
		MaxTokens: 100,
	})
	if err != nil {
		return "", fmt.Errorf("distillation inference failed: %w", err)
	}
	result := strings.TrimSpace(resp.Content)
	if result == "" {
		return "", fmt.Errorf("distillation returned empty response")
	}
	return result, nil
}
