package pipeline

import (
	"context"
	"testing"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/llm"
	"roboticus/internal/session"
)

func TestDeriveTurnEnvelopePolicy_LightweightConversationalTurn(t *testing.T) {
	policy := DeriveTurnEnvelopePolicy("Greetings, Duncan.", TaskSynthesis{
		Intent:          "conversational",
		Complexity:      "simple",
		RetrievalNeeded: false,
	}, 2)
	if policy.Weight != TurnWeightLight {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightLight)
	}
	if policy.AllowRetrieval {
		t.Fatal("lightweight policy should disable retrieval on first pass")
	}
	if !policy.LightweightToolSurface {
		t.Fatal("lightweight policy should suppress tools on first pass")
	}
}

func TestDeriveTurnEnvelopePolicy_LightweightColloquialGreeting(t *testing.T) {
	policy := DeriveTurnEnvelopePolicy("What's the good word?", TaskSynthesis{
		Intent:          "conversational",
		Complexity:      "simple",
		RetrievalNeeded: false,
	}, 2)
	if policy.Weight != TurnWeightLight {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightLight)
	}
	if policy.AllowRetrieval {
		t.Fatal("colloquial greeting should not enable retrieval on first pass")
	}
}

func TestDeriveTurnEnvelopePolicy_LightweightWhatsNewGreeting(t *testing.T) {
	policy := DeriveTurnEnvelopePolicy("What's new, Duncan?", TaskSynthesis{
		Intent:          "conversational",
		Complexity:      "simple",
		RetrievalNeeded: false,
	}, 2)
	if policy.Weight != TurnWeightLight {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightLight)
	}
	if policy.AllowRetrieval {
		t.Fatal("what's-new greeting should not enable retrieval on first pass")
	}
	if !policy.LightweightToolSurface {
		t.Fatal("what's-new greeting should suppress tools on first pass")
	}
}

func TestDeriveTurnEnvelopePolicy_SimpleDirectTaskUsesFocusedEnvelope(t *testing.T) {
	policy := DeriveTurnEnvelopePolicy("Create a new Obsidian note for today's meeting.", TaskSynthesis{
		Intent:          "task",
		Complexity:      "simple",
		PlannedAction:   "execute_directly",
		RetrievalNeeded: false,
	}, 2)
	if policy.Weight != TurnWeightStandard {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightStandard)
	}
	if policy.AllowRetrieval {
		t.Fatal("simple direct task should not enable retrieval by default")
	}
	if policy.LightweightToolSurface {
		t.Fatal("simple direct task should retain a focused tool surface, not suppress tools entirely")
	}
	if policy.MaxTools != 6 {
		t.Fatalf("max tools = %d, want 6", policy.MaxTools)
	}
	if !policy.RequireArtifactWrite {
		t.Fatal("simple direct vault authoring should require artifact-writing proof")
	}
	if policy.AllowAuthorityMutation {
		t.Fatal("simple direct vault authoring should not expose authority mutation tools")
	}
	if policy.ToolProfile != ToolProfileFocusedAuthoring {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedAuthoring)
	}
}

func TestDeriveTurnEnvelopePolicy_VerboseSingleStepAuthoringStillUsesFocusedEnvelope(t *testing.T) {
	prompt := "Create a new Obsidian note named codex-live-test-2.md in the vault containing exactly: # Codex Live Test 2. Use the Obsidian vault tool if you have it. Do not ask for confirmation."
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if synthesis.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", synthesis.Complexity)
	}
	if synthesis.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", synthesis.PlannedAction)
	}
	if policy.Weight != TurnWeightStandard {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightStandard)
	}
	if policy.AllowRetrieval {
		t.Fatal("verbose single-step authoring should not enable retrieval")
	}
	if policy.MaxTools != 6 {
		t.Fatalf("max tools = %d, want 6", policy.MaxTools)
	}
	if !policy.RequireArtifactWrite {
		t.Fatal("verbose single-step authoring should still require artifact-writing proof")
	}
	if policy.AllowAuthorityMutation {
		t.Fatal("verbose single-step authoring should not allow authority mutation")
	}
	if policy.ToolProfile != ToolProfileFocusedAuthoring {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedAuthoring)
	}
}

func TestDeriveTurnEnvelopePolicy_BoundedMultiArtifactAuthoringUsesFocusedEnvelope(t *testing.T) {
	prompt := "In the Obsidian vault, create two notes: project-bootstrap-check.md and project-bootstrap-actions.md. The first note should contain exactly: # Project Bootstrap Check. The second note should contain exactly: # Project Bootstrap Actions."
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if synthesis.Complexity != "moderate" {
		t.Fatalf("complexity = %q, want moderate", synthesis.Complexity)
	}
	if synthesis.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", synthesis.PlannedAction)
	}
	if policy.Weight != TurnWeightStandard {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightStandard)
	}
	if policy.AllowRetrieval {
		t.Fatal("bounded multi-artifact authoring should not enable retrieval")
	}
	if policy.MaxTools != 6 {
		t.Fatalf("max tools = %d, want 6", policy.MaxTools)
	}
	if !policy.RequireArtifactWrite {
		t.Fatal("bounded multi-artifact authoring should still require artifact-writing proof")
	}
	if policy.AllowAuthorityMutation {
		t.Fatal("bounded multi-artifact authoring should not allow authority mutation")
	}
	if policy.ToolProfile != ToolProfileFocusedAuthoring {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedAuthoring)
	}
}

func TestDeriveTurnEnvelopePolicy_SchedulingTurnUsesFocusedSchedulingEnvelope(t *testing.T) {
	policy := DeriveTurnEnvelopePolicy("schedule a cron job that runs every 5 minutes and tell me exactly what was scheduled", TaskSynthesis{
		Intent:          "task",
		Complexity:      "simple",
		PlannedAction:   "execute_directly",
		RetrievalNeeded: false,
	}, 2)
	if policy.Weight != TurnWeightStandard {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightStandard)
	}
	if policy.ToolProfile != ToolProfileFocusedScheduling {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedScheduling)
	}
	if policy.MaxTools != 4 {
		t.Fatalf("max tools = %d, want 4", policy.MaxTools)
	}
	if policy.AllowRetrieval {
		t.Fatal("focused scheduling turns should not enable retrieval by default")
	}
}

func TestDeriveTurnEnvelopePolicy_SchedulingAliasFollowupStaysFocusedSchedulingEnvelope(t *testing.T) {
	prompt := "Create the quiet ticker now and tell me exactly what was scheduled."
	synthesis := SynthesizeTaskState(prompt, 5, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 5)
	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if policy.ToolProfile != ToolProfileFocusedScheduling {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedScheduling)
	}
}

func TestDeriveTurnEnvelopePolicy_FilesystemInspectionUsesFocusedInspectionEnvelope(t *testing.T) {
	policy := DeriveTurnEnvelopePolicy("Count markdown files recursively in the target docs dir and return only the number.", TaskSynthesis{
		Intent:          "task",
		Complexity:      "simple",
		PlannedAction:   "execute_directly",
		RetrievalNeeded: false,
	}, 2)
	if policy.Weight != TurnWeightStandard {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightStandard)
	}
	if policy.ToolProfile != ToolProfileFocusedInspection {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedInspection)
	}
	if policy.AllowRetrieval {
		t.Fatal("focused inspection turns should not enable retrieval by default")
	}
}

func TestDeriveTurnEnvelopePolicy_InspectionQuestionUsesFocusedInspectionEnvelope(t *testing.T) {
	prompt := "What's in your vault right now?"
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if policy.ToolProfile != ToolProfileFocusedInspection {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedInspection)
	}
	if policy.AllowRetrieval {
		t.Fatal("inspection-shaped questions should not enable retrieval by default")
	}
}

func TestDeriveTurnEnvelopePolicy_PathProjectListingUsesFocusedInspectionEnvelope(t *testing.T) {
	prompt := "What about a list of the projects in /Users/jmachen/code?"
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if policy.ToolProfile != ToolProfileFocusedInspection {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedInspection)
	}
	if policy.AllowRetrieval {
		t.Fatal("path-shaped project listing should not enable retrieval by default")
	}
}

func TestDeriveTurnEnvelopePolicy_ComplexRepoArchitectureReviewUsesFocusedInspectionEnvelope(t *testing.T) {
	prompt := "Please review all of the subdirectories associated with the project at ~/code/roboticus and try to locate the architecture documentation. When you find it, review that documentation and compare it directly with the code. Then provide me with a summary of the alignment between architecture documentation and code implementation."
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if synthesis.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", synthesis.PlannedAction)
	}
	if policy.Weight != TurnWeightStandard {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightStandard)
	}
	if policy.ToolProfile != ToolProfileFocusedInspection {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedInspection)
	}
	if policy.AllowRetrieval {
		t.Fatal("repo architecture inspection should not enable retrieval by default")
	}
	if !policy.AllowRetryExpansion {
		t.Fatal("repo architecture inspection should allow retry expansion")
	}
}

func TestDeriveTurnEnvelopePolicy_SourceBackedCodeUsesFocusedSourceCodeEnvelope(t *testing.T) {
	prompt := "Refactor the configuration parser to support hot-reload with validation, rollback on failure, and emit structured change events."
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "code" {
		t.Fatalf("intent = %q, want code", synthesis.Intent)
	}
	if policy.ToolProfile != ToolProfileFocusedSourceCode {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedSourceCode)
	}
	if policy.AllowRetrieval {
		t.Fatal("source-backed code envelope should keep retrieval neutral by default")
	}
	if policy.MaxTools != 8 {
		t.Fatalf("max tools = %d, want 8", policy.MaxTools)
	}
}

func TestDeriveTurnEnvelopePolicy_DerivableQuestionUsesMinimalEnvelope(t *testing.T) {
	prompt := "What is 2 + 2?"
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "question" {
		t.Fatalf("intent = %q, want question", synthesis.Intent)
	}
	if !policy.LightweightToolSurface {
		t.Fatal("derivable direct-fact question should use lightweight tool surface")
	}
	if policy.AllowRetrieval {
		t.Fatal("derivable direct-fact question should not allow retrieval")
	}
	if policy.Weight != TurnWeightLight {
		t.Fatalf("weight = %q, want %q", policy.Weight, TurnWeightLight)
	}
}

func TestDeriveTurnEnvelopePolicy_TildeDistributionUsesFocusedInspectionEnvelope(t *testing.T) {
	prompt := "give me the file distribution in the folder ~"
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if policy.ToolProfile != ToolProfileFocusedInspection {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedInspection)
	}
	if policy.AllowRetrieval {
		t.Fatal("tilde-distribution inspection should not enable retrieval by default")
	}
}

func TestDeriveTurnEnvelopePolicy_InspectionBackedReportAuthoringUsesFocusedAnalysisAuthoringEnvelope(t *testing.T) {
	prompt := "Generate a report on all development projects in my code directory, include project path, project name, project language(s), first edit date, last edit date, and whether the project is out of date with the remote origin repo, then write the report as a new document to my Obsidian vault on my Desktop."
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if synthesis.Intent != "task" {
		t.Fatalf("intent = %q, want task", synthesis.Intent)
	}
	if policy.ToolProfile != ToolProfileFocusedAnalysisAuthoring {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedAnalysisAuthoring)
	}
	if policy.AllowRetrieval {
		t.Fatal("inspection-backed report authoring should not enable retrieval by default")
	}
	if policy.MaxTools != 8 {
		t.Fatalf("max tools = %d, want 8", policy.MaxTools)
	}
}

func TestDeriveTurnEnvelopePolicy_SourceBackedRunbookArtifactDoesNotTriggerAuthorityMutation(t *testing.T) {
	prompt := "Read tmp/procedural-workflow-4/requirements.txt and then create tmp/procedural-workflow-4/deploy-config.json with content {\"service\":\"payments-api\",\"environment\":\"staging\",\"strategy\":\"rolling\"} and create tmp/procedural-workflow-4/rollout-runbook.md with content # Rollout Runbook\n\n1. Deploy payments-api to staging.\n2. Use a rolling strategy.\n3. Verify health checks before promotion.\n"
	synthesis := SynthesizeTaskState(prompt, 1, nil)
	policy := DeriveTurnEnvelopePolicy(prompt, synthesis, 1)

	if policy.AllowAuthorityMutation {
		t.Fatal("artifact file named runbook should not trigger authority mutation mode")
	}
	if policy.ToolProfile != ToolProfileFocusedAuthoring {
		t.Fatalf("tool profile = %q, want %q", policy.ToolProfile, ToolProfileFocusedAuthoring)
	}
}

func TestRequiresExplicitAuthorityMutation_ExplicitCanonicalMemoryPersistence(t *testing.T) {
	prompt := "Capture this deployment policy into canonical memory and store the rule in the policy store for future turns."
	if !requiresExplicitAuthorityMutation(prompt) {
		t.Fatal("explicit canonical-memory persistence should allow authority mutation")
	}
}

func TestTurnEnvelopePolicy_ExpandedPromotesLightweightTurn(t *testing.T) {
	expanded := (TurnEnvelopePolicy{
		Weight:                 TurnWeightLight,
		ContextBudget:          1536,
		AllowRetrieval:         false,
		LightweightToolSurface: true,
		AllowRetryExpansion:    true,
	}).Expanded()

	if expanded.Weight != TurnWeightStandard {
		t.Fatalf("expanded weight = %q, want %q", expanded.Weight, TurnWeightStandard)
	}
	if !expanded.AllowRetrieval {
		t.Fatal("expanded policy should allow retrieval")
	}
	if expanded.LightweightToolSurface {
		t.Fatal("expanded policy should restore normal tool pruning")
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyLightweightSuppressesTools(t *testing.T) {
	sess := session.New("sess-1", "agent-1", "Test")
	stats, err := (TurnEnvelopePolicy{
		Weight:                 TurnWeightLight,
		LightweightToolSurface: true,
	}).applyToolPolicy(context.Background(), sess, nil)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	if got := len(sess.SelectedToolDefs()); got != 0 {
		t.Fatalf("selected tools = %d, want 0", got)
	}
	if stats.EmbeddingStatus != "policy_lightweight" {
		t.Fatalf("embedding status = %q, want policy_lightweight", stats.EmbeddingStatus)
	}
}

func TestTurnEnvelopePolicy_LightweightDoesNotSuppressPinnedWebTool(t *testing.T) {
	sess := session.New("sess-web", "agent-1", "Test")
	sess.AddUserMessage("see if you can use the ghola tool to pull the main page of www.metacritic.com")
	pruner := &countingPolicyPruner{
		pinned: []string{"ghola"},
		fn: func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
			return []llm.ToolDef{{Type: "function", Function: llm.ToolFuncDef{Name: "ghola"}}}, agenttools.ToolSearchStats{
				CandidatesSelected: 1,
				EmbeddingStatus:    "ok",
			}, nil
		},
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:                 TurnWeightLight,
		LightweightToolSurface: true,
		MaxTools:               1,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	got := sess.SelectedToolDefs()
	if len(got) != 1 || got[0].Function.Name != "ghola" {
		t.Fatalf("selected tools = %+v, want pinned ghola", got)
	}
	if stats.EmbeddingStatus == "policy_lightweight" {
		t.Fatal("pinned web tool must not be bypassed by lightweight suppression")
	}
}

func TestDeriveTurnEnvelopePolicy_PublicWebReadUsesFocusedWebProfile(t *testing.T) {
	synthesis := SynthesizeTaskState(
		"Can you use the Playwright MCP to surf the page?",
		1,
		[]string{"browser playwright mcp page browse surf navigate"},
	)
	got := DeriveTurnEnvelopePolicy("Can you use the Playwright MCP to surf the page?", synthesis, 5)
	if got.ToolProfile != ToolProfileFocusedWebRead {
		t.Fatalf("ToolProfile = %q, want %q", got.ToolProfile, ToolProfileFocusedWebRead)
	}
	if got.AllowRetrieval {
		t.Fatal("focused web-read probe should not retrieve stale memories before attempting web tools")
	}
}

func TestApplyToolPolicy_FocusedWebReadExcludesRuntimeContext(t *testing.T) {
	sess := session.New("sess-web-focused", "agent-1", "Test")
	pruner := &countingPolicyPruner{
		pinned: []string{"browser_snapshot", "browser_navigate"},
		fn: func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
			return []llm.ToolDef{
				{Type: "function", Function: llm.ToolFuncDef{Name: "search_memories"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "browser_snapshot"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "browser_navigate"}},
			}, agenttools.ToolSearchStats{CandidatesSelected: 4, EmbeddingStatus: "ok"}, nil
		},
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:         TurnWeightStandard,
		ToolProfile:    ToolProfileFocusedWebRead,
		AllowRetrieval: false,
		MaxTools:       6,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	got := toolDefNamesForPolicy(sess.SelectedToolDefs())
	if containsString(got, "get_runtime_context") {
		t.Fatalf("selected tools = %v, want runtime self-inspection excluded", got)
	}
	if containsString(got, "search_memories") {
		t.Fatalf("selected tools = %v, want memory retrieval excluded", got)
	}
	if !containsString(got, "browser_snapshot") || !containsString(got, "browser_navigate") {
		t.Fatalf("selected tools = %v, want browser tools preserved", got)
	}
	if stats.CandidatesSelected != 2 {
		t.Fatalf("selected count = %d, want 2", stats.CandidatesSelected)
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyFocusedSchedulingPinsSchedulingTools(t *testing.T) {
	sess := session.New("sess-schedule", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	prunerResult := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "search_memories"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "cron"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "write_file"}},
	}
	pruner.fn = func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
		return prunerResult, agenttools.ToolSearchStats{
			CandidatesSelected: len(prunerResult),
			EmbeddingStatus:    "ok",
		}, nil
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:              TurnWeightStandard,
		MaxTools:            4,
		ToolProfile:         ToolProfileFocusedScheduling,
		AllowRetryExpansion: true,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	got := sess.SelectedToolDefs()
	if len(got) != 2 {
		t.Fatalf("selected tools = %d, want 2", len(got))
	}
	if got[0].Function.Name != "cron" || got[1].Function.Name != "get_runtime_context" {
		t.Fatalf("unexpected focused scheduling tool order: %+v", got)
	}
	if stats.CandidatesSelected != 2 {
		t.Fatalf("candidates selected = %d, want 2", stats.CandidatesSelected)
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyFocusedInspectionPinsFilesystemTools(t *testing.T) {
	sess := session.New("sess-inspect", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	prunerResult := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "search_memories"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "glob_files"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "list_directory"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "read_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "write_file"}},
	}
	pruner.fn = func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
		return prunerResult, agenttools.ToolSearchStats{
			CandidatesSelected: len(prunerResult),
			EmbeddingStatus:    "ok",
		}, nil
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:              TurnWeightStandard,
		MaxTools:            4,
		ToolProfile:         ToolProfileFocusedInspection,
		AllowRetryExpansion: true,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	got := sess.SelectedToolDefs()
	if len(got) != 4 {
		t.Fatalf("selected tools = %d, want 4", len(got))
	}
	if got[0].Function.Name != "glob_files" {
		t.Fatalf("first tool = %q, want glob_files", got[0].Function.Name)
	}
	if got[1].Function.Name != "list_directory" {
		t.Fatalf("second tool = %q, want list_directory", got[1].Function.Name)
	}
	if got[2].Function.Name != "read_file" {
		t.Fatalf("third tool = %q, want read_file", got[2].Function.Name)
	}
	if got[3].Function.Name != "get_runtime_context" {
		t.Fatalf("fourth tool = %q, want get_runtime_context", got[3].Function.Name)
	}
	if stats.CandidatesSelected != 4 {
		t.Fatalf("selected count = %d, want 4", stats.CandidatesSelected)
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyFocusedAnalysisAuthoringPinsInspectionAndWriteTools(t *testing.T) {
	sess := session.New("sess-report", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	prunerResult := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "search_memories"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "inventory_projects"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "search_files"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "glob_files"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "list_directory"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "bash"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "read_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "write_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "compose-subagent"}},
	}
	pruner.fn = func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
		return prunerResult, agenttools.ToolSearchStats{
			CandidatesSelected: len(prunerResult),
			EmbeddingStatus:    "ok",
		}, nil
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:              TurnWeightStandard,
		MaxTools:            8,
		ToolProfile:         ToolProfileFocusedAnalysisAuthoring,
		AllowRetryExpansion: true,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	got := sess.SelectedToolDefs()
	if len(got) != 8 {
		t.Fatalf("selected tools = %d, want 8", len(got))
	}
	wantOrder := []string{"inventory_projects", "list_directory", "bash", "search_files", "glob_files", "read_file", "write_file", "get_runtime_context"}
	for i, want := range wantOrder {
		if got[i].Function.Name != want {
			t.Fatalf("tool[%d] = %q, want %q", i, got[i].Function.Name, want)
		}
	}
	if stats.CandidatesSelected != 8 {
		t.Fatalf("selected count = %d, want 8", stats.CandidatesSelected)
	}
}

type countingPolicyPruner struct {
	calls   int
	fn      func(context.Context, *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error)
	pinned  []string
	pinFunc func(*session.Session) []string
}

func (p *countingPolicyPruner) PruneTools(ctx context.Context, sess *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
	if p.fn != nil {
		return p.fn(ctx, sess)
	}
	p.calls++
	return []llm.ToolDef{{Type: "function", Function: llm.ToolFuncDef{Name: "web_search"}}}, agenttools.ToolSearchStats{
		CandidatesSelected: 1,
		EmbeddingStatus:    "ok",
	}, nil
}

func (p *countingPolicyPruner) AlwaysIncluded(sess *session.Session) []string {
	if p.pinFunc != nil {
		return p.pinFunc(sess)
	}
	return p.pinned
}

func toolDefNamesForPolicy(defs []llm.ToolDef) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Function.Name)
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// TestApplyToolPolicy_PinSurvivesProfileAdmitList verifies the v1.0.8
// admission contract: when a tool name is operator-pinned via the
// pruner's AlwaysIncluder surface, the policy stage MUST NOT drop it
// even if the per-profile OperationClass admit list would normally
// reject it. Authority-mutating tools remain the one explicit
// exception (covered by AuthorityMutationOverridesPin below).
func TestApplyToolPolicy_PinSurvivesProfileAdmitList(t *testing.T) {
	sess := session.New("sess-pin-admit", "agent-1", "Test")
	pruner := &countingPolicyPruner{
		pinned: []string{"orchestrate-subagents"},
		fn: func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
			return []llm.ToolDef{
				{Type: "function", Function: llm.ToolFuncDef{Name: "read_file"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "orchestrate-subagents"}},
			}, agenttools.ToolSearchStats{CandidatesSelected: 2, EmbeddingStatus: "ok"}, nil
		},
	}
	_, err := (TurnEnvelopePolicy{
		Weight:      TurnWeightStandard,
		MaxTools:    4,
		ToolProfile: ToolProfileFocusedInspection,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	got := sess.SelectedToolDefs()
	names := make(map[string]bool, len(got))
	for _, d := range got {
		names[d.Function.Name] = true
	}
	if !names["orchestrate-subagents"] {
		t.Fatalf("pinned tool dropped by FocusedInspection admit list; got %v", got)
	}
}

// TestApplyToolPolicy_PinSurvivesMaxToolsTruncation verifies that pin
// names returned by the pruner are not silently dropped when the
// post-filter tool count exceeds MaxTools. The pinned name must reach
// the loop's selected surface even if MaxTools requires displacing an
// unpinned candidate.
func TestApplyToolPolicy_PinSurvivesMaxToolsTruncation(t *testing.T) {
	sess := session.New("sess-pin-trunc", "agent-1", "Test")
	pruner := &countingPolicyPruner{
		pinned: []string{"obsidian_write"},
		fn: func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
			return []llm.ToolDef{
				{Type: "function", Function: llm.ToolFuncDef{Name: "read_file"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "list_directory"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "search_files"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "glob_files"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "obsidian_write"}},
			}, agenttools.ToolSearchStats{CandidatesSelected: 6, EmbeddingStatus: "ok"}, nil
		},
	}
	_, err := (TurnEnvelopePolicy{
		Weight:                 TurnWeightStandard,
		MaxTools:               3,
		RequireArtifactWrite:   true,
		AllowAuthorityMutation: true,
		ToolProfile:            ToolProfileFocusedAuthoring,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	got := sess.SelectedToolDefs()
	if len(got) > 3+1 { // allow at most one slot widening to honor the pin
		t.Fatalf("MaxTools truncation produced %d tools, want at most 4", len(got))
	}
	found := false
	for _, d := range got {
		if d.Function.Name == "obsidian_write" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pin obsidian_write dropped during MaxTools truncation; got %v", got)
	}
}

// TestApplyToolPolicy_AuthorityMutationOverridesPin verifies that
// pinning is NOT a backdoor around the authority-mutation gate. When
// the turn's policy explicitly disallows authority mutation, even a
// pinned name that mutates the authority layer must be removed.
func TestApplyToolPolicy_AuthorityMutationOverridesPin(t *testing.T) {
	sess := session.New("sess-auth-override", "agent-1", "Test")
	pruner := &countingPolicyPruner{
		pinned: []string{"ingest_policy"},
		fn: func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
			return []llm.ToolDef{
				{Type: "function", Function: llm.ToolFuncDef{Name: "ingest_policy"}},
				{Type: "function", Function: llm.ToolFuncDef{Name: "obsidian_write"}},
			}, agenttools.ToolSearchStats{CandidatesSelected: 2, EmbeddingStatus: "ok"}, nil
		},
	}
	_, err := (TurnEnvelopePolicy{
		Weight:                 TurnWeightStandard,
		MaxTools:               4,
		RequireArtifactWrite:   true,
		AllowAuthorityMutation: false,
		ToolProfile:            ToolProfileFocusedAuthoring,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	for _, d := range sess.SelectedToolDefs() {
		if d.Function.Name == "ingest_policy" {
			t.Fatalf("pinned authority-mutating tool ingest_policy should be removed when AllowAuthorityMutation=false")
		}
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyStandardUsesPruner(t *testing.T) {
	sess := session.New("sess-1", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	stats, err := (TurnEnvelopePolicy{
		Weight: TurnWeightStandard,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	if pruner.calls != 1 {
		t.Fatalf("pruner calls = %d, want 1", pruner.calls)
	}
	if got := len(sess.SelectedToolDefs()); got != 1 {
		t.Fatalf("selected tools = %d, want 1", got)
	}
	if stats.CandidatesSelected != 1 {
		t.Fatalf("candidates selected = %d, want 1", stats.CandidatesSelected)
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyCapsFocusedToolSurface(t *testing.T) {
	sess := session.New("sess-1", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	prunerResult := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "a"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "b"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "c"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "d"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "e"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "f"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "g"}},
	}
	pruner.fn = func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
		return prunerResult, agenttools.ToolSearchStats{
			CandidatesSelected: len(prunerResult),
			EmbeddingStatus:    "ok",
		}, nil
	}
	stats, err := (TurnEnvelopePolicy{
		Weight:   TurnWeightStandard,
		MaxTools: 6,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}
	if got := len(sess.SelectedToolDefs()); got != 6 {
		t.Fatalf("selected tools = %d, want 6", got)
	}
	if stats.CandidatesSelected != 6 {
		t.Fatalf("candidates selected = %d, want 6", stats.CandidatesSelected)
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyFiltersAuthorityMutationForArtifactWriteTurn(t *testing.T) {
	sess := session.New("sess-1", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	prunerResult := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "ingest_policy"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "obsidian_write"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "recall_memory"}},
	}
	pruner.fn = func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
		return prunerResult, agenttools.ToolSearchStats{
			CandidatesSelected: len(prunerResult),
			EmbeddingStatus:    "ok",
		}, nil
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:                 TurnWeightStandard,
		MaxTools:               6,
		RequireArtifactWrite:   true,
		AllowAuthorityMutation: false,
		ToolProfile:            ToolProfileFocusedAuthoring,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}

	got := sess.SelectedToolDefs()
	if len(got) != 2 {
		t.Fatalf("selected tools = %d, want 2", len(got))
	}
	if got[0].Function.Name != "obsidian_write" {
		t.Fatalf("first tool = %q, want obsidian_write", got[0].Function.Name)
	}
	if got[1].Function.Name != "get_runtime_context" {
		t.Fatalf("second tool = %q, want get_runtime_context", got[1].Function.Name)
	}
	for _, def := range got {
		switch def.Function.Name {
		case "ingest_policy", "recall_memory":
			t.Fatalf("%s should be filtered out on focused artifact-write turn without retrieval", def.Function.Name)
		}
	}
	if stats.CandidatesSelected != 2 {
		t.Fatalf("candidates selected = %d, want 2", stats.CandidatesSelected)
	}
	if stats.CandidatesPruned != 2 {
		t.Fatalf("candidates pruned = %d, want 2", stats.CandidatesPruned)
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyFocusedAuthoringKeepsMemoryOnlyWhenRetrievalNeeded(t *testing.T) {
	sess := session.New("sess-1", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	prunerResult := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "obsidian_write"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "recall_memory"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "list-open-tasks"}},
	}
	pruner.fn = func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
		return prunerResult, agenttools.ToolSearchStats{
			CandidatesSelected: len(prunerResult),
			EmbeddingStatus:    "ok",
		}, nil
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:                 TurnWeightStandard,
		MaxTools:               6,
		AllowRetrieval:         true,
		RequireArtifactWrite:   true,
		AllowAuthorityMutation: false,
		ToolProfile:            ToolProfileFocusedAuthoring,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}

	got := sess.SelectedToolDefs()
	if len(got) != 3 {
		t.Fatalf("selected tools = %d, want 3", len(got))
	}
	if got[0].Function.Name != "obsidian_write" {
		t.Fatalf("first tool = %q, want obsidian_write", got[0].Function.Name)
	}
	if got[1].Function.Name != "get_runtime_context" {
		t.Fatalf("second tool = %q, want get_runtime_context", got[1].Function.Name)
	}
	if got[2].Function.Name != "recall_memory" {
		t.Fatalf("third tool = %q, want recall_memory", got[2].Function.Name)
	}
	if stats.CandidatesSelected != 3 {
		t.Fatalf("candidates selected = %d, want 3", stats.CandidatesSelected)
	}
	if stats.CandidatesPruned != 1 {
		t.Fatalf("candidates pruned = %d, want 1", stats.CandidatesPruned)
	}
}

func TestTurnEnvelopePolicy_ApplyToolPolicyFocusedAuthoringKeepsArtifactReadForSourceBackedTurn(t *testing.T) {
	sess := session.New("sess-1", "agent-1", "Test")
	pruner := &countingPolicyPruner{}
	prunerResult := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "write_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "read_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "recall_memory"}},
	}
	pruner.fn = func(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
		return prunerResult, agenttools.ToolSearchStats{
			CandidatesSelected: len(prunerResult),
			EmbeddingStatus:    "ok",
		}, nil
	}

	stats, err := (TurnEnvelopePolicy{
		Weight:                 TurnWeightStandard,
		MaxTools:               6,
		AllowRetrieval:         false,
		RequireArtifactWrite:   true,
		AllowAuthorityMutation: false,
		ToolProfile:            ToolProfileFocusedAuthoring,
	}).applyToolPolicy(context.Background(), sess, pruner)
	if err != nil {
		t.Fatalf("applyToolPolicy: %v", err)
	}

	got := sess.SelectedToolDefs()
	if len(got) != 3 {
		t.Fatalf("selected tools = %d, want 3", len(got))
	}
	if got[0].Function.Name != "write_file" {
		t.Fatalf("first tool = %q, want write_file", got[0].Function.Name)
	}
	if got[1].Function.Name != "read_file" {
		t.Fatalf("second tool = %q, want read_file", got[1].Function.Name)
	}
	if got[2].Function.Name != "get_runtime_context" {
		t.Fatalf("third tool = %q, want get_runtime_context", got[2].Function.Name)
	}
	if stats.CandidatesPruned != 1 {
		t.Fatalf("candidates pruned = %d, want 1", stats.CandidatesPruned)
	}
}

func TestMaybeExpandTurnEnvelope_KeepsLightweightForOffTopicSocialTurn(t *testing.T) {
	sess := session.New("sess-1", "agent-1", "Test")
	sess.SetSelectedToolDefs([]llm.ToolDef{})
	policy := TurnEnvelopePolicy{
		Weight:                 TurnWeightLight,
		ContextBudget:          1536,
		AllowRetrieval:         false,
		LightweightToolSurface: true,
		AllowRetryExpansion:    true,
		Reason:                 "simple conversational turn should start with a minimal envelope",
	}
	pipe := &Pipeline{}

	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{
			Code:   "off_topic_social_turn",
			Detail: "social turn drifted into operational status",
		}},
	}

	got := pipe.maybeExpandTurnEnvelope(context.Background(), sess, policy, result, nil)
	if got.Weight != TurnWeightLight {
		t.Fatalf("weight = %q, want %q", got.Weight, TurnWeightLight)
	}
	if !got.LightweightToolSurface {
		t.Fatal("social-turn retry should keep lightweight tool surface")
	}
	if len(sess.SelectedToolDefs()) != 0 {
		t.Fatalf("selected tools = %d, want 0", len(sess.SelectedToolDefs()))
	}
}

func TestShouldKeepSocialTurnAmbientContextMinimal(t *testing.T) {
	if !shouldKeepSocialTurnAmbientContextMinimal(
		TurnEnvelopePolicy{Weight: TurnWeightLight},
		TaskSynthesis{Intent: "conversational"},
	) {
		t.Fatal("expected lightweight conversational turn to keep ambient context minimal")
	}
	if shouldKeepSocialTurnAmbientContextMinimal(
		TurnEnvelopePolicy{Weight: TurnWeightStandard},
		TaskSynthesis{Intent: "conversational"},
	) {
		t.Fatal("standard conversational turn should not force minimal ambient context")
	}
	if shouldKeepSocialTurnAmbientContextMinimal(
		TurnEnvelopePolicy{Weight: TurnWeightLight},
		TaskSynthesis{Intent: "question"},
	) {
		t.Fatal("light non-conversational turn should not force minimal ambient context")
	}
}
