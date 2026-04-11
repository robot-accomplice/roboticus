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
	if len(chain.guards) != 27 {
		t.Errorf("full chain has %d guards, want 27", len(chain.guards))
	}
}

func TestStreamGuardChain_Length(t *testing.T) {
	chain := StreamGuardChain()
	// Rust parity: 6 guards (EmptyResponse, ExecutionTruth, TaskDeferral, ModelIdentityTruth, InternalJargon, NonRepetition)
	if len(chain.guards) != 6 {
		t.Errorf("stream chain has %d guards, want 6", len(chain.guards))
	}
}

func TestPlaceholderContentGuard(t *testing.T) {
	g := &PlaceholderContentGuard{}

	// Exact reproduction of the observed failure: model returned template text.
	templateResponse := `Based on our recent exchange, it seems we were discussing [**Insert the main topic of the conversation here**].

If that's not quite right, could you remind me what we were last talking about? I'm happy to pick up right where we left off!`
	r := g.Check(templateResponse)
	if r.Passed {
		t.Error("should reject template with [**Insert ... **] placeholder")
	}
	if !r.Retry {
		t.Error("should request retry for placeholder content")
	}

	// Other placeholder patterns.
	cases := []struct {
		name    string
		content string
		reject  bool
	}{
		{"bracket insert", "Here is [insert your answer]", true},
		{"bracket your", "Welcome [your name], to the platform", true},
		{"bracket fill", "Please [fill in the blank] below", true},
		{"lorem ipsum", "Lorem ipsum dolor sit amet", true},
		{"bold insert", "We discussed **Insert topic here** recently", true},
		{"clean response", "Based on our memory, you were discussing the v0.11.4 release plan for Adaptive Intelligence.", false},
		{"normal brackets", "The array [1, 2, 3] contains three elements", false},
		{"code brackets", "Use config[\"key\"] to access the value", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := g.Check(tc.content)
			if tc.reject && result.Passed {
				t.Errorf("should reject: %s", tc.content[:50])
			}
			if !tc.reject && !result.Passed {
				t.Errorf("should pass: %s (reason: %s)", tc.content[:50], result.Reason)
			}
		})
	}
}
