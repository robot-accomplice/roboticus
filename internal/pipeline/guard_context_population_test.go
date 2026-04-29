package pipeline

import (
	"context"
	"testing"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/llm"
	"roboticus/internal/session"
	"roboticus/testutil"
)

func TestBuildGuardContext_PopulatesPipelineHintsAndStoreBackedFields(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		BGWorker: testutil.BGWorker(t, 2),
	})

	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO sub_agents (id, name, model, skills_json, enabled) VALUES ('sub-1', 'CodeReviewer', 'gpt-4', '[]', 1)`); err != nil {
		t.Fatalf("seed sub_agents: %v", err)
	}
	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO model_selection_events
		     (id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model, user_excerpt, candidates_json, created_at)
		 VALUES ('mse-1', 'turn-1', 'sess-1', '', 'api', 'openai/gpt-4.1', 'routed', 'openai/gpt-4.1', 'hello', '[]', datetime('now'))`); err != nil {
		t.Fatalf("seed model_selection_events: %v", err)
	}

	sess := session.New("sess-1", "agent-1", "TestBot")
	sess.AddUserMessage("who are you?")
	sess.AddAssistantMessage("previous answer", nil)
	sess.AddToolResult("call-1", "delegate_task", "subagent completed the audit", false)
	sess.SetSelectedToolDefs([]llm.ToolDef{{Type: "function", Function: llm.ToolFuncDef{Name: "browser_snapshot"}}})
	sess.SetTaskVerificationHints("model_identity", "simple", "delegate_to_specialist", nil)

	ctx := pipe.buildGuardContext(sess)
	if ctx == nil {
		t.Fatal("buildGuardContext returned nil")
	}
	if !ctx.HasIntent("model_identity") {
		t.Fatalf("Intents = %v, want model_identity", ctx.Intents)
	}
	if !ctx.HasIntent("delegation") {
		t.Fatalf("Intents = %v, want delegation derived from planned action", ctx.Intents)
	}
	if ctx.ResolvedModel != "openai/gpt-4.1" {
		t.Fatalf("ResolvedModel = %q, want openai/gpt-4.1", ctx.ResolvedModel)
	}
	if len(ctx.SubagentNames) != 1 || ctx.SubagentNames[0] != "codereviewer" {
		t.Fatalf("SubagentNames = %v, want [codereviewer]", ctx.SubagentNames)
	}
	if len(ctx.SelectedToolNames) != 1 || ctx.SelectedToolNames[0] != "browser_snapshot" {
		t.Fatalf("SelectedToolNames = %v, want [browser_snapshot]", ctx.SelectedToolNames)
	}
	if !ctx.DelegationProvenance.SubagentTaskStarted ||
		!ctx.DelegationProvenance.SubagentTaskCompleted ||
		!ctx.DelegationProvenance.SubagentResultAttached {
		t.Fatalf("DelegationProvenance = %+v, want all true", ctx.DelegationProvenance)
	}
}

func TestBuildGuardContext_UsesPriorTurnAssistantHistoryOnly(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		BGWorker: testutil.BGWorker(t, 2),
	})

	sess := session.New("sess-2", "agent-1", "TestBot")
	sess.AddUserMessage("old request")
	sess.AddAssistantMessage("prior turn answer", nil)
	sess.AddUserMessage("create the note now")
	sess.AddToolResult("call-1", "obsidian_write", `{"path":"codex-live-test.md","status":"ok"}`, false)
	sess.AddAssistantMessage("The note was created successfully.", nil)

	ctx := pipe.buildGuardContext(sess)
	if ctx == nil {
		t.Fatal("buildGuardContext returned nil")
	}
	if ctx.UserPrompt != "create the note now" {
		t.Fatalf("UserPrompt = %q, want latest user prompt", ctx.UserPrompt)
	}
	if ctx.PreviousAssistant != "prior turn answer" {
		t.Fatalf("PreviousAssistant = %q, want prior-turn assistant only", ctx.PreviousAssistant)
	}
	if len(ctx.PriorAssistantMessages) != 1 || ctx.PriorAssistantMessages[0] != "prior turn answer" {
		t.Fatalf("PriorAssistantMessages = %v, want only prior-turn assistant history", ctx.PriorAssistantMessages)
	}
	if len(ctx.ToolResults) != 1 || ctx.ToolResults[0].ToolName != "obsidian_write" {
		t.Fatalf("ToolResults = %+v, want current-turn tool results preserved", ctx.ToolResults)
	}
}

func TestBuildGuardContext_PreservesArtifactProofMetadata(t *testing.T) {
	pipe := New(PipelineDeps{BGWorker: testutil.BGWorker(t, 1)})
	sess := session.New("sess-3", "agent-1", "TestBot")
	sess.AddUserMessage("write the file")
	proof := agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "hello", false)
	sess.AddToolResultWithMetadata("call-1", "write_file", proof.Output(), proof.Metadata(), false)

	ctx := pipe.buildGuardContext(sess)
	if len(ctx.ToolResults) != 1 {
		t.Fatalf("tool results = %d, want 1", len(ctx.ToolResults))
	}
	if ctx.ToolResults[0].ArtifactProof == nil {
		t.Fatal("expected artifact proof on tool result entry")
	}
	if ctx.ToolResults[0].ArtifactProof.Path != "tmp/out.txt" {
		t.Fatalf("artifact path = %q", ctx.ToolResults[0].ArtifactProof.Path)
	}
}

func TestApplyFullWithContext_PrecomputesGuardScoresAndIntent(t *testing.T) {
	chain := NewGuardChain(&TaskDeferralGuard{}, &InternalJargonGuard{})
	ctx := &GuardContext{
		UserPrompt:        "who are you?",
		PreviousAssistant: "I completed the task and then completed the task again.",
		ToolResults: []ToolResultEntry{
			{ToolName: "get_memory_stats", Output: `{"count":42}`},
		},
	}

	result := chain.ApplyFullWithContext("I am Claude and I'll check that next.", ctx)
	if len(result.Violations) == 0 {
		t.Fatal("expected at least one guard violation to confirm live guard precompute path ran")
	}
	if len(ctx.Intents) == 0 {
		t.Fatal("Intents should be inferred during guard precompute")
	}
	if ctx.SemanticScores == nil {
		t.Fatal("SemanticScores should be initialized during guard precompute")
	}
	if _, ok := ctx.SemanticScores["identity_claim"]; !ok {
		t.Fatal("identity_claim score missing")
	}
	if _, ok := ctx.SemanticScores["prev_overlap"]; !ok {
		t.Fatal("prev_overlap score missing")
	}
}
