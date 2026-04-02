package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"goboticus/internal/core"
)

func TestToolPruner_UnderBudget(t *testing.T) {
	tools := []ToolDef{
		{Name: "echo", Description: "echoes input", RiskLevel: core.RiskLevelSafe},
		{Name: "web_search", Description: "searches the web", RiskLevel: core.RiskLevelSafe},
	}
	pruner := NewToolPruner(10000, nil) // large budget

	result := pruner.Prune(context.Background(), tools, "hello", nil)
	if len(result) != 2 {
		t.Errorf("got %d tools, want 2 (all under budget)", len(result))
	}
}

func TestToolPruner_OverBudget(t *testing.T) {
	tools := make([]ToolDef, 20)
	for i := range tools {
		tools[i] = ToolDef{
			Name:           fmt.Sprintf("tool_%d", i),
			Description:    strings.Repeat("description ", 20),
			ParametersJSON: `{"type":"object"}`,
			RiskLevel:      core.RiskLevelSafe,
		}
	}
	pruner := NewToolPruner(500, nil) // small budget forces pruning

	result := pruner.Prune(context.Background(), tools, "query", nil)
	if len(result) >= 20 {
		t.Error("expected tools to be pruned")
	}

	// Verify total tokens are within budget
	total := 0
	for _, td := range result {
		total += td.EstimateTokens()
	}
	if total > 500 {
		t.Errorf("total tokens %d exceeds budget 500", total)
	}
}

func TestToolPruner_PreservesSessionTools(t *testing.T) {
	tools := []ToolDef{
		{Name: "rarely_used", Description: strings.Repeat("x", 200), RiskLevel: core.RiskLevelSafe},
		{Name: "session_tool", Description: strings.Repeat("x", 200), RiskLevel: core.RiskLevelSafe},
		{Name: "another_tool", Description: strings.Repeat("x", 200), RiskLevel: core.RiskLevelSafe},
	}
	sessionTools := []string{"session_tool"}

	// Budget only allows ~1-2 tools
	pruner := NewToolPruner(150, nil)
	result := pruner.Prune(context.Background(), tools, "query", sessionTools)

	found := false
	for _, td := range result {
		if td.Name == "session_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("session tool should always be preserved")
	}
}

func TestToolPruner_EmptyList(t *testing.T) {
	pruner := NewToolPruner(1000, nil)
	result := pruner.Prune(context.Background(), nil, "query", nil)
	if len(result) != 0 {
		t.Errorf("got %d tools, want 0", len(result))
	}
}
