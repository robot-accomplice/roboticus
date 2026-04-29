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

func TestTaskDeferralGuard_RuntimeContextWithTestAssumptionDeferral(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "get_runtime_context", Output: `{"workspace":"/tmp/workspace"}`}},
	}
	result := g.CheckWithContext("Let me test that assumption right now.", ctx)
	if result.Passed {
		t.Fatal("should reject promissory final answer after runtime-context-only evidence")
	}
	if !result.Retry {
		t.Fatalf("expected retry for promissory final answer, got %#v", result)
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

func TestTaskDeferralGuard_SchedulingPromptRequiresCronEvidence(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "schedule a cron job that runs every 5 minutes and tell me exactly what was scheduled",
		SelectedToolNames: []string{"cron", "get_runtime_context"},
		ToolResults: []ToolResultEntry{
			{ToolName: "get_runtime_context", Output: `{"workspace":"/tmp/workspace"}`},
			{ToolName: "recall_memory", Output: "no relevant prior memory"},
		},
	}
	result := g.CheckWithContext("You can add this cron syntax: */5 * * * *. Please confirm if you want me to proceed.", ctx)
	if result.Passed {
		t.Fatal("scheduling task should reject generic cron advice without cron tool evidence")
	}
	if !result.Retry {
		t.Fatalf("expected retry for missing scheduling evidence, got %#v", result)
	}
}

func TestTaskDeferralGuard_SchedulingPromptAllowsCronEvidence(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "schedule a cron job that runs every 5 minutes and tell me exactly what was scheduled",
		SelectedToolNames: []string{"cron", "get_runtime_context"},
		ToolResults: []ToolResultEntry{
			{ToolName: "cron", Output: `Created cron job "quiet ticker" (id=job-1, schedule=*/5 * * * *, delivery=session/default)`},
		},
	}
	result := g.CheckWithContext("Created the quiet ticker cron job with schedule */5 * * * *.", ctx)
	if !result.Passed {
		t.Fatalf("cron tool evidence should satisfy scheduling contract, got reason %q", result.Reason)
	}
}

func TestTaskDeferralGuard_SchedulingAliasDefinitionIsContextSettingNotCronExecution(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "For the rest of this session, 'quiet ticker' means a cron job that runs every 5 minutes. Reply only with noted.",
		SelectedToolNames: []string{"cron", "get_runtime_context"},
	}
	result := g.CheckWithContext("noted", ctx)
	if !result.Passed {
		t.Fatalf("context-setting alias definition should not require cron evidence, got reason %q", result.Reason)
	}
}

func TestTaskDeferralGuard_GenericToolUseRequiresActionToolEvidence(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Pick an enabled runtime tool relevant to security and use it.",
		SelectedToolNames: []string{"list-available-skills", "ghola"},
		ToolResults: []ToolResultEntry{
			{ToolName: "list-available-skills", Output: "ClawdStrike: security analysis"},
		},
	}
	result := g.CheckWithContext("ClawdStrike is the relevant tool. Please confirm if you want me to run it.", ctx)
	if result.Passed {
		t.Fatal("tool-use prompt should reject skill discovery without action-tool evidence")
	}
}

func TestTaskDeferralGuard_GenericToolUseRejectsRuntimeInventoryEvidence(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Tell me about the tools you can use, pick one at random, and use it.",
		SelectedToolNames: []string{"list-available-skills", "get_subagent_status", "ghola"},
		ToolResults: []ToolResultEntry{
			{ToolName: "list-available-skills", Output: "ghola: web retrieval"},
			{ToolName: "get_subagent_status", Output: "4 idle subagents"},
		},
	}
	result := g.CheckWithContext("I listed the tools and checked subagent status.", ctx)
	if result.Passed {
		t.Fatal("runtime inventory/status tools should not satisfy a generic use-a-tool request")
	}
}

func TestTaskDeferralGuard_GenericToolUseRequiresFinalObservedToolResult(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Tell me about the tools you can use, pick one at random, and use it.",
		SelectedToolNames: []string{"list-available-skills", "inventory_projects"},
		ToolResults: []ToolResultEntry{
			{ToolName: "list-available-skills", Output: "workspace skill catalog"},
			{ToolName: "inventory_projects", Output: `{"project_count":1,"projects":[{"name":"Vault"}]}`},
		},
	}
	result := g.CheckWithContext("I will randomly choose code-analysis-bug-hunting and proceed to use it.", ctx)
	if result.Passed {
		t.Fatal("generic tool-use prompt should reject final answers that ignore the executed tool result")
	}
}

func TestTaskDeferralGuard_GenericToolUseAcceptsReportedObservedToolResult(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Tell me about the tools you can use, pick one at random, and use it.",
		SelectedToolNames: []string{"list-available-skills", "inventory_projects"},
		ToolResults: []ToolResultEntry{
			{ToolName: "list-available-skills", Output: "workspace skill catalog"},
			{ToolName: "inventory_projects", Output: `{"project_count":1,"projects":[{"name":"Vault"}]}`},
		},
	}
	result := g.CheckWithContext("I used inventory_projects to inspect the workspace project inventory and found one project: Vault.", ctx)
	if !result.Passed {
		t.Fatalf("reported non-inventory tool result should satisfy generic tool-use prompt, got reason %q", result.Reason)
	}
}

func TestTaskDeferralGuard_GenericToolUseRejectsOpenEndedTailAfterAction(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Tell me about the tools you can use, pick one at random, and use it.",
		SelectedToolNames: []string{"list_directory", "get_runtime_context"},
		ToolResults: []ToolResultEntry{
			{ToolName: "list_directory", Output: "ARCHITECTURE.md\ndocs/"},
		},
	}
	result := g.CheckWithContext("I used list_directory and found ARCHITECTURE.md and docs/. If you need more, let me know.", ctx)
	if result.Passed {
		t.Fatal("generic tool-use prompt should reject open-ended follow-up offers after successful tool execution")
	}
}

func TestTaskDeferralGuard_GenericToolUseRejectsFutureTenseFinalizationAfterAction(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Tell me about the tools you can use, pick one at random, and use it.",
		SelectedToolNames: []string{"read_file", "glob_files"},
		ToolResults: []ToolResultEntry{
			{ToolName: "read_file", Output: "Target docs dir set to /Users/jmachen/code/roboticus/docs"},
		},
	}
	result := g.CheckWithContext("I used read_file and observed the target docs directory. I will finalize this task now.", ctx)
	if result.Passed {
		t.Fatal("generic tool-use prompt should reject future-tense finalization after successful tool execution")
	}
}

func TestTaskDeferralGuard_ResolvedFolderScanRequiresInspectionEvidence(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Look in ~/Downloads and tell me the newest PDF file.",
		SelectedToolNames: []string{"list_directory", "glob_files", "read_file"},
	}
	result := g.CheckWithContext("Should I use your default Downloads directory for this?", ctx)
	if result.Passed {
		t.Fatal("resolved folder scan should not ask for confirmation instead of using selected inspection tools")
	}
}

func TestTaskDeferralGuard_DownloadsFolderAliasRequiresInspectionEvidence(t *testing.T) {
	g := &TaskDeferralGuard{}
	ctx := &GuardContext{
		UserPrompt:        "Now look in my Downloads folder",
		SelectedToolNames: []string{"list_directory", "glob_files", "read_file"},
	}
	result := g.CheckWithContext("Could you please confirm the exact path to your Downloads folder?", ctx)
	if result.Passed {
		t.Fatal("allowed common-folder alias should require inspection evidence instead of confirmation")
	}
}

func TestClarificationDeflectionGuard_CannedRestatementRequest(t *testing.T) {
	g := &ClarificationDeflectionGuard{}
	ctx := &GuardContext{
		UserPrompt: "Rewrite my previous response so it sounds natural and avoids repetition.",
	}
	result := g.CheckWithContext(`I understand. You need me to address the conversation flow in a more natural and context-aware way, avoiding direct repetition or circular responses.

Please provide the last message or the context you want me to respond to, and I will generate a revised, non-repetitive answer.`, ctx)
	if result.Passed {
		t.Error("should reject canned request to restate already-provided context")
	}
	if !result.Retry {
		t.Error("should request retry")
	}
}

func TestClarificationDeflectionGuard_TargetedClarificationPasses(t *testing.T) {
	g := &ClarificationDeflectionGuard{}
	ctx := &GuardContext{
		UserPrompt: "Help me debug the test failure.",
	}
	result := g.CheckWithContext("Which test is failing, and what error are you seeing?", ctx)
	if !result.Passed {
		t.Error("targeted clarification should pass")
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
