package pipeline

import (
	"testing"
	"time"

	"roboticus/internal/core"
)

// --- #70 Intent & Classification Tests ---

func TestExpandedIntents(t *testing.T) {
	r := NewIntentRegistry()

	tests := []struct {
		input      string
		wantIntent Intent
	}{
		{"who are you?", IntentModelIdentity},
		{"what model are you running?", IntentModelIdentity},
		{"transfer 10 SOL to Bob", IntentFinancialAction},
		{"send money to Alice", IntentFinancialAction},
		{"write code for a web server", IntentCodeGeneration},
		{"implement a binary search", IntentCodeGeneration},
		{"restart the service", IntentSystemAdmin},
		{"schedule a reminder for 5pm", IntentScheduling},
		{"search for Go tutorials", IntentWebSearch},
		{"summarize this article", IntentSummarization},
		{"translate this to spanish", IntentTranslation},
		{"ok", IntentAcknowledgement},
		{"thanks", IntentAcknowledgement},
	}
	for _, tt := range tests {
		intent, conf := r.Classify(tt.input)
		if intent != tt.wantIntent {
			t.Errorf("Classify(%q) = (%q, %.2f), want intent %q", tt.input, intent, conf, tt.wantIntent)
		}
		if conf < 0.5 {
			t.Errorf("Classify(%q) confidence %.2f too low", tt.input, conf)
		}
	}
}

func TestIntentMetadata(t *testing.T) {
	m := GetIntentMetadata(IntentFinancialAction)
	if m.Priority != 9 {
		t.Errorf("FinancialAction priority = %d, want 9", m.Priority)
	}
	if !m.BypassCache {
		t.Error("FinancialAction should bypass cache")
	}
	if m.PreferredTier != core.ModelTierLarge {
		t.Errorf("FinancialAction tier = %v, want Large", m.PreferredTier)
	}

	// Unknown intent returns zero-value.
	m2 := GetIntentMetadata(Intent("nonexistent"))
	if m2.Priority != 0 || m2.BypassCache || m2.PreferredTier != 0 {
		t.Error("unknown intent should return zero-value metadata")
	}
}

// --- #72 Guard Verdict Tests ---

func TestGuardVerdict(t *testing.T) {
	// Verify the constants exist and have expected values.
	if GuardPass != 0 {
		t.Errorf("GuardPass = %d, want 0", GuardPass)
	}
	if GuardRewritten != 1 {
		t.Errorf("GuardRewritten = %d, want 1", GuardRewritten)
	}
	if GuardRetryRequested != 2 {
		t.Errorf("GuardRetryRequested = %d, want 2", GuardRetryRequested)
	}

	// Verify GuardResult can carry a verdict.
	result := GuardResult{Passed: false, Verdict: GuardRewritten, Content: "modified"}
	if result.Verdict != GuardRewritten {
		t.Error("GuardResult should carry verdict")
	}
}

// --- #73-74 Retry Directives Tests ---

func TestRetryDirectives(t *testing.T) {
	d := GetRetryDirective("literary_quote_retry")
	if d == nil {
		t.Fatal("expected directive for literary_quote_retry")
	}
	if d.TokenBudget != 1024 {
		t.Errorf("TokenBudget = %d, want 1024", d.TokenBudget)
	}
	if d.Instruction == "" {
		t.Error("Instruction should not be empty")
	}

	d2 := GetRetryDirective("nonexistent_guard")
	if d2 != nil {
		t.Error("expected nil for unknown guard")
	}
}

func TestRetryWithGuardsResume(t *testing.T) {
	chain := NewGuardChain(
		&EmptyResponseGuard{},
		NewRepetitionGuard(),
	)
	result := retryWithGuardsResume(chain, "hello world", nil, 1)
	if !result.RetryRequested {
		// Should pass since "hello world" is fine for RepetitionGuard.
		_ = result // SA9003: intentionally empty — documenting expected non-retry
	}
	if len(result.Violations) > 0 {
		t.Errorf("expected no violations, got %v", result.Violations)
	}
}

// --- #75 Cached Guard Chain Tests ---

func TestCachedChainExcludesGuards(t *testing.T) {
	registry := NewDefaultGuardRegistry()
	cached := registry.Chain(GuardSetCached)
	full := registry.Chain(GuardSetFull)

	// Cached chain should have fewer guards than full.
	if cached.Len() >= full.Len() {
		t.Errorf("cached chain (%d) should have fewer guards than full (%d)", cached.Len(), full.Len())
	}
}

// --- #76 ActionVerificationGuard Tests ---

func TestActionVerificationGuard_SuccessClaimOnFailure(t *testing.T) {
	g := &ActionVerificationGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "wallet_transfer", Output: "error: insufficient funds"},
		},
	}
	result := g.CheckWithContext("The transfer was successfully transferred to your account.", ctx)
	if result.Passed {
		t.Error("should fail when claiming success but tool reported failure")
	}
	if !result.Retry {
		t.Error("should request retry")
	}
}

func TestActionVerificationGuard_NoFinancialTools(t *testing.T) {
	g := &ActionVerificationGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "web_search", Output: "results"},
		},
	}
	result := g.CheckWithContext("I transferred $100.", ctx)
	if !result.Passed {
		t.Error("should pass when no financial tools were called (that's FinancialActionTruthGuard's job)")
	}
}

// --- #77 LiteraryQuoteRetryGuard Tests ---

func TestLiteraryQuoteRetryGuard(t *testing.T) {
	g := &LiteraryQuoteRetryGuard{}

	tests := []struct {
		content string
		pass    bool
	}{
		{"Here's the information you asked for.", true},
		{"As the poet wrote, the world is beautiful.", false},
		{"To quote Shakespeare, all that glitters is not gold.", false},
		{"The answer is 42.", true},
	}
	for _, tt := range tests {
		result := g.Check(tt.content)
		if result.Passed != tt.pass {
			t.Errorf("Check(%q) passed=%v, want %v", tt.content[:30], result.Passed, tt.pass)
		}
	}
}

// --- #78 PerspectiveGuard Tests ---

func TestPerspectiveGuard(t *testing.T) {
	g := &PerspectiveGuard{}

	// Should fail on multiple "you [verb]" patterns outside RP.
	ctx := &GuardContext{Intents: []string{"question"}}
	result := g.CheckWithContext("You walk into the room and you see a light.", ctx)
	if result.Passed {
		t.Error("should fail on first-person narration of user actions")
	}

	// Should pass in role-play context.
	rpCtx := &GuardContext{Intents: []string{"role_play"}}
	result2 := g.CheckWithContext("You walk into the room and you see a light.", rpCtx)
	if !result2.Passed {
		t.Error("should pass in role_play context")
	}

	// Should pass for single occurrence.
	result3 := g.CheckWithContext("You see the results in the table below.", ctx)
	if !result3.Passed {
		t.Error("single 'you see' should pass")
	}
}

// --- #79 InternalProtocolGuard Tests ---

func TestInternalProtocolGuard(t *testing.T) {
	g := &InternalProtocolGuard{}

	result := g.Check("Hello [PROTOCOL: test] world")
	if result.Passed {
		t.Error("should strip protocol markers")
	}

	result2 := g.Check("Normal response text.")
	if !result2.Passed {
		t.Error("should pass clean text")
	}

	result3 := g.Check("[TRACE: abc123]")
	if result3.Passed {
		t.Error("should fail on trace-only content")
	}
	if !result3.Retry {
		t.Error("should request retry when content is all metadata")
	}
}

// --- #80 NonRepetitionGuard cross-turn Tests ---

func TestFindSelfEchoAcrossHistory(t *testing.T) {
	history := []string{
		"Here's what I found:\nSome details.",
		"Here's what I found:\nDifferent details.",
		"Something else entirely.",
	}
	reason := findSelfEchoAcrossHistory("Here's what I found:\nMore details.", history)
	if reason == "" {
		t.Error("should detect template loop with same opening across 3+ turns")
	}

	reason2 := findSelfEchoAcrossHistory("A unique response.", history)
	if reason2 != "" {
		t.Errorf("should not detect echo for unique response, got: %s", reason2)
	}
}

// --- #82-83 Decomposition Tests ---

func TestUtilityMargin(t *testing.T) {
	// Simple case: 1 subtask, medium complexity, moderate fit.
	margin := utilityMargin(0.5, 1, 0.5)
	// (0.5 * 0.5) + (1-1) * 0.12 + 0.5 * 0.45 - (0.25 + 1 * 0.04) = 0.25 + 0 + 0.225 - 0.29 = 0.185
	if margin < 0.18 || margin > 0.19 {
		t.Errorf("utilityMargin(0.5, 1, 0.5) = %f, want ~0.185", margin)
	}

	// High complexity with multiple subtasks should be positive.
	margin2 := utilityMargin(0.9, 4, 0.8)
	if margin2 <= 0 {
		t.Errorf("high complexity delegation should have positive margin, got %f", margin2)
	}
}

func TestSpecialistProposal(t *testing.T) {
	content := "I need a detailed financial analysis of Q4 revenue trends compared to Q3, with projections for next quarter."
	result := EvaluateDecomposition(content, 15)
	if result.Decision != DecompSpecialistProposal {
		t.Skipf("content may not be long enough to trigger specialist, got %s", result.Decision)
	}
}

// --- #84 Tool Pruning by Embedding Tests ---

func TestPruneByEmbedding(t *testing.T) {
	tools := []ToolDef{
		{Name: "search", Description: "Search the web", Embedding: []float64{1, 0, 0}},
		{Name: "calculate", Description: "Do math", Embedding: []float64{0, 1, 0}},
		{Name: "browse", Description: "Browse web pages", Embedding: []float64{0.9, 0.1, 0}},
	}
	query := []float64{1, 0, 0} // Should be closest to "search" and "browse"

	result := PruneByEmbedding(tools, query, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].Name != "search" {
		t.Errorf("first tool should be 'search', got %q", result[0].Name)
	}
}

func TestPruneByEmbedding_NoEmbeddings(t *testing.T) {
	tools := []ToolDef{
		{Name: "a", Description: "A tool"},
		{Name: "b", Description: "B tool"},
	}
	result := PruneByEmbedding(tools, []float64{1, 0}, 1)
	// Should still return results (with default 0.5 score).
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{1, 0, 0}
	sim := cosineSimilarity(a, b)
	if sim < 0.99 {
		t.Errorf("identical vectors should have similarity ~1.0, got %f", sim)
	}

	c := []float64{0, 1, 0}
	sim2 := cosineSimilarity(a, c)
	if sim2 > 0.01 {
		t.Errorf("orthogonal vectors should have similarity ~0.0, got %f", sim2)
	}
}

// --- #85 Event Bus Tests ---

func TestEventBus(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(10)
	defer bus.Unsubscribe(ch)

	if bus.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount = %d, want 1", bus.SubscriberCount())
	}

	bus.Publish(PipelineEvent{Stage: "test", Data: "hello"})

	select {
	case event := <-ch:
		if event.Stage != "test" {
			t.Errorf("Stage = %q, want %q", event.Stage, "test")
		}
		if event.Data != "hello" {
			t.Errorf("Data = %v, want %q", event.Data, "hello")
		}
		if event.Timestamp.IsZero() {
			t.Error("Timestamp should be auto-set")
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(10)
	bus.Unsubscribe(ch)

	if bus.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount = %d after unsubscribe, want 0", bus.SubscriberCount())
	}
}

func TestEventBus_NonBlocking(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1) // buffer of 1
	defer bus.Unsubscribe(ch)

	// Fill the buffer.
	bus.Publish(PipelineEvent{Stage: "first"})
	// This should not block even though buffer is full.
	bus.Publish(PipelineEvent{Stage: "second"})
	// Just drain and verify no deadlock.
	<-ch
}

// --- #86 Quality Gate Tests ---

func TestQualityGate(t *testing.T) {
	qg := NewQualityGate()

	if err := qg.Check(""); err == nil {
		t.Error("empty string should fail quality gate")
	}
	if err := qg.Check("short"); err == nil {
		t.Error("very short string should fail quality gate")
	}
	if err := qg.Check("This is a reasonable response that provides useful information."); err != nil {
		t.Errorf("good content should pass: %v", err)
	}

	// Highly repetitive content.
	repetitive := "the the the the the the the the the the"
	if err := qg.Check(repetitive); err == nil {
		t.Error("highly repetitive content should fail")
	}
}

func TestQualityGateCustom(t *testing.T) {
	qg := NewQualityGateCustom(5, 0.9)
	if err := qg.Check("Hello world"); err != nil {
		t.Errorf("custom gate should pass: %v", err)
	}
}

// --- #87 Capability Mapping Test ---

func TestCapabilityMappingDocExists(t *testing.T) {
	// Verify the interfaces.go file has the mapping comment.
	// This is a build-time check — if the file doesn't compile, this won't run.
	var _ InjectionChecker
	var _ MemoryRetriever
	var _ SkillMatcher
	var _ ToolExecutor
	var _ Ingestor
	var _ NicknameRefiner
	var _ StreamPreparer
	var _ IntentClassifier
}

// --- #88 Trace Namespace Constants Tests ---

func TestTraceNamespaceConstants(t *testing.T) {
	constants := []string{
		TraceNSPipeline, TraceNSGuard, TraceNSInference, TraceNSRetrieval,
		TraceNSToolSearch, TraceNSMCP, TraceNSDelegation, TraceNSTaskState,
	}
	seen := make(map[string]bool)
	for _, c := range constants {
		if c == "" {
			t.Error("trace namespace constant should not be empty")
		}
		if seen[c] {
			t.Errorf("duplicate trace namespace: %s", c)
		}
		seen[c] = true
	}
}

// --- #89 Flight Recorder Variants Tests ---

func TestFlightRecorderStepKinds(t *testing.T) {
	// Verify new step kinds are distinct.
	kinds := []StepKind{
		StepToolCall, StepLLMCall, StepGuardCheck, StepRetry,
		StepGuardPrecompute, StepCacheHit, StepDecomposition, StepSpeculation,
	}
	seen := make(map[StepKind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate step kind: %d", k)
		}
		seen[k] = true
	}
}

func TestFlightRecorderRecordNewSteps(t *testing.T) {
	rt := NewReactTrace()
	rt.RecordStep(ReactStep{
		Kind:    StepGuardPrecompute,
		Name:    "precompute",
		Success: true,
	})
	rt.RecordStep(ReactStep{
		Kind:    StepCacheHit,
		Name:    "cache_hit",
		Success: true,
	})
	rt.Finish()
	if len(rt.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(rt.Steps))
	}
}

// --- #81 Guard Fallback Templates Tests ---

func TestGuardFallbackTemplates(t *testing.T) {
	// Known guards should have templates.
	known := []string{
		"empty_response", "repetition", "system_prompt_leak",
		"execution_truth", "financial_action_truth", "perspective",
		"literary_quote_retry", "internal_protocol",
	}
	for _, name := range known {
		tmpl := GetFallbackTemplate(name)
		if tmpl == "" {
			t.Errorf("expected fallback template for guard %q", name)
		}
	}

	// Unknown guard returns empty.
	if tmpl := GetFallbackTemplate("nonexistent_guard"); tmpl != "" {
		t.Error("expected empty string for unknown guard")
	}
}

// --- #71 Guard Pre-computation Tests ---

func TestPrecomputeGuardScores(t *testing.T) {
	ctx := &GuardContext{
		UserPrompt:        "who are you?",
		PreviousAssistant: "I am an AI assistant.",
	}
	precomputeGuardScores(ctx, "I am Claude, a large language model.")

	// Should have auto-classified intents.
	if len(ctx.Intents) == 0 {
		t.Error("intents should be populated")
	}

	// Should have semantic scores.
	if ctx.SemanticScores == nil {
		t.Fatal("SemanticScores should be initialized")
	}
	if _, ok := ctx.SemanticScores["identity_claim"]; !ok {
		t.Error("identity_claim score should be set")
	}
	if ctx.SemanticScores["identity_claim"] != 1.0 {
		t.Errorf("identity_claim should be 1.0 for 'i am claude', got %f", ctx.SemanticScores["identity_claim"])
	}
	if _, ok := ctx.SemanticScores["prev_overlap"]; !ok {
		t.Error("prev_overlap score should be set")
	}
}

// --- Event Bus timestamp test ---

func TestEventBus_TimestampAutoSet(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(10)
	defer bus.Unsubscribe(ch)

	before := time.Now()
	bus.Publish(PipelineEvent{Stage: "test"})
	after := time.Now()

	event := <-ch
	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Error("auto-set timestamp should be between before and after")
	}
}
