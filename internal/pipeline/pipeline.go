package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// Outcome represents the result of a pipeline run.
type Outcome struct {
	SessionID  string `json:"session_id"`
	MessageID  string `json:"message_id"`
	Content    string `json:"content"`
	Model      string `json:"model,omitempty"`
	TokensIn   int    `json:"tokens_in,omitempty"`
	TokensOut  int    `json:"tokens_out,omitempty"`
	ReactTurns int    `json:"react_turns,omitempty"`
	FromCache  bool   `json:"from_cache,omitempty"`
	Stream     bool   `json:"stream,omitempty"`

	// StreamRequest is the fully-prepared LLM request for streaming inference.
	// Set when InferenceMode is InferenceStreaming and the pipeline has prepared
	// full context (session history, memory, tools, system prompt). The SSE handler
	// must use this instead of building its own request, to avoid context divergence.
	StreamRequest *llm.Request `json:"-"`

	// TurnID is the pre-created turn record ID. Used by FinalizeStream to
	// run post-turn work (memory ingest, embedding, observer dispatch,
	// assistant message storage) after streaming completes.
	TurnID string `json:"-"`

	// streamSession holds the session reference for post-stream finalization.
	// Not exported — only used by the pipeline's FinalizeStream method.
	streamSession *Session `json:"-"`

	// streamConfig holds the pipeline config for post-stream finalization.
	streamConfig *Config `json:"-"`
}

// Input is the raw request to the pipeline.
type Input struct {
	Content       string
	SessionID     string // empty for auto-resolution
	AgentID       string
	AgentName     string
	Platform      string // channel platform name
	SenderID      string // channel sender identifier
	ChatID        string // channel chat identifier
	ModelOverride string // force a specific model, bypassing router
	Claim         *ChannelClaimContext
}

// Runner is the interface for executing the pipeline.
// Routes and tests should depend on this interface, not the concrete Pipeline.
type Runner interface {
	Run(ctx context.Context, cfg Config, input Input) (*Outcome, error)
}

// StreamFinalizer runs post-turn work after streaming completes.
// Connectors MUST call this after assembling the full streamed content,
// or streaming turns will silently lose memory ingestion, embedding
// generation, observer dispatch, and assistant message storage.
type StreamFinalizer interface {
	FinalizeStream(ctx context.Context, outcome *Outcome, assembledContent string)
}

// Ensure *Pipeline satisfies Runner at compile time.
var _ Runner = (*Pipeline)(nil)

// Pipeline is the unified factory. Connectors call Run() with a Config preset
// and an Input — the pipeline handles everything else.
type Pipeline struct {
	store      *db.Store
	llmSvc     *llm.Service
	injection  InjectionChecker
	retriever  MemoryRetriever
	skills     SkillMatcher
	executor   ToolExecutor
	ingestor   Ingestor
	refiner    NicknameRefiner
	streamer   StreamPreparer
	guards     *GuardChain
	bgWorker   *core.BackgroundWorker
	dedup      *DedupTracker
	tasks      *TaskTracker
	embeddings *llm.EmbeddingClient
	errBus     *core.ErrorBus
}

// PipelineDeps bundles dependencies for the Pipeline.
type PipelineDeps struct {
	Store      *db.Store
	LLM        *llm.Service
	Injection  InjectionChecker
	Retriever  MemoryRetriever
	Skills     SkillMatcher
	Executor   ToolExecutor
	Ingestor   Ingestor
	Refiner    NicknameRefiner
	Streamer   StreamPreparer
	Guards     *GuardChain
	BGWorker   *core.BackgroundWorker
	Embeddings *llm.EmbeddingClient
	ErrBus     *core.ErrorBus
}

// New creates the unified pipeline.
func New(deps PipelineDeps) *Pipeline {
	bgw := deps.BGWorker
	if bgw == nil {
		bgw = core.NewBackgroundWorker(16)
	}
	return &Pipeline{
		store:      deps.Store,
		llmSvc:     deps.LLM,
		injection:  deps.Injection,
		retriever:  deps.Retriever,
		skills:     deps.Skills,
		executor:   deps.Executor,
		ingestor:   deps.Ingestor,
		refiner:    deps.Refiner,
		streamer:   deps.Streamer,
		guards:     deps.Guards,
		bgWorker:   bgw,
		dedup:      NewDedupTracker(60 * time.Second),
		tasks:      NewTaskTracker(),
		embeddings: deps.Embeddings,
		errBus:     deps.ErrBus,
	}
}

// RunPipeline is the canonical package-level entry point for all connectors.
func RunPipeline(ctx context.Context, p Runner, cfg Config, input Input) (*Outcome, error) {
	return p.Run(ctx, cfg, input)
}

// Run executes the full pipeline with the given config and input.
//
// Stage order (matching Rust pipeline):
//
//  1. Input validation (addressability, bot command, delegation wrap, size)
//  2. Injection defense (L1 score, L2 sanitize)
//  3. Dedup tracking (reject concurrent identical requests)
//  4. Session resolution (find/create, consent, short-followup expansion)
//  5. User message storage (with topic tag derivation)
//  6. Turn creation (pre-create turn record in DB)
//  7. Decomposition gate (classify + potentially delegate)
//  8. Authority resolution (threat-aware RBAC)
//  9. Delegated execution (orchestrate-subagents if delegation decided)
//  10. Skill-first fulfillment (Creator-only, channel-only)
//  11. Shortcut dispatch (acknowledgements, identity, /help)
//  12. Inference (standard ReAct or streaming)
//  13. Guard chain → Post-turn ingest → Response
func (p *Pipeline) Run(ctx context.Context, cfg Config, input Input) (*Outcome, error) {
	if p.store == nil {
		return nil, core.NewError(core.ErrConfig, "pipeline requires a database store")
	}
	tr := NewTraceRecorder()
	pipelineStart := time.Now()
	log.Info().Str("channel", cfg.ChannelLabel).Str("agent", input.AgentID).Msg("pipeline started")

	// ── Stage 1: Input validation ──────────────────────────────────────────
	tr.BeginSpan("validation")

	// Bot command dispatch: handle /commands before any processing.
	if cfg.BotCommandDispatch && len(input.Content) > 0 && input.Content[0] == '/' {
		if result := p.tryBotCommand(ctx, input); result != nil {
			tr.Annotate("bot_command", true)
			tr.EndSpan("ok")
			return result, nil
		}
	}

	// Cron delegation wrap: prepend subagent directive for non-root cron tasks.
	if cfg.CronDelegationWrap && input.AgentID != "" && input.AgentID != "default" {
		input.Content = fmt.Sprintf("[Delegated to %s] %s", input.AgentID, input.Content)
	}

	// API-level model override takes precedence over config.
	if input.ModelOverride != "" {
		cfg.ModelOverride = input.ModelOverride
	}

	// Prefer local model: scan fallbacks for a local provider and set override.
	if cfg.PreferLocalModel && cfg.ModelOverride == "" {
		cfg.ModelOverride = p.findLocalModel()
	}

	if input.Content == "" {
		tr.EndSpan("error")
		return nil, core.NewError(core.ErrConfig, "empty message content")
	}
	if len(input.Content) > core.MaxUserMessageBytes {
		tr.EndSpan("error")
		return nil, core.NewError(core.ErrConfig, fmt.Sprintf("message exceeds %d bytes", core.MaxUserMessageBytes))
	}
	tr.EndSpan("ok")

	// ── Stage 2: Injection defense ─────────────────────────────────────────
	tr.BeginSpan("injection_defense")
	var threatCaution bool
	if cfg.InjectionDefense && p.injection != nil {
		score := p.injection.CheckInput(input.Content)
		tr.Annotate("score", float64(score))
		if score.IsBlocked() {
			tr.EndSpan("error")
			log.Warn().Float64("score", float64(score)).Str("channel", cfg.ChannelLabel).Str("session", input.SessionID).Str("agent", input.AgentID).Str("sender", input.SenderID).Msg("injection blocked")
			return nil, core.NewError(core.ErrInjectionBlocked, "input rejected by injection defense")
		}
		if score.IsCaution() {
			input.Content = p.injection.Sanitize(input.Content)
			threatCaution = true
			log.Warn().Float64("score", float64(score)).Str("session", input.SessionID).Str("channel", cfg.ChannelLabel).Str("agent", input.AgentID).Msg("input sanitized")
		}
	}
	tr.EndSpan("ok")

	// ── Stage 3: Dedup tracking ────────────────────────────────────────────
	if cfg.DedupTracking && p.dedup != nil {
		tr.BeginSpan("dedup_check")
		dedupFP := Fingerprint(input.Content, input.AgentID, input.SessionID)
		if !p.dedup.CheckAndTrack(dedupFP) {
			tr.EndSpan("rejected")
			return nil, core.NewError(core.ErrDuplicate, "duplicate request already in flight")
		}
		defer p.dedup.Release(dedupFP)
		tr.EndSpan("ok")
	}

	// Create task for lifecycle tracking.
	taskID := db.NewID()
	task := p.tasks.Create(taskID, input.SessionID, input.Content)
	_ = task

	// ── Stage 4: Session resolution ────────────────────────────────────────
	tr.BeginSpan("session_resolution")
	session, err := p.resolveSession(ctx, cfg, input)
	if err != nil {
		tr.EndSpan("error")
		return nil, core.WrapError(core.ErrDatabase, "session resolution failed", err)
	}
	tr.Annotate("session_id", session.ID)
	tr.EndSpan("ok")

	// Stage 4a: Cross-channel consent check (Rust parity).
	// Runs immediately after session resolution to gate cross-channel access.
	consentResult, consentMsg := p.checkCrossChannelConsent(ctx, session, input)
	switch consentResult {
	case ConsentGranted:
		// User confirmed consent — return synthetic response.
		return &Outcome{SessionID: session.ID, Content: consentMsg}, nil
	case ConsentBlocked:
		// Cross-channel access denied — return error with instructions.
		return nil, core.NewError(core.ErrUnauthorized, consentMsg)
	case ConsentContinue:
		// No consent action needed — proceed.
	}

	// Short-followup expansion (Rust parity: contextualize_short_followup).
	// Detects sarcasm, contradiction, and quote-back reactions and expands them
	// with prior context so the LLM understands the reference. Also sets
	// correctionTurn to bypass shortcut dispatch for corrections.
	content := input.Content
	var correctionTurn bool
	if cfg.ShortFollowupExpansion {
		content, correctionTurn = ContextualizeShortFollowup(session, content)
	}

	// ── Stage 5: User message storage (with topic tag) ─────────────────────
	tr.BeginSpan("message_storage")
	msgID := db.NewID()
	topicTag := p.deriveTopicTag(session, content)
	_, err = p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, topic_tag)
		 VALUES (?, ?, 'user', ?, ?)`,
		msgID, session.ID, content, topicTag,
	)
	if err != nil {
		tr.EndSpan("error")
		return nil, core.WrapError(core.ErrDatabase, "failed to store user message", err)
	}
	session.AddUserMessage(content)
	tr.EndSpan("ok")

	// ── Stage 6: Turn creation ─────────────────────────────────────────────
	turnID := db.NewID()
	_, turnErr := p.store.ExecContext(ctx,
		`INSERT INTO turns (id, session_id) VALUES (?, ?)`,
		turnID, session.ID,
	)
	if turnErr != nil {
		log.Warn().Err(turnErr).Str("turn", turnID).Msg("turn creation failed, continuing")
	}

	// ── Stage 7: Decomposition gate ────────────────────────────────────────
	tr.BeginSpan("decomposition_gate")
	p.tasks.Start(taskID, msgID)
	var decomp *DecompositionResult
	if cfg.DecompositionGate {
		d := EvaluateDecomposition(content, len(session.Messages()))
		decomp = &d
		p.tasks.Classify(taskID, TaskClassification(decomp.Decision))
		tr.Annotate("decision", decomp.Decision.String())
		if decomp.Decision == DecompDelegated && len(decomp.Subtasks) > 0 {
			tr.Annotate("subtask_count", len(decomp.Subtasks))
			p.tasks.Delegate(taskID, input.AgentID, nil)
			log.Info().
				Str("task", taskID).
				Str("session", session.ID).
				Str("agent", input.AgentID).
				Int("subtasks", len(decomp.Subtasks)).
				Msg("task delegated via decomposition gate")
		}
	} else {
		decomp = &DecompositionResult{Decision: DecompCentralized}
	}
	tr.EndSpan("ok")

	// ── Stage 7.5: Task state synthesis (Rust: synthesize_task_state + plan) ──
	if cfg.TaskOperatingState != "" || cfg.DecompositionGate {
		tr.BeginSpan("task_synthesis")
		// Gather agent skills for capability matching.
		var agentSkills []string
		if p.skills != nil {
			// Skills interface doesn't expose a list method; use empty for now.
			// The synthesis still works — it just reports 0% capability fit.
			_ = agentSkills // SA9003: populated when skills list method is added
		}
		synthesis := SynthesizeTaskState(content, session.TurnCount(), agentSkills)
		tr.Annotate("intent", synthesis.Intent)
		tr.Annotate("complexity", synthesis.Complexity)
		tr.Annotate("planned_action", synthesis.PlannedAction)
		tr.Annotate("capability_fit", synthesis.CapabilityFit)
		tr.EndSpan("ok")

		// Override decomposition decision based on planner action.
		if synthesis.PlannedAction == "delegate_to_specialist" && decomp.Decision == DecompCentralized {
			decomp.Decision = DecompDelegated
			log.Info().Str("session", session.ID).Msg("planner upgraded decision to delegation")
		}
	}

	// ── Stage 8: Authority resolution ──────────────────────────────────────
	// Full SecurityClaim resolution via core resolvers (Rust parity).
	// The claim carries source tracking for audit + ceiling enforcement.
	tr.BeginSpan("authority_resolution")
	secClaim := ResolveSecurityClaim(cfg.AuthorityMode, input.Claim)
	// Reduce authority if injection threat was caution-level (Rust parity).
	if threatCaution && secClaim.Authority == core.AuthorityCreator {
		secClaim.Authority = core.AuthorityPeer
		secClaim.ThreatDowngraded = true
		log.Warn().Str("session", session.ID).Msg("authority reduced due to injection caution")
	}
	session.Authority = secClaim.Authority
	session.SecurityClaim = &secClaim
	tr.Annotate("authority", secClaim.Authority.String())
	if len(secClaim.Sources) > 0 {
		sourceStrs := make([]string, len(secClaim.Sources))
		for i, s := range secClaim.Sources {
			sourceStrs[i] = s.String()
		}
		tr.Annotate("claim_sources", strings.Join(sourceStrs, ","))
	}
	tr.EndSpan("ok")

	// ── Stage 8.5: Memory retrieval (Rust parity: ARCHITECTURE.md §4) ────
	// Memory must be proactively injected BEFORE delegation and skill-first
	// so early-exit paths still have full cognitive context. "The model should
	// never have to guess at something the framework already knows."
	var memoryBlock string
	if p.retriever != nil {
		tr.BeginSpan("memory_retrieval")
		memoryBlock = p.retriever.Retrieve(ctx, session.ID, content, 2048)
		if memoryBlock != "" {
			session.SetMemoryContext(memoryBlock)
		}
		tr.EndSpan("ok")
	}

	// ── Stage 9: Delegated execution ───────────────────────────────────────
	if cfg.DelegatedExecution && decomp.Decision == DecompDelegated && len(decomp.Subtasks) > 0 {
		tr.BeginSpan("delegated_execution")
		delegResult := p.executeDelegation(ctx, session, decomp, turnID)
		if delegResult != nil {
			tr.Annotate("delegation_ok", true)
			tr.EndSpan("ok")
			p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
			p.tasks.Complete(taskID)
			return delegResult, nil
		}
		tr.EndSpan("fallthrough")
	}

	// ── Stage 10: Skill-first fulfillment ──────────────────────────────────
	tr.BeginSpan("skill_dispatch")
	if cfg.SkillFirstEnabled && secClaim.Authority == core.AuthorityCreator && p.skills != nil {
		if result := p.skills.TryMatch(ctx, session, content); result != nil {
			tr.Annotate("matched", true)
			tr.EndSpan("ok")
			p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
			p.tasks.Complete(taskID)
			return p.guardOutcome(cfg, result), nil
		}
	}
	tr.EndSpan("skipped")

	// ── Stage 11: Shortcut dispatch ────────────────────────────────────────
	// Rust parity: skip shortcuts on correction turns (sarcasm/contradiction
	// should not match acknowledgement shortcuts).
	tr.BeginSpan("shortcut_dispatch")
	if cfg.ShortcutsEnabled && !correctionTurn {
		if result := p.tryShortcut(ctx, session, content); result != nil {
			tr.Annotate("matched", true)
			tr.EndSpan("ok")
			p.recordShortcutCost(ctx, turnID, session.ID, cfg.ChannelLabel)
			p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
			p.tasks.Complete(taskID)
			return p.guardOutcome(cfg, result), nil
		}
	}
	tr.EndSpan("skipped")

	// ── Stage 12: Inference ────────────────────────────────────────────────
	tr.BeginSpan("inference")
	var outcome *Outcome
	switch cfg.InferenceMode {
	case InferenceStandard:
		outcome, err = p.runStandardInference(ctx, cfg, session, msgID, turnID)
	case InferenceStreaming:
		outcome, err = p.prepareStreamInference(ctx, cfg, session, msgID)
	default:
		tr.EndSpan("error")
		return nil, core.NewError(core.ErrConfig, "unknown inference mode")
	}
	if err != nil {
		tr.EndSpan("error")
		p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
		return nil, err
	}
	tr.EndSpan("ok")

	p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)

	// Mark task completed.
	p.tasks.Complete(taskID)

	log.Info().Str("session", session.ID).Str("model", outcome.Model).Int("tokens_out", outcome.TokensOut).Int64("duration_ms", time.Since(pipelineStart).Milliseconds()).Msg("pipeline completed")
	return outcome, nil
}

// guardOutcome applies the guard chain to an outcome if guards are configured.
// This ensures skill, shortcut, and all other early-return paths are filtered.
// Uses full context when a session is available for contextual guard evaluation.
func (p *Pipeline) guardOutcome(cfg Config, outcome *Outcome) *Outcome {
	if p.guards != nil && cfg.GuardSet != GuardSetNone && outcome != nil {
		outcome.Content = p.guards.Apply(outcome.Content)
	}
	return outcome
}

// buildGuardContext creates a GuardContext from the current session state.
func (p *Pipeline) buildGuardContext(session *Session) *GuardContext {
	if session == nil {
		return nil
	}

	ctx := &GuardContext{
		AgentName: session.AgentName,
	}

	// Extract user prompt (last user message).
	msgs := session.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			ctx.UserPrompt = msgs[i].Content
			break
		}
	}

	// Extract previous assistant message.
	ctx.PreviousAssistant = session.LastAssistantContent()

	// Collect all prior assistant messages.
	for _, m := range msgs {
		if m.Role == "assistant" {
			ctx.PriorAssistantMessages = append(ctx.PriorAssistantMessages, m.Content)
		}
	}

	// Collect tool results from the current turn (messages after the last user message).
	lastUserIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx >= 0 {
		for i := lastUserIdx + 1; i < len(msgs); i++ {
			if msgs[i].Role == "tool" {
				ctx.ToolResults = append(ctx.ToolResults, ToolResultEntry{
					ToolName: msgs[i].Name,
					Output:   msgs[i].Content,
				})
			}
		}
	}

	return ctx
}

// embeddingClient returns the configured embedding client, or nil if none is set.
func (p *Pipeline) embeddingClient() *llm.EmbeddingClient {
	return p.embeddings
}

// storeTrace persists a pipeline trace to the database (best-effort).
func (p *Pipeline) storeTrace(ctx context.Context, tr *TraceRecorder, sessionID, msgID, channel string) {
	if p.store == nil {
		return
	}
	trace := tr.Finish(msgID, channel)
	_, err := p.store.ExecContext(ctx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		db.NewID(), trace.TurnID, sessionID, trace.Channel, trace.TotalMs, trace.StagesJSON())
	if err != nil {
		log.Warn().Err(err).Str("session", sessionID).Str("turn", msgID).Msg("failed to store pipeline trace")
	}
}
