package pipeline

import "context"

// Embedder provides text embedding for relevance scoring.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ToolPruner reduces the tool list to fit within a token budget.
type ToolPruner struct {
	maxToolTokens int
	embedder      Embedder // optional, for relevance-based pruning
}

// NewToolPruner creates a tool pruner with the given token budget.
func NewToolPruner(maxTokens int, embedder Embedder) *ToolPruner {
	return &ToolPruner{maxToolTokens: maxTokens, embedder: embedder}
}

// Prune reduces the tool list to fit within the token budget.
// Strategy:
//  1. Always include tools used in the current session (sessionTools).
//  2. Fill remaining budget with other tools (sorted by name for determinism).
//  3. Drop tools that would exceed the budget.
func (tp *ToolPruner) Prune(_ context.Context, tools []ToolDef, _ string, sessionTools []string) []ToolDef {
	if len(tools) == 0 {
		return nil
	}

	// Check if everything fits.
	total := 0
	for _, td := range tools {
		total += td.EstimateTokens()
	}
	if total <= tp.maxToolTokens {
		return tools
	}

	// Build session tool set for O(1) lookup.
	sessionSet := make(map[string]bool, len(sessionTools))
	for _, name := range sessionTools {
		sessionSet[name] = true
	}

	// Phase 1: Include session tools first (they always get priority).
	var result []ToolDef
	budget := tp.maxToolTokens
	for _, td := range tools {
		if sessionSet[td.Name] {
			cost := td.EstimateTokens()
			if budget >= cost {
				result = append(result, td)
				budget -= cost
			}
		}
	}

	// Phase 2: Fill remaining budget with non-session tools.
	for _, td := range tools {
		if sessionSet[td.Name] {
			continue // already included
		}
		cost := td.EstimateTokens()
		if budget >= cost {
			result = append(result, td)
			budget -= cost
		}
	}

	return result
}
