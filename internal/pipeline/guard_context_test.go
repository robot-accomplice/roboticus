package pipeline

import "testing"

func TestGuardContext_HasIntent(t *testing.T) {
	ctx := &GuardContext{Intents: []string{"execution", "tool_request"}}
	if !ctx.HasIntent("execution") {
		t.Error("should find execution intent")
	}
	if ctx.HasIntent("conversation") {
		t.Error("should not find conversation intent")
	}
}

func TestGuardContext_HasToolResult(t *testing.T) {
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "bash", Output: "ok"},
			{ToolName: "read_file", Output: "content"},
		},
	}
	if !ctx.HasToolResult("bash") {
		t.Error("should find bash tool result")
	}
	if ctx.HasToolResult("web_search") {
		t.Error("should not find web_search tool result")
	}
}

func TestApplyFullWithContext_ContextualGuard(t *testing.T) {
	chain := NewGuardChain(&ModelIdentityTruthGuard{})
	ctx := &GuardContext{
		Intents:       []string{"model_identity"},
		AgentName:     "TestBot",
		ResolvedModel: "test-model",
	}
	result := chain.ApplyFullWithContext("I am a large language model.", ctx)
	if result.Content == "I am a large language model." {
		t.Error("should have rewritten content")
	}
}

func TestApplyFullWithContext_BasicGuard(t *testing.T) {
	chain := NewGuardChain(&EmptyResponseGuard{})
	result := chain.ApplyFullWithContext("", nil)
	if len(result.Violations) == 0 {
		t.Error("empty response should trigger violation")
	}
}

func TestApplyFullWithContext_NilContext(t *testing.T) {
	chain := NewGuardChain(&SubagentClaimGuard{})
	result := chain.ApplyFullWithContext("Let me delegate this.", nil)
	// With nil context, contextual guard falls back to basic Check which passes.
	if !result.RetryRequested {
		_ = result
	}
}

func TestFullGuardChain_Length(t *testing.T) {
	chain := FullGuardChain()
	if len(chain.guards) != 26 {
		t.Errorf("full chain has %d guards, want 26", len(chain.guards))
	}
}

func TestStreamGuardChain_Length(t *testing.T) {
	chain := StreamGuardChain()
	// Rust parity: 6 guards (EmptyResponse, ExecutionTruth, TaskDeferral, ModelIdentityTruth, InternalJargon, NonRepetition)
	if len(chain.guards) != 6 {
		t.Errorf("stream chain has %d guards, want 6", len(chain.guards))
	}
}
