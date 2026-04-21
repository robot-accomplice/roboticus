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

type countingPolicyPruner struct {
	calls int
	fn    func(context.Context, *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error)
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
