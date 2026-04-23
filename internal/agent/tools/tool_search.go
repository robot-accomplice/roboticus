// tool_search.go — Semantic tool search. Rank and prune tools before
// presenting them to the LLM.
//
// v1.0.6 Rust-parity closure: the Rust reference implementation
// (roboticus-agent/src/tool_search.rs) embeds tool descriptions at
// registration time, ranks them against the user query embedding at
// inference time, and prunes to top-K within a token budget. The
// original Go port dropped this entirely and bulk-injected every
// registered tool (46 tools × ~128 tokens ≈ 5886 tokens per request),
// pushing requests past the runtime context ceiling on
// memory-constrained models. This file closes that regression with a
// faithful port of the Rust algorithm and config defaults.
//
// Pipeline shape at request time:
//
//   [all registered tools] → rank by cosine similarity to query →
//   [scored candidates]    → pin always-include + top-K within budget →
//   [pruned set]           → injected into LLM request
//
// MCP tools receive a small latency penalty so local tools win ties
// (matches Rust: remote calls add round-trip cost).
//
// Config surface (roboticus.toml — Go-native; see core.ToolSearchConfig):
//
//   [tool_search]
//   top_k = 15
//   token_budget = 4000
//   mcp_latency_penalty = 0.05
//   always_include = ["recall_memory", "search_memories",
//                     "get_memory_stats", "get_runtime_context",
//                     "get_subagent_status", "list-subagent-roster",
//                     "list-available-skills", "compose-skill"]
//
// Rust's equivalent has no TOML section — Go's is an Improvement (operator
// overridability), recorded as an Intentional Deviation in System 02.
//
// The always_include default is a functional analogue of Rust's runtime
// `always_include_operational_tools` (12-item list in
// `crates/roboticus-pipeline/src/core/tool_prune.rs`). Go pins the subset
// of names that map onto tools which actually exist in Go's registry —
// memory recall primitives (Go improvements per System 03 SYS-03-005),
// Rust-parity introspection tools, and the v1.0.7 roster/inventory/composition
// tools restored through PAR-002 and PAR-004. Pinning names that aren't
// registered is a
// silent no-op, so the list is deliberately Go-native.
// Do not restore Rust's `SearchConfig::default` values
// (`["memory_store", "delegate"]`) — that default is a Rust agent-crate
// test fixture and is not the list Rust uses at runtime.

package tools

import (
	"math"
	"sort"
)

// ToolSource captures where a tool came from. Used for MCP latency
// penalty and telemetry.
type ToolSource int

const (
	// ToolSourceBuiltIn is a locally-implemented tool (roboticus
	// process, no IPC, no remote call).
	ToolSourceBuiltIn ToolSource = iota
	// ToolSourcePlugin is a tool loaded from a script/plugin
	// directory. Local execution, no latency penalty.
	ToolSourcePlugin
	// ToolSourceMCP is a tool exposed via an MCP server. Remote
	// call, incurs latency penalty in ranking to break ties in
	// favor of local alternatives.
	ToolSourceMCP
)

// ToolDescriptor is a tool annotated with its cached embedding and
// token cost for ranking and budget enforcement. Built once at
// registration time; reused on every request.
type ToolDescriptor struct {
	// Name is the tool's canonical identifier (matches Tool.Name()).
	Name string

	// Description is the human-readable summary the model sees.
	// Embedding is computed over (Name + Description).
	Description string

	// TokenCost is the estimated token count of this tool's full
	// schema (name + description + parameters JSON). Measured once
	// at registration; used for budget accounting at request time.
	TokenCost int

	// Source identifies the provenance (builtin, plugin, MCP). MCP
	// tools get a ranking penalty.
	Source ToolSource

	// MCPServer, when Source == ToolSourceMCP, carries the server
	// identifier (for telemetry + tie-breaking between MCP sources).
	// Empty for non-MCP tools.
	MCPServer string

	// Embedding is the cached vector embedding of (Name +
	// Description). May be nil if embedding was never computed or
	// if the embedding provider was unavailable at registration.
	// Nil-embedded tools score 0.0 and are pinned by
	// always_include or lost to higher-scoring alternatives.
	Embedding []float32
}

// ToolSearchConfig controls ranking and pruning behavior. Defaults
// match Rust's SearchConfig (roboticus-agent/src/tool_search.rs).
type ToolSearchConfig struct {
	// TopK is the maximum number of tools to include after pruning,
	// regardless of token budget. Rust default: 15.
	TopK int

	// TokenBudget is the cumulative token cap for the selected tool
	// set. Rust default: 4000 (~47% of a typical 8192-ceiling
	// model's total context).
	TokenBudget int

	// MCPLatencyPenalty is subtracted from the raw cosine-similarity
	// score for MCP tools before ranking. Rust default: 0.05 —
	// small enough that a strong semantic match still wins, large
	// enough to break ties in favor of local tools.
	MCPLatencyPenalty float64

	// AlwaysInclude names tools pinned into the selected set regardless
	// of score. Consumed from budget first. See the package doc comment
	// for why Go's list differs from Rust's agent-crate test fixture
	// and how it relates to Rust's runtime
	// `always_include_operational_tools`.
	AlwaysInclude []string
}

// DefaultToolSearchConfig returns the Go defaults for tool search.
// Operators override via [tool_search] in roboticus.toml (see
// core.ToolSearchConfig). Ranking knobs match Rust's
// `SearchConfig::default` values; AlwaysInclude is a Go-native
// functional analogue of Rust's runtime
// `always_include_operational_tools`, pinning the memory-recall
// primitives Go refined beyond the Rust baseline (see System 03
// SYS-03-005), the Rust-parity introspection tools Go registers, and the
// explicit roster/inventory/composition tools restored in PAR-002 and
// PAR-004.
//
// This helper exists for callers that construct ToolSearchConfig
// outside the normal config-loading path (tests, ad-hoc tooling, and
// the pipeline's own defensive fallbacks). Production code receives
// ToolSearchConfig from core.Config; do not rely on this helper to
// derive the authoritative operator values.
func DefaultToolSearchConfig() ToolSearchConfig {
	return ToolSearchConfig{
		TopK:              15,
		TokenBudget:       4000,
		MCPLatencyPenalty: 0.05,
		AlwaysInclude: []string{
			"recall_memory",
			"search_memories",
			"get_memory_stats",
			"get_runtime_context",
			"get_subagent_status",
			"obsidian_write",
			"list-subagent-roster",
			"list-available-skills",
			"compose-skill",
			"compose-subagent",
			"orchestrate-subagents",
			"task-status",
			"list-open-tasks",
		},
	}
}

// RankedTool is the result of ranking + pruning: a tool descriptor
// plus its computed scores. Exported so telemetry consumers (trace
// annotations, /api/admin/tool-search) can inspect selections.
type RankedTool struct {
	Descriptor    *ToolDescriptor
	RawScore      float64 // raw cosine similarity, pre-penalty
	AdjustedScore float64 // raw - penalty (floored at 0)
}

// ToolSearchStats is the telemetry surface for a single ranking pass.
// Emitted to the pipeline trace under the `tool_search.*` namespace
// per Rust parity (pipeline/trace_helpers.rs).
type ToolSearchStats struct {
	// CandidatesConsidered is the total number of tools ranked
	// (including those without embeddings).
	CandidatesConsidered int

	// CandidatesSelected is the number of tools kept after pruning.
	CandidatesSelected int

	// CandidatesPruned is the number dropped (CandidatesConsidered
	// - CandidatesSelected).
	CandidatesPruned int

	// TokenSavings is the estimated difference between injecting
	// every ranked candidate vs the pruned set.
	TokenSavings int

	// TopScores is up to 10 (name, adjusted_score) pairs for the
	// selected tools, highest first. Useful for operator debugging
	// ("why did this tool get selected?"). Empty when embedding
	// was unavailable.
	TopScores []ScoredTool

	// EmbeddingStatus is "ok" when query+tool embeddings both
	// succeeded; "failed" when the embedding provider errored and
	// we fell back to static ordering. A persistent "failed"
	// signals an embedding-provider configuration problem.
	EmbeddingStatus string
}

// ScoredTool is a (name, score) tuple for TopScores reporting.
type ScoredTool struct {
	Name  string
	Score float64
}

// RankTools scores every tool in tools against queryEmbedding using
// cosine similarity, minus the MCP penalty for remote tools. Returns
// the full ranked list sorted by adjusted score descending.
//
// Tools with nil embeddings score 0.0 and sort to the bottom of the
// list (but aren't filtered — they're still candidates for
// always-include pinning). An empty queryEmbedding yields all-zero
// scores (caller should use AlwaysInclude to produce a useful set
// in that case).
//
// Does not mutate the input slice.
func RankTools(tools []*ToolDescriptor, queryEmbedding []float32, config ToolSearchConfig) []RankedTool {
	ranked := make([]RankedTool, 0, len(tools))
	for _, tool := range tools {
		var raw float64
		if len(tool.Embedding) > 0 && len(queryEmbedding) > 0 {
			raw = cosineSimilarity(tool.Embedding, queryEmbedding)
		}

		penalty := 0.0
		if tool.Source == ToolSourceMCP {
			penalty = config.MCPLatencyPenalty
		}

		adjusted := raw - penalty
		if adjusted < 0 {
			adjusted = 0
		}

		ranked = append(ranked, RankedTool{
			Descriptor:    tool,
			RawScore:      raw,
			AdjustedScore: adjusted,
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].AdjustedScore > ranked[j].AdjustedScore
	})
	return ranked
}

// SearchAndPrune is the full end-to-end pipeline: rank + pin + top-K
// within budget. Returns the pruned set (always-include pinned first,
// then top-K by adjusted score, cut off at TokenBudget) plus a
// ToolSearchStats for telemetry.
//
// Pinned (always-include) tools are guaranteed to appear in the
// output regardless of their score. They consume budget first; if
// the pinned set alone exceeds TokenBudget, it's still returned
// intact (budget is a soft cap for the rest, not a hard ceiling on
// pinned essentials).
//
// Rust parity: roboticus-agent/src/tool_search.rs::search_and_prune.
func SearchAndPrune(tools []*ToolDescriptor, queryEmbedding []float32, config ToolSearchConfig) ([]RankedTool, ToolSearchStats) {
	ranked := RankTools(tools, queryEmbedding, config)
	totalBefore := len(tools)

	alwaysSet := make(map[string]struct{}, len(config.AlwaysInclude))
	for _, name := range config.AlwaysInclude {
		alwaysSet[name] = struct{}{}
	}

	var pinned, rest []RankedTool
	for _, r := range ranked {
		if _, ok := alwaysSet[r.Descriptor.Name]; ok {
			pinned = append(pinned, r)
		} else {
			rest = append(rest, r)
		}
	}

	// Top-K with pinned-first budget allocation.
	selected := make([]RankedTool, 0, config.TopK)
	spent := 0
	for _, r := range pinned {
		selected = append(selected, r)
		spent += r.Descriptor.TokenCost
	}
	for _, r := range rest {
		if len(selected) >= config.TopK {
			break
		}
		if spent+r.Descriptor.TokenCost > config.TokenBudget {
			break
		}
		selected = append(selected, r)
		spent += r.Descriptor.TokenCost
	}

	// Telemetry.
	totalRankedTokens := 0
	for _, r := range ranked {
		totalRankedTokens += r.Descriptor.TokenCost
	}
	selectedTokens := 0
	for _, r := range selected {
		selectedTokens += r.Descriptor.TokenCost
	}

	topScores := make([]ScoredTool, 0, 10)
	for i, r := range selected {
		if i >= 10 {
			break
		}
		topScores = append(topScores, ScoredTool{
			Name:  r.Descriptor.Name,
			Score: r.AdjustedScore,
		})
	}

	status := "ok"
	if len(queryEmbedding) == 0 {
		status = "no_query_embedding"
	} else {
		anyEmbedded := false
		for _, t := range tools {
			if len(t.Embedding) > 0 {
				anyEmbedded = true
				break
			}
		}
		if !anyEmbedded {
			status = "no_tool_embeddings"
		}
	}

	stats := ToolSearchStats{
		CandidatesConsidered: totalBefore,
		CandidatesSelected:   len(selected),
		CandidatesPruned:     totalBefore - len(selected),
		TokenSavings:         totalRankedTokens - selectedTokens,
		TopScores:            topScores,
		EmbeddingStatus:      status,
	}

	return selected, stats
}

// cosineSimilarity computes the cosine similarity of two float32
// vectors. Returns 0 when either vector has zero norm (degenerate
// case — better to score equally-low than produce NaN).
//
// Matches Rust's cosine_similarity (tool_search.rs:139).
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
