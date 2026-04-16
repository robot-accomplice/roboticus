package tools

import (
	"math"
	"testing"
)

// TestRankTools_SortsByAdjustedScore is the baseline ranking test.
// Two tools, one better matching the query; the better match must
// appear first in the ranked output.
func TestRankTools_SortsByAdjustedScore(t *testing.T) {
	tools := []*ToolDescriptor{
		{
			Name:        "web_search",
			Description: "Search the web",
			TokenCost:   50,
			Source:      ToolSourceBuiltIn,
			Embedding:   []float32{0.9, 0.1, 0.0},
		},
		{
			Name:        "memory_store",
			Description: "Store a memory",
			TokenCost:   30,
			Source:      ToolSourceBuiltIn,
			Embedding:   []float32{0.1, 0.9, 0.0},
		},
	}
	query := []float32{0.85, 0.15, 0.0}

	ranked := RankTools(tools, query, DefaultToolSearchConfig())
	if len(ranked) != 2 {
		t.Fatalf("want 2 ranked tools; got %d", len(ranked))
	}
	if ranked[0].Descriptor.Name != "web_search" {
		t.Fatalf("highest-similarity tool should rank first; got %q", ranked[0].Descriptor.Name)
	}
}

// TestRankTools_MCPPenaltyBreaksTies pins the v1.0.6 MCP latency
// discipline: when an MCP tool and a local tool have identical
// semantic match, the local tool wins because MCP incurs a
// round-trip penalty.
func TestRankTools_MCPPenaltyBreaksTies(t *testing.T) {
	tools := []*ToolDescriptor{
		{
			Name:      "local_tool",
			Source:    ToolSourceBuiltIn,
			Embedding: []float32{0.9, 0.1},
			TokenCost: 50,
		},
		{
			Name:      "remote_tool",
			Source:    ToolSourceMCP,
			MCPServer: "someserver",
			Embedding: []float32{0.9, 0.1},
			TokenCost: 50,
		},
	}
	query := []float32{0.9, 0.1}

	cfg := DefaultToolSearchConfig()
	cfg.MCPLatencyPenalty = 0.1
	ranked := RankTools(tools, query, cfg)

	if ranked[0].Descriptor.Name != "local_tool" {
		t.Fatalf("MCP penalty should have ranked local_tool first; got %q", ranked[0].Descriptor.Name)
	}
	// Adjusted score of MCP tool should be raw-penalty, floored at 0.
	if math.Abs(ranked[1].AdjustedScore-(ranked[1].RawScore-0.1)) > 1e-9 {
		t.Fatalf("MCP adjusted score should equal raw - penalty; got raw=%v adjusted=%v", ranked[1].RawScore, ranked[1].AdjustedScore)
	}
}

// TestRankTools_NoEmbeddingsScoresZero guards the graceful-degradation
// case: tools without cached embeddings still appear in the ranked
// output (so always-include pinning can pick them up), but score 0.0
// so any embedded tool ranks above them.
func TestRankTools_NoEmbeddingsScoresZero(t *testing.T) {
	tools := []*ToolDescriptor{
		{Name: "no_embedding", Source: ToolSourceBuiltIn, TokenCost: 50},
	}
	ranked := RankTools(tools, []float32{0.9, 0.1}, DefaultToolSearchConfig())
	if len(ranked) != 1 {
		t.Fatalf("want 1 ranked tool; got %d", len(ranked))
	}
	if ranked[0].AdjustedScore != 0 {
		t.Fatalf("tool without embedding should score 0; got %v", ranked[0].AdjustedScore)
	}
}

// TestSearchAndPrune_PinsAlwaysInclude is the v1.0.6 Rust-parity
// contract: tools in AlwaysInclude MUST appear in the output even
// if they'd rank below the TopK cutoff on semantic score alone.
// Matches Rust's top_k_with_pinned.
func TestSearchAndPrune_PinsAlwaysInclude(t *testing.T) {
	tools := []*ToolDescriptor{
		{Name: "irrelevant_a", Source: ToolSourceBuiltIn, Embedding: []float32{0.9, 0.1}, TokenCost: 50},
		{Name: "irrelevant_b", Source: ToolSourceBuiltIn, Embedding: []float32{0.9, 0.1}, TokenCost: 50},
		{Name: "memory_store", Source: ToolSourceBuiltIn, Embedding: []float32{0.0, 0.0}, TokenCost: 30},
	}
	query := []float32{0.9, 0.1}

	cfg := DefaultToolSearchConfig()
	cfg.TopK = 2 // pinning would otherwise be pushed out
	cfg.AlwaysInclude = []string{"memory_store"}

	selected, stats := SearchAndPrune(tools, query, cfg)
	names := extractNames(selected)
	if !containsName(names, "memory_store") {
		t.Fatalf("memory_store must be pinned; got selection %v", names)
	}
	if stats.CandidatesConsidered != 3 {
		t.Fatalf("stats should count all 3 candidates considered; got %d", stats.CandidatesConsidered)
	}
}

// TestSearchAndPrune_BudgetEnforcement pins the token-budget contract:
// pinned tools consume budget first, then the top-scoring rest fill
// until either TopK or TokenBudget is reached.
func TestSearchAndPrune_BudgetEnforcement(t *testing.T) {
	tools := []*ToolDescriptor{
		{Name: "cheap", Source: ToolSourceBuiltIn, Embedding: []float32{0.9, 0.1}, TokenCost: 10},
		{Name: "medium", Source: ToolSourceBuiltIn, Embedding: []float32{0.8, 0.2}, TokenCost: 50},
		{Name: "expensive", Source: ToolSourceBuiltIn, Embedding: []float32{0.7, 0.3}, TokenCost: 500},
	}
	query := []float32{0.9, 0.1}

	cfg := DefaultToolSearchConfig()
	cfg.TopK = 10
	cfg.TokenBudget = 100 // fits cheap + medium, NOT expensive
	cfg.AlwaysInclude = nil

	selected, _ := SearchAndPrune(tools, query, cfg)
	names := extractNames(selected)
	if containsName(names, "expensive") {
		t.Fatalf("budget should have excluded 'expensive' (cost 500 > remaining %d); got %v", cfg.TokenBudget, names)
	}
	if !containsName(names, "cheap") || !containsName(names, "medium") {
		t.Fatalf("both 'cheap' and 'medium' should fit; got %v", names)
	}
}

// TestSearchAndPrune_StatsReportCorrectly confirms the telemetry
// surface matches Rust's ToolSearchStats. Operators reading trace
// annotations rely on these numbers being accurate.
func TestSearchAndPrune_StatsReportCorrectly(t *testing.T) {
	tools := []*ToolDescriptor{
		{Name: "kept", Source: ToolSourceBuiltIn, Embedding: []float32{0.9, 0.1}, TokenCost: 50},
		{Name: "pruned_1", Source: ToolSourceBuiltIn, Embedding: []float32{0.1, 0.9}, TokenCost: 50},
		{Name: "pruned_2", Source: ToolSourceBuiltIn, Embedding: []float32{0.0, 1.0}, TokenCost: 50},
	}
	query := []float32{0.9, 0.1}

	cfg := DefaultToolSearchConfig()
	cfg.TopK = 1
	cfg.TokenBudget = 10_000
	cfg.AlwaysInclude = nil

	_, stats := SearchAndPrune(tools, query, cfg)
	if stats.CandidatesConsidered != 3 {
		t.Fatalf("considered = %d; want 3", stats.CandidatesConsidered)
	}
	if stats.CandidatesSelected != 1 {
		t.Fatalf("selected = %d; want 1", stats.CandidatesSelected)
	}
	if stats.CandidatesPruned != 2 {
		t.Fatalf("pruned = %d; want 2", stats.CandidatesPruned)
	}
	if stats.TokenSavings != 100 {
		t.Fatalf("token savings = %d; want 100 (50+50 pruned)", stats.TokenSavings)
	}
	if stats.EmbeddingStatus != "ok" {
		t.Fatalf("status = %q; want 'ok'", stats.EmbeddingStatus)
	}
}

// TestSearchAndPrune_ReportsNoQueryEmbeddingStatus is the graceful-
// degradation signal: when the orchestrator couldn't produce a query
// embedding (embedding provider down, empty query, etc.), stats
// should flag "no_query_embedding" so operators can diagnose why
// tool selection looks uniformly zero-scored.
func TestSearchAndPrune_ReportsNoQueryEmbeddingStatus(t *testing.T) {
	tools := []*ToolDescriptor{
		{Name: "a", Source: ToolSourceBuiltIn, Embedding: []float32{0.9, 0.1}, TokenCost: 50},
	}
	// Empty query embedding.
	_, stats := SearchAndPrune(tools, nil, DefaultToolSearchConfig())
	if stats.EmbeddingStatus != "no_query_embedding" {
		t.Fatalf("status = %q; want 'no_query_embedding' when query embedding is empty", stats.EmbeddingStatus)
	}
}

// TestDefaultToolSearchConfig_MatchesRustParity freezes the Rust-
// parity defaults. If a future edit changes these, it should be a
// deliberate choice that breaks the test, not an accidental drift.
//
// The ranking knobs (TopK/TokenBudget/MCPLatencyPenalty) mirror Rust's
// SearchConfig::default in crates/roboticus-agent/src/tool_search.rs.
//
// AlwaysInclude is deliberately NOT Rust's SearchConfig::default value
// (`["memory_store", "delegate"]`) — that Rust default is an agent-crate
// test fixture, not the runtime pin list. Rust's runtime owner
// (crates/roboticus-pipeline/src/core/tool_prune.rs) overrides it with
// `always_include_operational_tools()` — a 12-item list of subagent,
// task-lifecycle, skill-management, and memory-write tools. Of those 12,
// only two (`get_memory_stats`, `get_runtime_context`) have matching
// names in Go's registry; the rest are a separate tool-surface parity
// gap tracked in System 02 and deferred to a later remediation.
//
// Go's AlwaysInclude is the functional analogue: memory-recall
// primitives that Go refined beyond the Rust baseline (per System 03
// SYS-03-005 — `recall_memory` with richer lookup and `search_memories`
// are recorded as Go improvements), plus the two Rust-parity
// introspection tools, plus `get_subagent_status` (the closest Go
// analogue to Rust's `list-subagent-roster`). The list pins only names
// that are actually registered, so every pin is guaranteed to survive
// ranking — there are no silent no-op pins.
func TestDefaultToolSearchConfig_MatchesRustParity(t *testing.T) {
	cfg := DefaultToolSearchConfig()
	if cfg.TopK != 15 {
		t.Fatalf("TopK = %d; want 15 (Rust parity)", cfg.TopK)
	}
	if cfg.TokenBudget != 4000 {
		t.Fatalf("TokenBudget = %d; want 4000 (Rust parity)", cfg.TokenBudget)
	}
	if cfg.MCPLatencyPenalty != 0.05 {
		t.Fatalf("MCPLatencyPenalty = %v; want 0.05 (Rust parity)", cfg.MCPLatencyPenalty)
	}
	wantIncludes := map[string]bool{
		"recall_memory":       true, // Go improvement (SYS-03-005)
		"search_memories":     true, // Go improvement (SYS-03-005)
		"get_memory_stats":    true, // Rust parity
		"get_runtime_context": true, // Rust parity
		"get_subagent_status": true, // functional analogue of Rust `list-subagent-roster`
	}
	if len(cfg.AlwaysInclude) != len(wantIncludes) {
		t.Fatalf("AlwaysInclude = %v; want %d entries: %v", cfg.AlwaysInclude, len(wantIncludes), wantIncludes)
	}
	for _, name := range cfg.AlwaysInclude {
		if !wantIncludes[name] {
			t.Fatalf("AlwaysInclude contains unexpected %q; want one of %v", name, wantIncludes)
		}
	}
}

// TestCosineSimilarity_EdgeCases pins degenerate-input behavior:
// mismatched lengths, zero vectors, identical vectors, opposite
// vectors. Ranking correctness depends on all of these being
// predictable.
func TestCosineSimilarity_EdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		a, b    []float32
		wantMin float64
		wantMax float64
	}{
		{"empty", []float32{}, []float32{}, 0, 0},
		{"mismatched length", []float32{1, 0}, []float32{1, 0, 0}, 0, 0},
		{"zero vector a", []float32{0, 0}, []float32{1, 0}, 0, 0},
		{"zero vector b", []float32{1, 0}, []float32{0, 0}, 0, 0},
		{"identical unit", []float32{1, 0, 0}, []float32{1, 0, 0}, 1, 1},
		{"opposite unit", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1, -1},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineSimilarity(tc.a, tc.b)
			if got < tc.wantMin-1e-9 || got > tc.wantMax+1e-9 {
				t.Fatalf("cosine(%v, %v) = %v; want in [%v, %v]", tc.a, tc.b, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// Helpers for tests.

func extractNames(ranked []RankedTool) []string {
	names := make([]string, len(ranked))
	for i, r := range ranked {
		names[i] = r.Descriptor.Name
	}
	return names
}

func containsName(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
