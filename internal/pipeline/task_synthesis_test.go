package pipeline

import (
	"testing"
)

func TestFormatPlannedAction(t *testing.T) {
	tests := []struct {
		action string
		want   string
	}{
		{"execute_directly", "Execute Directly"},
		{"delegate_to_specialist", "Delegate to Specialist"},
		{"compose_subagent", "Compose Sub-Agent"},
		{"unknown", "Execute Directly"},
	}
	for _, tt := range tests {
		got := FormatPlannedAction(tt.action)
		if got != tt.want {
			t.Errorf("FormatPlannedAction(%q) = %q, want %q", tt.action, got, tt.want)
		}
	}
}

func TestMapPlannedAction_ExecuteDirectly(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "execute_directly", Confidence: 0.8}
	decomp := &DecompositionResult{Decision: DecompCentralized}

	decision := MapPlannedAction(synthesis, decomp)
	if decision != ActionGateContinue {
		t.Errorf("expected ActionGateContinue, got %d", decision)
	}
}

func TestMapPlannedAction_DelegateWithAgreement(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "delegate_to_specialist", Confidence: 0.7}
	decomp := &DecompositionResult{Decision: DecompDelegated}

	decision := MapPlannedAction(synthesis, decomp)
	if decision != ActionGateDelegate {
		t.Errorf("expected ActionGateDelegate, got %d", decision)
	}
}

func TestMapPlannedAction_DelegateHighConfidenceOverride(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "delegate_to_specialist", Confidence: 0.75}
	// Decomp is nil — planner's high confidence should override.
	decision := MapPlannedAction(synthesis, nil)
	if decision != ActionGateDelegate {
		t.Errorf("expected ActionGateDelegate with high confidence, got %d", decision)
	}
}

func TestMapPlannedAction_DelegateLowConfidenceFallsThrough(t *testing.T) {
	synthesis := TaskSynthesis{PlannedAction: "delegate_to_specialist", Confidence: 0.5}
	decomp := &DecompositionResult{Decision: DecompCentralized}

	decision := MapPlannedAction(synthesis, decomp)
	if decision != ActionGateContinue {
		t.Errorf("expected ActionGateContinue with low confidence, got %d", decision)
	}
}

func TestMapPlannedAction_ComposeSubagent(t *testing.T) {
	synthesis := TaskSynthesis{
		PlannedAction: "compose_subagent",
		Confidence:    0.7,
		CapabilityFit: 0.2, // Low fit → specialist proposal
	}

	decision := MapPlannedAction(synthesis, nil)
	if decision != ActionGateSpecialistPropose {
		t.Errorf("expected ActionGateSpecialistPropose, got %d", decision)
	}
}

func TestMapPlannedAction_ComposeSubagentHighFitFallsThrough(t *testing.T) {
	synthesis := TaskSynthesis{
		PlannedAction: "compose_subagent",
		Confidence:    0.7,
		CapabilityFit: 0.8, // High fit → no specialist needed
	}

	decision := MapPlannedAction(synthesis, nil)
	if decision != ActionGateContinue {
		t.Errorf("expected ActionGateContinue with high fit, got %d", decision)
	}
}

func TestSynthesizeTaskState_TreatsColloquialGreetingAsConversational(t *testing.T) {
	result := SynthesizeTaskState("What's the good word?", 2, nil)
	if result.Intent != "conversational" {
		t.Fatalf("intent = %q, want conversational", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.RetrievalNeeded {
		t.Fatal("colloquial greeting should not require retrieval")
	}
}

func TestSynthesizeTaskState_TreatsWhatsNewAsConversational(t *testing.T) {
	result := SynthesizeTaskState("What's new, Duncan?", 2, nil)
	if result.Intent != "conversational" {
		t.Fatalf("intent = %q, want conversational", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.RetrievalNeeded {
		t.Fatal("phatic what's-new turn should not require retrieval")
	}
}

func TestSynthesizeTaskState_SimpleDirectTaskDoesNotRequireRetrieval(t *testing.T) {
	result := SynthesizeTaskState("Create a new markdown document in the Obsidian vault for today's notes.", 2, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("simple direct vault authoring should not require retrieval")
	}
}

func TestSynthesizeTaskState_CountPromptIsTaskNotConversation(t *testing.T) {
	result := SynthesizeTaskState("Count markdown files recursively in /Users/jmachen/code and return only the number.", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
}

func TestSynthesizeTaskState_InspectionQuestionUsesFocusedTaskPath(t *testing.T) {
	result := SynthesizeTaskState("What's in your vault right now?", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("inspection-shaped question should not require retrieval by default")
	}
}

func TestSynthesizeTaskState_WorkspaceVaultFollowupStaysInspectionTask(t *testing.T) {
	result := SynthesizeTaskState("What about the vault in your workspace?", 2, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("workspace vault follow-up should not require retrieval by default")
	}
}

func TestSynthesizeTaskState_DesktopVaultSummaryUsesInspectionTaskPath(t *testing.T) {
	result := SynthesizeTaskState("Please give me the briefest summary you can of the contents of the vault on my Desktop.", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("desktop vault summary should not require retrieval by default")
	}
}

func TestSynthesizeTaskState_PathProjectListingUsesInspectionTaskPath(t *testing.T) {
	result := SynthesizeTaskState("What about a list of the projects in /Users/jmachen/code?", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("path-shaped project listing should not require retrieval by default")
	}
}

func TestSynthesizeTaskState_TildeDistributionUsesInspectionTaskPath(t *testing.T) {
	result := SynthesizeTaskState("give me the file distribution in the folder ~", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("tilde-distribution inspection should not require retrieval by default")
	}
}

func TestSynthesizeTaskState_InspectionBackedReportAuthoringIsTaskNotCreative(t *testing.T) {
	prompt := "Generate a report on all development projects in my code directory, include project path, project name, project language(s), first edit date, last edit date, and whether the project is out of date with the remote origin repo, then write the report as a new document to my Obsidian vault on my Desktop."
	result := SynthesizeTaskState(prompt, 1, nil)

	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("inspection-backed report authoring should not widen into retrieval by default")
	}
}

func TestSynthesizeTaskState_TaskWithContinuityCueRequiresRetrieval(t *testing.T) {
	result := SynthesizeTaskState("Update the existing Obsidian note we discussed earlier with today's decisions.", 2, nil)
	if !result.RetrievalNeeded {
		t.Fatal("task with explicit continuity cue should require retrieval")
	}
}

func TestSynthesizeTaskState_InvestigativeTaskRequiresRetrieval(t *testing.T) {
	result := SynthesizeTaskState("Create a report that explains the root cause and identifies which systems were affected.", 1, nil)
	if !result.RetrievalNeeded {
		t.Fatal("investigative reporting task should require retrieval")
	}
}

func TestSynthesizeTaskState_ExplicitSchedulingStaysFocusedWithoutRetrieval(t *testing.T) {
	result := SynthesizeTaskState("schedule a cron job that runs every 5 minutes and tell me exactly what was scheduled", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("explicit scheduling should not require retrieval by default")
	}
	if result.ProceduralUncertainty {
		t.Fatal("explicit scheduling should not be treated as procedurally uncertain")
	}
}

func TestSynthesizeTaskState_SchedulingAliasFollowupUsesContinuityRetrieval(t *testing.T) {
	result := SynthesizeTaskState("Create the quiet ticker now and tell me exactly what was scheduled.", 5, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if !result.RetrievalNeeded {
		t.Fatal("scheduling alias follow-up should retrieve prior session continuity")
	}
	if result.RetrievalReason != "scheduling_alias_continuity" {
		t.Fatalf("retrieval reason = %q, want scheduling_alias_continuity", result.RetrievalReason)
	}
	if result.ProceduralUncertainty {
		t.Fatal("scheduling alias follow-up should not be marked procedurally uncertain")
	}
}

func TestSynthesizeTaskState_ProceduralUncertaintyTriggersAppliedLearningRetrieval(t *testing.T) {
	result := SynthesizeTaskState(
		"Set up a canary release workflow for the auth service with rollout and rollback gates.",
		1,
		nil,
	)
	if !result.ProceduralUncertainty {
		t.Fatal("expected procedural uncertainty for unfamiliar procedural task")
	}
	if !result.RetrievalNeeded {
		t.Fatal("procedural uncertainty should trigger applied-learning retrieval")
	}
	if result.RetrievalReason != "applied_learning_uncertainty" {
		t.Fatalf("retrieval reason = %q, want applied_learning_uncertainty", result.RetrievalReason)
	}
}

func TestSynthesizeTaskState_NoteTitleWithTestDoesNotBecomeCode(t *testing.T) {
	result := SynthesizeTaskState("Create a new Obsidian note named codex-live-test.md in the vault containing exactly: # Codex Live Test.", 1, nil)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.RetrievalNeeded {
		t.Fatal("simple vault note creation should not require retrieval")
	}
	if result.ProceduralUncertainty {
		t.Fatal("simple vault note creation should not be treated as procedural uncertainty")
	}
	if result.RetrievalReason != "none" {
		t.Fatalf("retrieval reason = %q, want none", result.RetrievalReason)
	}
}

func TestSynthesizeTaskState_VerboseSingleStepAuthoringStaysSimple(t *testing.T) {
	result := SynthesizeTaskState(
		"Create a new Obsidian note named codex-live-test-2.md in the vault containing exactly: # Codex Live Test 2. Use the Obsidian vault tool if you have it. Do not ask for confirmation.",
		1,
		nil,
	)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("verbose single-step authoring should not require retrieval")
	}
}

func TestSynthesizeTaskState_BoundedMultiArtifactAuthoringStaysDirect(t *testing.T) {
	result := SynthesizeTaskState(
		"In the Obsidian vault, create two notes: project-bootstrap-check.md and project-bootstrap-actions.md. The first note should contain exactly: # Project Bootstrap Check. The second note should contain exactly: # Project Bootstrap Actions.",
		1,
		nil,
	)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "moderate" {
		t.Fatalf("complexity = %q, want moderate", result.Complexity)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("bounded multi-artifact authoring should not require retrieval")
	}
	if result.ProceduralUncertainty {
		t.Fatal("bounded multi-artifact authoring should not be treated as procedural uncertainty")
	}
}

func TestSynthesizeTaskState_PathShapedFileAuthoringStaysDirect(t *testing.T) {
	result := SynthesizeTaskState(
		"Create tmp/procedural-canary/rollout-config.json containing exactly: {\"service\":\"auth\"}.",
		1,
		nil,
	)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "simple" {
		t.Fatalf("complexity = %q, want simple", result.Complexity)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("path-shaped file authoring should not require retrieval")
	}
}

func TestSynthesizeTaskState_InlineExactMultiArtifactAuthoringStaysDirect(t *testing.T) {
	result := SynthesizeTaskState(
		"Create the following files:\n- tmp/procedural-canary/rollout-config.json containing exactly:\n{\"service\":\"auth\"}\n- tmp/procedural-canary/rollout-runbook.md containing exactly:\n# Rollout Runbook\n\n1. Read [[rollout-config.json]].",
		1,
		nil,
	)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "moderate" {
		t.Fatalf("complexity = %q, want moderate", result.Complexity)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
}

func TestSynthesizeTaskState_SourceBackedExactAuthoringStaysTask(t *testing.T) {
	result := SynthesizeTaskState(
		"Read tmp/procedural-workflow-4/requirements.txt and then create tmp/procedural-workflow-4/deploy-config.json with content {\"service\":\"payments-api\",\"environment\":\"staging\",\"strategy\":\"rolling\"} and create tmp/procedural-workflow-4/rollout-runbook.md with content # Rollout Runbook\n\n1. Deploy payments-api to staging.\n2. Use a rolling strategy.\n3. Verify health checks before promotion.\n",
		1,
		nil,
	)
	if result.Intent != "task" {
		t.Fatalf("intent = %q, want task", result.Intent)
	}
	if result.Complexity != "moderate" {
		t.Fatalf("complexity = %q, want moderate", result.Complexity)
	}
	if result.PlannedAction != "execute_directly" {
		t.Fatalf("planned action = %q, want execute_directly", result.PlannedAction)
	}
	if result.RetrievalNeeded {
		t.Fatal("source-backed exact authoring should not require retrieval")
	}
}

func TestSynthesizeTaskState_CapabilityFitRecognizesHyphenatedSkillConcepts(t *testing.T) {
	result := SynthesizeTaskState(
		"Create a new markdown document in the Obsidian vault for today's notes.",
		1,
		[]string{"obsidian-vault Read and write to the shared Obsidian vault for persistent memory"},
	)
	if result.CapabilityFit <= 0 {
		t.Fatalf("capability fit = %v, want > 0", result.CapabilityFit)
	}
	for _, missing := range result.MissingSkills {
		if missing == "obsidian" || missing == "vault" {
			t.Fatalf("missing skills unexpectedly includes %q", missing)
		}
	}
}
