package agent

import (
	"testing"

	"roboticus/internal/llm"
)

func TestContextBuilder_TagsFinalRequestFootprintCategories(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.BudgetConfig = nil
	cfg.MaxTokens = 256
	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt("system prompt")
	cb.SetMemory("active memory block")
	cb.SetMemoryIndex("memory index block")
	cb.SetTools([]llm.ToolDef{{
		Type:     "function",
		Function: llm.ToolFuncDef{Name: "search_memories", Description: "Search memories"},
	}})
	cb.AppendSystemNote("runtime note")

	sess := NewSession("s1", "agent", "test")
	sess.AddUserMessage("first request")
	sess.AddAssistantMessage("first response", nil)
	sess.AddUserMessage("current request")
	sess.SetTurnExecutionNote("User follow-up is a pending-action continuation.")

	req := cb.BuildRequest(sess)
	fp := llm.RequestContextFootprint(req)

	for _, key := range []string{
		llm.ContextKindSystem,
		llm.ContextKindMemory,
		llm.ContextKindMemoryIndex,
		llm.ContextKindAmbient,
		llm.ContextKindHistory,
		llm.ContextKindCurrentUser,
		llm.ContextKindExecutionOverlay,
		llm.ContextKindTools,
	} {
		if fp.Categories[key] <= 0 {
			t.Fatalf("footprint category %q = %d, want > 0; categories=%v", key, fp.Categories[key], fp.Categories)
		}
	}
	if req.ContextBudget != 256 {
		t.Fatalf("ContextBudget = %d, want 256", req.ContextBudget)
	}
}
