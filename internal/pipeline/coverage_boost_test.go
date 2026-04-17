package pipeline

import (
	"context"
	"errors"
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// ---------------------------------------------------------------------------
// types_pipeline.go
// ---------------------------------------------------------------------------

func TestDefaultLoopConfig(t *testing.T) {
	lc := DefaultLoopConfig()
	if lc.MaxTurns != 10 { // Rust parity: 10 turns
		t.Errorf("MaxTurns = %d, want 10", lc.MaxTurns)
	}
	if lc.IdleThreshold != 3 {
		t.Errorf("IdleThreshold = %d, want 3", lc.IdleThreshold)
	}
	if lc.LoopWindow != 3 {
		t.Errorf("LoopWindow = %d, want 3", lc.LoopWindow)
	}
}

func TestToolDef_EstimateTokens(t *testing.T) {
	td := ToolDef{
		Name:           "search",     // 6 chars
		Description:    "Search web", // 10 chars
		ParametersJSON: `{"q":"s"}`,  // 9 chars => total 25 / 4 = 6
	}
	got := td.EstimateTokens()
	if got != 6 {
		t.Errorf("EstimateTokens() = %d, want 6", got)
	}
}

// ---------------------------------------------------------------------------
// normalization.go — NormalizationPattern.String() and intToStr coverage
// ---------------------------------------------------------------------------

func TestNormalizationPattern_String(t *testing.T) {
	tests := []struct {
		p    NormalizationPattern
		want string
	}{
		{PatternNone, "none"},
		{PatternMalformedToolCall, "malformed_tool_call"},
		{PatternNarratedToolUse, "narrated_tool_use"},
		{PatternEmptyAction, "empty_action"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("NormalizationPattern(%d).String() = %q, want %q", tt.p, got, tt.want)
		}
	}
}

func TestIntToStr(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{42, "42"},
		{100, "100"},
		{999, "999"},
	}
	for _, tt := range tests {
		if got := intToStr(tt.n); got != tt.want {
			t.Errorf("intToStr(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestNormalizationRetryPrompt_None(t *testing.T) {
	p := NormalizationRetryPrompt(PatternNone, 5)
	if p != "" {
		t.Errorf("PatternNone should return empty, got %q", p)
	}
}

// ---------------------------------------------------------------------------
// trace.go — Annotate on nil span, Finish with active span, EndSpan no-op
// ---------------------------------------------------------------------------

func TestTraceRecorder_AnnotateNilSpan(t *testing.T) {
	tr := NewTraceRecorder()
	// No span active — Annotate should not panic.
	tr.Annotate("key", "val")
}

func TestTraceRecorder_EndSpanNoOp(t *testing.T) {
	tr := NewTraceRecorder()
	// No span active — EndSpan should not panic.
	tr.EndSpan("ok")
	trace := tr.Finish("t1", "test")
	if len(trace.Stages) != 0 {
		t.Errorf("expected 0 stages, got %d", len(trace.Stages))
	}
}

func TestTraceRecorder_FinishWithActiveSpan(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("active")
	tr.Annotate("foo", "bar")
	// Finish should auto-close the active span.
	trace := tr.Finish("t2", "api")
	if len(trace.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(trace.Stages))
	}
	if trace.Stages[0].Name != "active" {
		t.Errorf("stage name = %q, want 'active'", trace.Stages[0].Name)
	}
	if trace.Stages[0].Outcome != "ok" {
		t.Errorf("outcome = %q, want 'ok'", trace.Stages[0].Outcome)
	}
	if trace.Stages[0].Metadata["foo"] != "bar" {
		t.Errorf("metadata missing foo annotation")
	}
}

// ---------------------------------------------------------------------------
// guard_fallback.go — summarizeQuery long string
// ---------------------------------------------------------------------------

func TestSummarizeQuery_Short(t *testing.T) {
	q := "What is 2+2?"
	got := summarizeQuery(q)
	if got != q {
		t.Errorf("short query should be returned as-is, got %q", got)
	}
}

func TestSummarizeQuery_Long(t *testing.T) {
	// Build a query longer than 100 chars.
	long := ""
	for len(long) < 120 {
		long += "abcdefghijklmnop "
	}
	got := summarizeQuery(long)
	if len(got) != 103 { // 100 + "..."
		t.Errorf("expected truncated to 103, got len=%d", len(got))
	}
}

func TestFallbackResponse_WithUserMessage(t *testing.T) {
	sess := NewSession("s1", "agent1", "TestBot")
	sess.AddUserMessage("What is the weather?")
	result := fallbackResponse(sess, "rejected content", "test_guard", "unsafe")
	if result.Content == "" {
		t.Error("fallback should produce content")
	}
	if result.SessionID != "s1" {
		t.Errorf("session ID = %q, want s1", result.SessionID)
	}
}

func TestFallbackResponse_NoUserMessage(t *testing.T) {
	sess := NewSession("s2", "agent1", "TestBot")
	result := fallbackResponse(sess, "rejected", "guard_x", "reason_y")
	if result.Content == "" {
		t.Error("fallback should produce content")
	}
}

// ---------------------------------------------------------------------------
// guard_registry.go — Chain with different presets
// ---------------------------------------------------------------------------

func TestGuardRegistry_ChainNone(t *testing.T) {
	r := NewDefaultGuardRegistry()
	chain := r.Chain(GuardSetNone)
	if chain.Len() != 0 {
		t.Errorf("GuardSetNone chain should be empty, got %d", chain.Len())
	}
}

func TestGuardRegistry_ChainStream(t *testing.T) {
	r := NewDefaultGuardRegistry()
	chain := r.Chain(GuardSetStream)
	if chain.Len() == 0 {
		t.Error("GuardSetStream chain should have guards")
	}
}

func TestGuardRegistry_ChainUnknown(t *testing.T) {
	r := NewDefaultGuardRegistry()
	chain := r.Chain(GuardSetPreset(99))
	if chain.Len() != 0 {
		t.Errorf("unknown preset should return empty chain, got %d", chain.Len())
	}
}

// ---------------------------------------------------------------------------
// guards_config_protection.go — Name coverage
// ---------------------------------------------------------------------------

func TestConfigProtectionGuard_Name(t *testing.T) {
	g := &ConfigProtectionGuard{}
	if g.Name() != "config_protection" {
		t.Errorf("Name() = %q", g.Name())
	}
}

// ---------------------------------------------------------------------------
// guards_financial_truth.go — Name coverage
// ---------------------------------------------------------------------------

func TestFinancialActionTruthGuard_Name(t *testing.T) {
	g := &FinancialActionTruthGuard{}
	if g.Name() != "financial_action_truth" {
		t.Errorf("Name() = %q", g.Name())
	}
}

// ---------------------------------------------------------------------------
// guard_context.go — ApplyFullWithContext edge cases
// ---------------------------------------------------------------------------

func TestApplyFullWithContext_NilContextCoverage(t *testing.T) {
	// Covers the basic guard path when ctx is nil (guards fall back to Check).
	chain := NewGuardChain(&EmptyResponseGuard{}, &LowValueParrotingGuard{})
	result := chain.ApplyFullWithContext("hello world", nil)
	if result.Content != "hello world" {
		t.Errorf("content should pass through, got %q", result.Content)
	}
}

func TestApplyFullWithContext_ContentRewrite(t *testing.T) {
	// PersonalityIntegrityGuard rewrites foreign identity.
	// "as an ai developed by" is one of the markers; include a second
	// sentence so the guard strips the marker sentence and returns cleaned content.
	chain := NewGuardChain(&PersonalityIntegrityGuard{})
	ctx := &GuardContext{}
	input := "As an AI developed by OpenAI, I follow instructions. Here is your answer."
	result := chain.ApplyFullWithContext(input, ctx)
	if result.Content == input {
		t.Error("personality guard should rewrite foreign identity content")
	}
}

func TestApplyFullWithContext_RetryShortCircuit(t *testing.T) {
	chain := NewGuardChain(&LowValueParrotingGuard{}, &EmptyResponseGuard{})
	ctx := &GuardContext{UserPrompt: "test"}
	result := chain.ApplyFullWithContext("ready", ctx)
	if !result.RetryRequested {
		t.Error("should request retry for low-value response")
	}
}

// ---------------------------------------------------------------------------
// pipeline_stages.go — session resolution, shortcuts, followup expansion
// ---------------------------------------------------------------------------

func TestResolveSession_SessionFromBody_NewSession(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	ctx := context.Background()

	// No SessionID provided => creates new session.
	sess, err := pipe.resolveSession(ctx, Config{SessionResolution: SessionFromBody}, Input{
		AgentID:  "a1",
		Platform: "test",
	})
	if err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if sess == nil || sess.ID == "" {
		t.Fatal("session should be created")
	}
}

func TestResolveSession_PropagatesWorkspaceAndAllowedPaths(t *testing.T) {
	store := testutil.TempStore(t)
	allowed := []string{"/opt/vault", "/data/shared"}
	pipe := New(PipelineDeps{
		Store:        store,
		Workspace:    "/home/agent/workspace",
		AllowedPaths: allowed,
	})
	ctx := context.Background()

	// New session should inherit workspace + allowed paths from pipeline.
	sess, err := pipe.resolveSession(ctx, Config{SessionResolution: SessionFromBody}, Input{
		AgentID:  "a1",
		Platform: "test",
	})
	if err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if sess.Workspace != "/home/agent/workspace" {
		t.Errorf("workspace = %q, want /home/agent/workspace", sess.Workspace)
	}
	if len(sess.AllowedPaths) != 2 || sess.AllowedPaths[0] != "/opt/vault" {
		t.Errorf("AllowedPaths = %v, want [/opt/vault /data/shared]", sess.AllowedPaths)
	}
}

func TestResolveSession_SessionFromBody_ExistingSession(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	ctx := context.Background()

	// Pre-create a session.
	sessID := db.NewID()
	_, err := store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		sessID, "a1", "test:"+sessID)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Provide existing SessionID.
	sess, err := pipe.resolveSession(ctx, Config{SessionResolution: SessionFromBody}, Input{
		SessionID: sessID,
		AgentID:   "a1",
		Platform:  "test",
	})
	if err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if sess.ID != sessID {
		t.Errorf("session ID = %s, want %s", sess.ID, sessID)
	}
}

func TestResolveSession_SessionDedicated(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	ctx := context.Background()

	sess, err := pipe.resolveSession(ctx, Config{SessionResolution: SessionDedicated}, Input{
		AgentID:  "a1",
		Platform: "test",
	})
	if err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if sess == nil {
		t.Fatal("should create dedicated session")
	}
}

func TestResolveSession_SessionFromChannel(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	ctx := context.Background()

	// First call: no existing session => creates one.
	sess1, err := pipe.resolveSession(ctx, Config{SessionResolution: SessionFromChannel}, Input{
		AgentID:  "a1",
		Platform: "discord",
		ChatID:   "ch123",
	})
	if err != nil {
		t.Fatalf("resolveSession (new): %v", err)
	}
	if sess1 == nil {
		t.Fatal("should create channel session")
	}

	// Second call: same channel => reuse session.
	sess2, err := pipe.resolveSession(ctx, Config{SessionResolution: SessionFromChannel}, Input{
		AgentID:  "a1",
		Platform: "discord",
		ChatID:   "ch123",
	})
	if err != nil {
		t.Fatalf("resolveSession (reuse): %v", err)
	}
	if sess2.ID != sess1.ID {
		t.Errorf("should reuse session: got %s, want %s", sess2.ID, sess1.ID)
	}
}

func TestResolveSession_UnknownMode(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	_, err := pipe.resolveSession(context.Background(), Config{SessionResolution: SessionResolutionMode(99)}, Input{})
	if err == nil {
		t.Error("unknown mode should return error")
	}
}

func TestLoadSession_WithMessages(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	ctx := context.Background()

	sessID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		sessID, "a1", "test:"+sessID)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, 'user', ?)`,
		db.NewID(), sessID, "Hello")
	_, _ = store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, 'assistant', ?)`,
		db.NewID(), sessID, "Hi there")
	_, _ = store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, 'system', ?)`,
		db.NewID(), sessID, "System prompt")

	sess, err := pipe.loadSession(ctx, Input{SessionID: sessID, AgentID: "a1", AgentName: "Bot"})
	if err != nil {
		t.Fatalf("loadSession: %v", err)
	}
	if sess.MessageCount() != 3 {
		t.Errorf("message count = %d, want 3", sess.MessageCount())
	}
}

func TestLoadSessionByID(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	ctx := context.Background()

	sessID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		sessID, "a1", "test:"+sessID)

	sess, err := pipe.loadSessionByID(ctx, sessID, Input{AgentID: "a1", AgentName: "Bot"})
	if err != nil {
		t.Fatalf("loadSessionByID: %v", err)
	}
	if sess.ID != sessID {
		t.Errorf("session ID = %s, want %s", sess.ID, sessID)
	}
}

func TestCreateSessionWithScope(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	ctx := context.Background()

	sess, err := pipe.createSessionWithScope(ctx, Input{AgentID: "a1", AgentName: "Bot"}, "discord:ch123")
	if err != nil {
		t.Fatalf("createSessionWithScope: %v", err)
	}
	if sess == nil || sess.ID == "" {
		t.Fatal("should create session")
	}
}

func TestExpandShortFollowup_Short(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")
	sess.AddUserMessage("What is Go?")
	sess.AddAssistantMessage("Go is a programming language.", nil)

	got := pipe.expandShortFollowup(sess, "ok")
	if got == "ok" {
		t.Error("short followup should be expanded when prior context exists")
	}
}

func TestExpandShortFollowup_Long(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")
	got := pipe.expandShortFollowup(sess, "This is a much longer message that should not be expanded")
	if got != "This is a much longer message that should not be expanded" {
		t.Error("long message should not be expanded")
	}
}

func TestExpandShortFollowup_NoPrior(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")
	got := pipe.expandShortFollowup(sess, "ok")
	// No prior assistant content, TurnCount() is 0 => no expansion.
	if got != "ok" {
		t.Errorf("should not expand when no turns, got %q", got)
	}
}

func TestExpandShortFollowup_LongPriorTruncated(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")
	sess.AddUserMessage("test")
	// Build a prior message longer than 200 chars.
	longPrior := ""
	for len(longPrior) < 250 {
		longPrior += "abcdefghijklmnop "
	}
	sess.AddAssistantMessage(longPrior, nil)

	got := pipe.expandShortFollowup(sess, "yes")
	if got == "yes" {
		t.Error("should expand short followup with truncated prior")
	}
}

func TestTryShortcut_WhoAreYou(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "TestBot")

	for _, input := range []string{"who are you", "who are you?", "what are you?"} {
		result := pipe.tryShortcut(context.Background(), sess, input, false, "test")
		if result == nil {
			t.Errorf("tryShortcut(%q) should match", input)
			continue
		}
		if result.Content == "" {
			t.Errorf("tryShortcut(%q) content empty", input)
		}
	}
}

func TestTryShortcut_Acknowledgments(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")

	acks := []string{"ok", "okay", "thanks", "thank you", "got it", "understood", "k", "ty"}
	for _, ack := range acks {
		result := pipe.tryShortcut(context.Background(), sess, ack, false, "test")
		if result == nil {
			t.Errorf("tryShortcut(%q) should match acknowledgment", ack)
		}
	}
}

func TestTryShortcut_Help(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")

	for _, h := range []string{"help", "/help"} {
		result := pipe.tryShortcut(context.Background(), sess, h, false, "test")
		if result == nil {
			t.Errorf("tryShortcut(%q) should match help", h)
		}
	}
}

func TestTryShortcut_NoMatch(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")

	result := pipe.tryShortcut(context.Background(), sess, "Tell me about quantum physics", false, "test")
	if result != nil {
		t.Error("should not match non-shortcut")
	}
}

// ---------------------------------------------------------------------------
// pipeline.go — guardOutcome, storeTrace
// ---------------------------------------------------------------------------

func TestGuardOutcome_NoGuards(t *testing.T) {
	pipe := &Pipeline{}
	outcome := &Outcome{Content: "hello"}
	result := pipe.guardOutcome(Config{GuardSet: GuardSetFull}, nil, outcome)
	if result.Content != "hello" {
		t.Error("no guards should pass through")
	}
}

func TestGuardOutcome_WithGuards(t *testing.T) {
	pipe := &Pipeline{guards: DefaultGuardChain()}
	outcome := &Outcome{Content: "valid response"}
	result := pipe.guardOutcome(Config{GuardSet: GuardSetFull}, nil, outcome)
	if result == nil {
		t.Fatal("should return outcome")
	}
}

func TestGuardOutcome_GuardSetNone(t *testing.T) {
	pipe := &Pipeline{guards: DefaultGuardChain()}
	outcome := &Outcome{Content: "anything"}
	result := pipe.guardOutcome(Config{GuardSet: GuardSetNone}, nil, outcome)
	if result.Content != "anything" {
		t.Error("GuardSetNone should pass through")
	}
}

func TestGuardOutcome_NilOutcome(t *testing.T) {
	pipe := &Pipeline{guards: DefaultGuardChain()}
	result := pipe.guardOutcome(Config{GuardSet: GuardSetFull}, nil, nil)
	if result != nil {
		t.Error("nil outcome should return nil")
	}
}

func TestStoreTrace_NilStore(t *testing.T) {
	pipe := &Pipeline{store: nil}
	tr := NewTraceRecorder()
	tr.BeginSpan("test")
	tr.EndSpan("ok")
	// Should not panic.
	pipe.storeTrace(context.Background(), tr, "s1", "m1", "api")
}

func TestStoreTrace_WithStore(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := &Pipeline{store: store}
	tr := NewTraceRecorder()
	tr.BeginSpan("test")
	tr.EndSpan("ok")
	// Best-effort, should not panic even if pipeline_traces table exists.
	pipe.storeTrace(context.Background(), tr, "s1", "m1", "api")
}

// ---------------------------------------------------------------------------
// pipeline_stages.go — prepareStreamInference
// ---------------------------------------------------------------------------

func TestPrepareStreamInference_WithStreamer(t *testing.T) {
	pipe := &Pipeline{
		streamer: &testStreamPreparer{},
	}
	sess := NewSession("s1", "a1", "Bot")
	outcome, err := pipe.prepareStreamInference(context.Background(), Config{}, sess, "m1")
	if err != nil {
		t.Fatalf("prepareStreamInference: %v", err)
	}
	if !outcome.Stream {
		t.Error("should be a stream outcome")
	}
	if outcome.StreamRequest == nil {
		t.Error("StreamRequest should be set")
	}
}

func TestPrepareStreamInference_NoStreamer(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")
	outcome, err := pipe.prepareStreamInference(context.Background(), Config{}, sess, "m1")
	if err != nil {
		t.Fatalf("prepareStreamInference: %v", err)
	}
	if !outcome.Stream {
		t.Error("should be a stream outcome")
	}
	if outcome.StreamRequest != nil {
		t.Error("StreamRequest should be nil without streamer")
	}
}

type testStreamPreparer struct{}

func (s *testStreamPreparer) PrepareStream(_ context.Context, _ *Session) (*llm.Request, error) {
	return &llm.Request{Stream: true}, nil
}

func TestPrepareStreamInference_StreamerError(t *testing.T) {
	pipe := &Pipeline{
		streamer: &errorStreamPreparer{},
	}
	sess := NewSession("s1", "a1", "Bot")
	_, err := pipe.prepareStreamInference(context.Background(), Config{}, sess, "m1")
	if err == nil {
		t.Error("should return error when streamer fails")
	}
}

type errorStreamPreparer struct{}

func (s *errorStreamPreparer) PrepareStream(_ context.Context, _ *Session) (*llm.Request, error) {
	return nil, errors.New("stream prep failed")
}

// ---------------------------------------------------------------------------
// pipeline_stages.go — runStandardInference edge cases
// ---------------------------------------------------------------------------

func TestRunStandardInference_NoExecutor(t *testing.T) {
	pipe := &Pipeline{}
	sess := NewSession("s1", "a1", "Bot")
	_, err := pipe.runStandardInference(context.Background(), Config{}, sess, "m1", "t1")
	if err == nil {
		t.Error("should fail without executor")
	}
}

func TestRunStandardInference_WithGuards(t *testing.T) {
	store := testutil.TempStore(t)
	bgw := core.NewBackgroundWorker(4)
	pipe := &Pipeline{
		store:    store,
		executor: &stubExec{response: "test response"},
		guards:   DefaultGuardChain(),
		bgWorker: bgw,
	}
	sess := NewSession("s1", "a1", "Bot")
	outcome, err := pipe.runStandardInference(context.Background(), Config{GuardSet: GuardSetFull}, sess, "m1", "t1")
	if err != nil {
		t.Fatalf("runStandardInference: %v", err)
	}
	if outcome.Content == "" {
		t.Error("should produce content")
	}
}

type stubExec struct {
	response string
}

func (s *stubExec) RunLoop(_ context.Context, sess *Session) (string, int, error) {
	sess.AddAssistantMessage(s.response, nil)
	return s.response, 1, nil
}

type errorExec struct{}

func (e *errorExec) RunLoop(_ context.Context, _ *Session) (string, int, error) {
	return "", 0, errors.New("inference exploded")
}

func TestRunStandardInference_ExecutorError(t *testing.T) {
	store := testutil.TempStore(t)
	bgw := core.NewBackgroundWorker(4)
	pipe := &Pipeline{
		store:    store,
		executor: &errorExec{},
		bgWorker: bgw,
	}
	sess := NewSession("s1", "a1", "Bot")
	_, err := pipe.runStandardInference(context.Background(), Config{}, sess, "m1", "t1")
	if err == nil {
		t.Error("should propagate executor error")
	}
}

// ---------------------------------------------------------------------------
// bot_commands.go — cmdMemory, cmdMemoryStats, cmdMemorySearch
// ---------------------------------------------------------------------------

func TestCmdMemory_NoArgs(t *testing.T) {
	h := NewBotCommandHandler(nil, nil)
	sess := NewSession("s1", "a1", "Bot")
	result, _ := h.cmdMemory(context.Background(), "", sess)
	if result == nil || result.Content == "" {
		t.Error("empty args should return usage help")
	}
}

func TestCmdMemory_NoStore(t *testing.T) {
	h := NewBotCommandHandler(nil, nil) // no store
	sess := NewSession("s1", "a1", "Bot")
	result, _ := h.cmdMemory(context.Background(), "stats", sess)
	if result == nil {
		t.Fatal("should return result")
	}
	if result.Content == "" {
		t.Error("should say memory unavailable")
	}
}

func TestCmdMemory_UnknownSubcommand(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")
	result, _ := h.cmdMemory(context.Background(), "foobar", sess)
	if result == nil {
		t.Fatal("should return result")
	}
}

func TestCmdMemory_SearchNoQuery(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")
	result, _ := h.cmdMemory(context.Background(), "search", sess)
	if result == nil {
		t.Fatal("should return usage")
	}
}

func TestCmdMemory_SearchEmpty(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")
	result, _ := h.cmdMemory(context.Background(), "search   ", sess)
	if result == nil {
		t.Fatal("should return usage for empty query")
	}
}

func TestCmdMemoryStats(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")
	result, err := h.cmdMemoryStats(context.Background(), sess)
	if err != nil {
		t.Fatalf("cmdMemoryStats: %v", err)
	}
	if result == nil || result.Content == "" {
		t.Error("should return stats")
	}
}

func TestCmdMemorySearch(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")

	// Insert some test data.
	_, _ = store.ExecContext(context.Background(),
		`INSERT INTO episodic_memory (id, agent_id, content, embedding, event_type)
		 VALUES (?, ?, ?, zeroblob(16), 'test')`,
		db.NewID(), "a1", "Hello world test data for search")

	result, err := h.cmdMemorySearch(context.Background(), "test", sess)
	if err != nil {
		t.Fatalf("cmdMemorySearch: %v", err)
	}
	if result == nil || result.Content == "" {
		t.Error("should return search results")
	}
}

func TestCmdMemorySearch_NoResults(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")
	result, err := h.cmdMemorySearch(context.Background(), "xyznonexistent", sess)
	if err != nil {
		t.Fatalf("cmdMemorySearch: %v", err)
	}
	if result == nil {
		t.Fatal("should return result even with no matches")
	}
}

// Test the /memory command through TryHandle to cover the full dispatch path.
func TestBotCommand_MemoryStats_ViaTryHandle(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")
	result, ok := h.TryHandle(context.Background(), "/memory stats", sess)
	if !ok {
		t.Error("should match /memory command")
	}
	if result == nil || result.Content == "" {
		t.Error("should return memory stats")
	}
}

func TestBotCommand_MemorySearch_ViaTryHandle(t *testing.T) {
	store := testutil.TempStore(t)
	h := NewBotCommandHandler(nil, store)
	sess := NewSession("s1", "a1", "Bot")
	result, ok := h.TryHandle(context.Background(), "/memory search hello", sess)
	if !ok {
		t.Error("should match /memory command")
	}
	if result == nil || result.Content == "" {
		t.Error("should return search results")
	}
}

// ---------------------------------------------------------------------------
// pipeline.go — Run with streaming mode and unknown inference mode
// ---------------------------------------------------------------------------

func TestPipeline_Run_StreamingMode(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExec{response: "test"},
		Streamer: &testStreamPreparer{},
	})
	cfg := PresetStreaming()
	input := Input{
		Content: "Hello streaming",
		AgentID: "test-agent",
	}
	outcome, err := pipe.Run(context.Background(), cfg, input)
	if err != nil {
		t.Fatalf("Run streaming: %v", err)
	}
	if !outcome.Stream {
		t.Error("should be streaming outcome")
	}
}

func TestPipeline_Run_UnknownInferenceMode(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store, Executor: &stubExec{response: "test"}})
	cfg := PresetAPI()
	cfg.InferenceMode = InferenceMode(99)
	_, err := pipe.Run(context.Background(), cfg, Input{
		Content: "Hello",
		AgentID: "test",
	})
	if err == nil {
		t.Error("unknown inference mode should return error")
	}
}

func TestPipeline_Run_SkillMatch(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store: store,
		Skills: &testSkillMatcher{
			result: &Outcome{Content: "skill handled it"},
		},
	})
	cfg := PresetAPI()
	cfg.SkillFirstEnabled = true
	cfg.AuthorityMode = AuthorityAPIKey // resolves to creator

	input := Input{
		Content: "Use my skill",
		AgentID: "test",
	}
	outcome, err := pipe.Run(context.Background(), cfg, input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Content != "skill handled it" {
		t.Errorf("expected skill response, got %q", outcome.Content)
	}
}

type testSkillMatcher struct {
	result *Outcome
}

func (s *testSkillMatcher) TryMatch(_ context.Context, _ *Session, _ string) *Outcome {
	return s.result
}

// ---------------------------------------------------------------------------
// pipeline.go — Run with executor error
// ---------------------------------------------------------------------------

func TestPipeline_Run_ExecutorError(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &errorExec{},
	})
	cfg := PresetAPI()
	_, err := pipe.Run(context.Background(), cfg, Input{
		Content: "Tell me something complex",
		AgentID: "test",
	})
	if err == nil {
		t.Error("executor error should propagate")
	}
}

// ---------------------------------------------------------------------------
// decomposition.go — min function edge cases
// ---------------------------------------------------------------------------

func TestMin_EdgeCases(t *testing.T) {
	// min is only used in decomposition.go; exercise the a < b path.
	if min(5, 10) != 5 {
		t.Error("min(5,10) != 5")
	}
	if min(10, 5) != 5 {
		t.Error("min(10,5) != 5")
	}
	if min(5, 5) != 5 {
		t.Error("min(5,5) != 5")
	}
}

// ---------------------------------------------------------------------------
// guards_quality.go — edge cases for tokenOverlapRatio, commonPrefixRatio
// ---------------------------------------------------------------------------

func TestTokenOverlapRatio_EmptyInputs(t *testing.T) {
	if tokenOverlapRatio("", "hello") != 0 {
		t.Error("empty a should return 0")
	}
	if tokenOverlapRatio("hello", "") != 0 {
		t.Error("empty b should return 0")
	}
}

func TestCommonPrefixRatio_EmptyInputs(t *testing.T) {
	if commonPrefixRatio("", "hello") != 0 {
		t.Error("empty a should return 0")
	}
	if commonPrefixRatio("hello", "") != 0 {
		t.Error("empty b should return 0")
	}
	if commonPrefixRatio("", "") != 0 {
		t.Error("both empty should return 0")
	}
}

// ---------------------------------------------------------------------------
// guards_truthfulness.go — PersonalityIntegrityGuard.CheckWithContext
// ---------------------------------------------------------------------------

func TestPersonalityIntegrityGuard_CheckWithContext(t *testing.T) {
	g := &PersonalityIntegrityGuard{}
	ctx := &GuardContext{}
	result := g.CheckWithContext("Normal response about coding", ctx)
	if !result.Passed {
		t.Error("normal response should pass")
	}

	result = g.CheckWithContext("As an AI developed by OpenAI, I can help", ctx)
	if result.Passed {
		t.Error("foreign identity should be caught by Check, forwarded by CheckWithContext")
	}
}

// ---------------------------------------------------------------------------
// config.go — ResolveAuthority with claim
// ---------------------------------------------------------------------------

func TestResolveAuthority_WithClaim_Allowlisted(t *testing.T) {
	// Allowlisted → Peer (DefaultClaimSecurityConfig.AllowlistAuthority)
	claim := &ChannelClaimContext{
		SenderID:            "user123",
		SenderInAllowlist:   true,
		AllowlistConfigured: true,
	}
	got := ResolveAuthority(AuthorityChannel, claim)
	if got != core.AuthorityPeer {
		t.Errorf("allowlisted sender = %v, want peer", got)
	}
}

func TestResolveAuthority_WithClaim_TrustedSender(t *testing.T) {
	// Trusted sender → Creator (DefaultClaimSecurityConfig.TrustedAuthority)
	claim := &ChannelClaimContext{
		SenderID:         "user456",
		TrustedSenderIDs: []string{"user456"},
	}
	got := ResolveAuthority(AuthorityChannel, claim)
	if got != core.AuthorityCreator {
		t.Errorf("trusted sender = %v, want creator", got)
	}
}

func TestResolveAuthority_WithClaim_Untrusted(t *testing.T) {
	claim := &ChannelClaimContext{
		SenderID: "unknown",
	}
	got := ResolveAuthority(AuthorityChannel, claim)
	if got != core.AuthorityExternal {
		t.Errorf("untrusted sender = %v, want external", got)
	}
}

func TestResolveAuthority_SelfGen(t *testing.T) {
	got := ResolveAuthority(AuthoritySelfGen, nil)
	if got != core.AuthoritySelfGenerated {
		t.Errorf("SelfGen = %v, want self_generated", got)
	}
}
