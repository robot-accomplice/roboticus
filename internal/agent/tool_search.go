package agent

import (
	"sort"
	"strings"
	"sync"
)

// RankedTool is a tool scored by semantic relevance for the current query.
type RankedTool struct {
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	AdjustedScore float64 `json:"adjusted_score"`
	TokenCost     int     `json:"token_cost"`
	Source        string  `json:"source"` // "builtin", "plugin", "mcp"
}

// ToolSearchConfig controls tool ranking and pruning behavior.
type ToolSearchConfig struct {
	TopK              int      `json:"top_k" mapstructure:"top_k"`                             // max tools to return (default 15)
	TokenBudget       int      `json:"token_budget" mapstructure:"token_budget"`                // max token cost (default 4000)
	MCPLatencyPenalty float64  `json:"mcp_latency_penalty" mapstructure:"mcp_latency_penalty"`  // score penalty for MCP tools (default 0.05)
	AlwaysInclude     []string `json:"always_include" mapstructure:"always_include"`            // tools always included
}

// DefaultToolSearchConfig returns sensible defaults.
func DefaultToolSearchConfig() ToolSearchConfig {
	return ToolSearchConfig{
		TopK:              15,
		TokenBudget:       4000,
		MCPLatencyPenalty: 0.05,
		AlwaysInclude:     []string{"memory_store", "delegate"},
	}
}

// ToolSearchEngine ranks and prunes tools based on semantic relevance
// to the current user query. This reduces prompt bloat and LLM latency
// by only presenting relevant tools.
type ToolSearchEngine struct {
	mu         sync.RWMutex
	embeddings map[string][]float32 // tool name → embedding
	dims       int
	config     ToolSearchConfig
}

// ToolSearchStats reports search metrics for observability.
type ToolSearchStats struct {
	CandidatesConsidered int             `json:"candidates_considered"`
	CandidatesSelected   int             `json:"candidates_selected"`
	CandidatesPruned     int             `json:"candidates_pruned"`
	TokenSavings         int             `json:"token_savings"`
	TopScores            []RankedTool    `json:"top_scores"`
}

// NewToolSearchEngine creates a tool search engine with the given config.
func NewToolSearchEngine(cfg ToolSearchConfig) *ToolSearchEngine {
	if cfg.TopK < 1 {
		cfg.TopK = 15
	}
	if cfg.TokenBudget < 1 {
		cfg.TokenBudget = 4000
	}
	return &ToolSearchEngine{
		embeddings: make(map[string][]float32),
		dims:       128,
		config:     cfg,
	}
}

// IndexTool computes and caches the embedding for a tool's description.
func (e *ToolSearchEngine) IndexTool(name, description, source string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.embeddings[name] = NgramEmbedding(strings.ToLower(description), e.dims)
}

// Search ranks all indexed tools against the query and returns the top-K
// within the token budget.
func (e *ToolSearchEngine) Search(query string, tools []ToolDescriptor) ([]RankedTool, ToolSearchStats) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	queryVec := NgramEmbedding(strings.ToLower(query), e.dims)
	stats := ToolSearchStats{CandidatesConsidered: len(tools)}

	// Score all tools.
	candidates := make([]RankedTool, 0, len(tools))
	totalTokenCost := 0
	for _, t := range tools {
		emb, ok := e.embeddings[t.Name]
		if !ok {
			emb = NgramEmbedding(strings.ToLower(t.Description), e.dims)
		}
		rawScore := CosineSimilarity(queryVec, emb)
		penalty := 0.0
		if t.Source == "mcp" {
			penalty = e.config.MCPLatencyPenalty
		}
		adjusted := rawScore - penalty
		if adjusted < 0 {
			adjusted = 0
		}
		totalTokenCost += t.TokenCost
		candidates = append(candidates, RankedTool{
			Name:          t.Name,
			Description:   t.Description,
			AdjustedScore: adjusted,
			TokenCost:     t.TokenCost,
			Source:        t.Source,
		})
	}

	// Sort by score descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].AdjustedScore > candidates[j].AdjustedScore
	})

	// Prune to top-K within token budget, always including pinned tools.
	pinned := make(map[string]bool)
	for _, name := range e.config.AlwaysInclude {
		pinned[name] = true
	}

	var selected []RankedTool
	usedTokens := 0
	for _, c := range candidates {
		if len(selected) >= e.config.TopK && !pinned[c.Name] {
			continue
		}
		if usedTokens+c.TokenCost > e.config.TokenBudget && !pinned[c.Name] {
			continue
		}
		selected = append(selected, c)
		usedTokens += c.TokenCost
	}

	stats.CandidatesSelected = len(selected)
	stats.CandidatesPruned = len(candidates) - len(selected)
	stats.TokenSavings = totalTokenCost - usedTokens
	if len(candidates) > 10 {
		stats.TopScores = candidates[:10]
	} else {
		stats.TopScores = candidates
	}
	return selected, stats
}

// ToolDescriptor describes a tool for search indexing.
type ToolDescriptor struct {
	Name        string
	Description string
	TokenCost   int
	Source      string // "builtin", "plugin", "mcp"
}
