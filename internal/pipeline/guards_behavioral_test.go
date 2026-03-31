package pipeline

import "testing"

func TestSubagentClaimGuard_NarratedDelegation(t *testing.T) {
	g := &SubagentClaimGuard{}
	ctx := &GuardContext{DelegationProvenance: DelegationProvenance{}}
	result := g.CheckWithContext("Let me delegate this to my specialist agent.", ctx)
	if result.Passed {
		t.Error("should reject narrated delegation without provenance")
	}
	if !result.Retry {
		t.Error("should request retry")
	}
}

func TestSubagentClaimGuard_CompletedProvenance(t *testing.T) {
	g := &SubagentClaimGuard{}
	ctx := &GuardContext{DelegationProvenance: DelegationProvenance{
		SubagentTaskStarted: true, SubagentTaskCompleted: true, SubagentResultAttached: true,
	}}
	result := g.CheckWithContext("Here are the results from the specialist.", ctx)
	if !result.Passed {
		t.Error("should pass with completed provenance")
	}
}

func TestTaskDeferralGuard_IntrospectionWithDeferral(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "get_memory_stats", Output: "{}"}},
	}
	result := g.CheckWithContext("Memory looks good. Let me check the other systems next.", ctx)
	if result.Passed {
		t.Error("should reject introspection-only turn with deferred action")
	}
}

func TestTaskDeferralGuard_RealToolUse(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "bash", Output: "done"}},
	}
	result := g.CheckWithContext("I ran the command. Let me check the output.", ctx)
	if !result.Passed {
		t.Error("should pass with real tool use")
	}
}

func TestInternalJargonGuard_InfraLeak(t *testing.T) {
	g := &InternalJargonGuard{}
	ctx := &GuardContext{}
	result := g.CheckWithContext("The decomposition gate decision was to proceed.", ctx)
	if result.Passed {
		t.Error("should detect infrastructure leak")
	}
}

func TestInternalJargonGuard_SubagentNameLeak(t *testing.T) {
	g := &InternalJargonGuard{}
	ctx := &GuardContext{SubagentNames: []string{"codereviewer", "researcher"}}
	result := g.CheckWithContext("The CodeReviewer agent found several issues.", ctx)
	if result.Passed {
		t.Error("should detect subagent name leak")
	}
}

func TestInternalJargonGuard_Clean(t *testing.T) {
	g := &InternalJargonGuard{}
	ctx := &GuardContext{SubagentNames: []string{"codereviewer"}}
	result := g.CheckWithContext("I found several issues in the code.", ctx)
	if !result.Passed {
		t.Error("clean response should pass")
	}
}

func TestDeclaredActionGuard_UnresolvedAction(t *testing.T) {
	g := &DeclaredActionGuard{}
	ctx := &GuardContext{UserPrompt: "I attack the goblin with my sword"}
	result := g.CheckWithContext("The goblin stands before you, looking menacing.", ctx)
	if result.Passed {
		t.Error("should detect unresolved declared action")
	}
}

func TestDeclaredActionGuard_ResolvedAction(t *testing.T) {
	g := &DeclaredActionGuard{}
	ctx := &GuardContext{UserPrompt: "I attack the goblin"}
	result := g.CheckWithContext("Roll for attack. You roll a d20 and get 17, hitting the goblin.", ctx)
	if !result.Passed {
		t.Error("resolved action should pass")
	}
}

func TestDeclaredActionGuard_NoAction(t *testing.T) {
	g := &DeclaredActionGuard{}
	ctx := &GuardContext{UserPrompt: "What's the weather like?"}
	result := g.CheckWithContext("It's sunny and 72 degrees.", ctx)
	if !result.Passed {
		t.Error("non-action prompt should pass")
	}
}
