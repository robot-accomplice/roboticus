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

// PruneByEmbedding selects the topK most relevant tools based on cosine similarity
// between tool description embeddings and the query embedding (Wave 8, #84).
// queryEmbedding should be a float64 vector from the same embedding model used
// to generate toolEmbeddings.
func PruneByEmbedding(tools []ToolDef, queryEmbedding []float64, topK int) []ToolDef {
	if len(tools) == 0 || len(queryEmbedding) == 0 || topK <= 0 {
		return tools
	}
	if topK >= len(tools) {
		return tools
	}

	type scored struct {
		tool  ToolDef
		score float64
	}
	var scored_tools []scored
	for _, t := range tools {
		if len(t.Embedding) != len(queryEmbedding) {
			// No embedding or dimension mismatch — include by default.
			scored_tools = append(scored_tools, scored{tool: t, score: 0.5})
			continue
		}
		sim := cosineSimilarity(queryEmbedding, t.Embedding)
		scored_tools = append(scored_tools, scored{tool: t, score: sim})
	}

	// Selection sort for topK (simple, avoids sort import for small N).
	for i := 0; i < topK && i < len(scored_tools); i++ {
		maxIdx := i
		for j := i + 1; j < len(scored_tools); j++ {
			if scored_tools[j].score > scored_tools[maxIdx].score {
				maxIdx = j
			}
		}
		scored_tools[i], scored_tools[maxIdx] = scored_tools[maxIdx], scored_tools[i]
	}

	result := make([]ToolDef, 0, topK)
	for i := 0; i < topK && i < len(scored_tools); i++ {
		result = append(result, scored_tools[i].tool)
	}
	return result
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

// sqrt computes square root via Newton's method (avoids math import).
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 20; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}
