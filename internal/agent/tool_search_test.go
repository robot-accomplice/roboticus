package agent

import "testing"

func TestToolSearchEngine_Ranking(t *testing.T) {
	e := NewToolSearchEngine(ToolSearchConfig{TopK: 5, TokenBudget: 1000})
	e.IndexTool("read_file", "Read the contents of a file from disk", "builtin")
	e.IndexTool("web_search", "Search the internet for information", "builtin")
	e.IndexTool("bash", "Execute a shell command", "builtin")

	tools := []ToolDescriptor{
		{Name: "read_file", Description: "Read the contents of a file", TokenCost: 100, Source: "builtin"},
		{Name: "web_search", Description: "Search the internet", TokenCost: 100, Source: "builtin"},
		{Name: "bash", Description: "Execute a shell command", TokenCost: 100, Source: "builtin"},
	}

	results, stats := e.Search("read the file config.yaml", tools)
	if len(results) == 0 {
		t.Fatal("should return results")
	}
	if stats.CandidatesConsidered != 3 {
		t.Errorf("candidates considered = %d, want 3", stats.CandidatesConsidered)
	}
	// read_file should rank highest for "read the file" query.
	if results[0].Name != "read_file" {
		t.Errorf("top result = %s, want read_file", results[0].Name)
	}
}

func TestToolSearchEngine_MCPPenalty(t *testing.T) {
	e := NewToolSearchEngine(ToolSearchConfig{TopK: 5, TokenBudget: 1000, MCPLatencyPenalty: 0.1})
	tools := []ToolDescriptor{
		{Name: "local_tool", Description: "A local tool for tasks", TokenCost: 100, Source: "builtin"},
		{Name: "mcp_tool", Description: "A local tool for tasks", TokenCost: 100, Source: "mcp"},
	}
	results, _ := e.Search("local tool for tasks", tools)
	if len(results) < 2 {
		t.Fatal("should return both tools")
	}
	// builtin should rank higher due to MCP penalty.
	if results[0].Source == "mcp" && results[1].Source == "builtin" {
		t.Error("MCP tool should be penalized below builtin")
	}
}

func TestToolSearchEngine_TokenBudget(t *testing.T) {
	e := NewToolSearchEngine(ToolSearchConfig{TopK: 10, TokenBudget: 250})
	tools := make([]ToolDescriptor, 5)
	for i := range tools {
		tools[i] = ToolDescriptor{
			Name: "tool_" + string(rune('a'+i)), Description: "A test tool",
			TokenCost: 100, Source: "builtin",
		}
	}
	results, stats := e.Search("test", tools)
	totalTokens := 0
	for _, r := range results {
		totalTokens += r.TokenCost
	}
	if totalTokens > 250 {
		t.Errorf("total tokens %d exceeds budget 250", totalTokens)
	}
	if stats.TokenSavings <= 0 {
		t.Error("should have token savings from pruning")
	}
}

func TestToolSearchEngine_PinnedTools(t *testing.T) {
	e := NewToolSearchEngine(ToolSearchConfig{
		TopK: 2, TokenBudget: 200, AlwaysInclude: []string{"delegate"},
	})
	tools := []ToolDescriptor{
		{Name: "tool_a", Description: "A high relevance tool", TokenCost: 100, Source: "builtin"},
		{Name: "tool_b", Description: "Another high relevance tool", TokenCost: 100, Source: "builtin"},
		{Name: "delegate", Description: "Delegate to specialist", TokenCost: 50, Source: "builtin"},
	}
	results, _ := e.Search("high relevance", tools)
	found := false
	for _, r := range results {
		if r.Name == "delegate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pinned tool 'delegate' should always be included")
	}
}
