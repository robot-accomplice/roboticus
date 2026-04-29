package llm

import "testing"

func TestRequestContextFootprint_AttributesRequestCategoriesAndDetails(t *testing.T) {
	req := &Request{
		ContextBudget: 200,
		Messages: []Message{
			{Role: "system", Content: "system prompt", ContextKind: ContextKindSystem},
			{Role: "system", Content: "active memory", ContextKind: ContextKindMemory},
			{Role: "system", Content: "memory index", ContextKind: ContextKindMemoryIndex},
			{Role: "system", Content: "runtime note", ContextKind: ContextKindAmbient},
			{Role: "assistant", Content: "prior assistant turn", ContextKind: ContextKindHistory},
			{Role: "user", Content: "latest user request", ContextKind: ContextKindCurrentUser},
			{Role: "system", Content: "continue the pending action", ContextKind: ContextKindExecutionOverlay},
		},
		Tools: []ToolDef{{
			Type:     "function",
			Function: ToolFuncDef{Name: "read_file", Description: "Read a file"},
		}},
	}

	fp := RequestContextFootprint(req)

	for _, key := range []string{
		ContextKindSystem,
		ContextKindMemory,
		ContextKindMemoryIndex,
		ContextKindAmbient,
		ContextKindHistory,
		ContextKindCurrentUser,
		ContextKindExecutionOverlay,
		ContextKindTools,
		ContextKindUnused,
	} {
		if fp.Categories[key] <= 0 {
			t.Fatalf("category %q tokens = %d, want > 0; all=%v", key, fp.Categories[key], fp.Categories)
		}
		if len(fp.Details[key]) == 0 {
			t.Fatalf("category %q details missing", key)
		}
	}
	if fp.TokenBudget != 200 {
		t.Fatalf("TokenBudget = %d, want 200", fp.TokenBudget)
	}
	if fp.UsedTokens <= 0 {
		t.Fatalf("UsedTokens = %d, want > 0", fp.UsedTokens)
	}
	if fp.OverheadTokens != fp.UsedTokens-fp.Categories[ContextKindCurrentUser] {
		t.Fatalf("OverheadTokens = %d, want used-current_user", fp.OverheadTokens)
	}
	if got := fp.Details[ContextKindTools][0].Name; got != "read_file" {
		t.Fatalf("tool detail name = %q, want read_file", got)
	}
}
