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

	// Trace artifacts (not serialized to clients — used for pipeline-internal persistence).
	reactTrace      *ReactTrace      `json:"-"`
	inferenceParams *InferenceParams `json:"-"`

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
	// synthesis is hoisted out of the if-block so DecideRetrievalStrategy
	// can use it in Stage 8.5 (H10: stage separation).
	var synthesis TaskSynthesis
	if cfg.TaskOperatingState != "" || cfg.DecompositionGate {
		tr.BeginSpan("task_synthesis")
		// Gather agent skills for capability matching.
		var agentSkills []string
		if p.skills != nil {
			// Skills interface doesn't expose a list method; use empty for now.
			// The synthesis still works — it just reports 0% capability fit.
			_ = agentSkills // SA9003: populated when skills list method is added
		}
		synthesis = SynthesizeTaskState(content, session.TurnCount(), agentSkills)

		// Structured trace annotations (Rust: annotate_task_state_trace).
		AnnotateTaskStateTrace(tr, synthesis)
		tr.EndSpan("ok")

		// Map planned action to gate decision (Rust: map_planned_action).
		gateDecision := MapPlannedAction(synthesis, decomp)
		switch gateDecision {
		case ActionGateDelegate:
			if decomp.Decision == DecompCentralized {
				decomp.Decision = DecompDelegated
				log.Info().Str("session", session.ID).Msg("planner upgraded decision to delegation")
			}
		case ActionGateSpecialistPropose:
			if decomp.Decision == DecompCentralized {
				decomp.Decision = DecompSpecialistProposal
				log.Info().Str("session", session.ID).Msg("planner upgraded decision to specialist proposal")
			}
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
	//
	// H10: Retrieval strategy is decided as a separate function, decoupling
	// retrieval policy from retrieval execution.
	var memoryBlock string
	retrievalStrat := DecideRetrievalStrategy(synthesis, session.TurnCount(), 2048)
	if p.retriever != nil && retrievalStrat.Strategy != "none" {
		tr.BeginSpan("memory_retrieval")
		memoryBlock = p.retriever.Retrieve(ctx, session.ID, content, retrievalStrat.Budget)
		if memoryBlock != "" {
			session.SetMemoryContext(memoryBlock)
		}
		fragmentCount := 0
		if memoryBlock != "" {
			fragmentCount = strings.Count(memoryBlock, "---") + 1
		}

		// Personality reinforcement on early turns (Rust parity).
		// On turns 1-3, memory retrieval returns empty because IngestTurn
		// runs as post-turn background work — no episodic/semantic memories
		// exist yet. Without reinforcement, the model sees only the system
		// prompt personality and deprioritizes it as boilerplate. Rust solves
		// this by seeding an initial memory orientation; we inject a system
		// note that explicitly directs the model to embody its identity.
		if memoryBlock == "" && session.TurnCount() <= 3 {
			personalityBoost := "[Identity Reinforcement] This is an early turn in the conversation. " +
				"Your personality, voice, and behavioral directives from the system prompt are " +
				"your PRIMARY guide for tone, style, and approach. Embody them fully — do not " +
				"fall back to generic AI assistant behavior. Respond as the character defined in " +
				"your system prompt, not as a generic helpful assistant."
			session.AddSystemMessage(personalityBoost)
			tr.Annotate("personality_boost", true)
		}

		AnnotateRetrievalStrategy(tr, retrievalStrat.Strategy, retrievalStrat.Budget, fragmentCount)
		tr.EndSpan("ok")
	}

	// ── Stage 9: Delegated execution ───────────────────────────────────────
	// Rust parity (H8): delegation results are either returned directly
	// (when complete) or threaded back into the inference context as an
	// initial tool observation so the main agent can incorporate them.
	var delegationResult string // Threaded to inference if non-empty.
	if cfg.DelegatedExecution && decomp.Decision == DecompDelegated && len(decomp.Subtasks) > 0 {
		tr.BeginSpan("delegated_execution")
		delegOutcome := p.executeDelegation(ctx, session, decomp, turnID)
		if delegOutcome != nil {
			AnnotateDelegationTrace(tr, input.AgentID, len(decomp.Subtasks), "decomposition_gate")
			if delegOutcome.Complete {
				// Delegation fully satisfied the request — return directly.
				tr.Annotate("delegation_complete", true)
				tr.EndSpan("ok")
				p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
				p.tasks.Complete(taskID)
				return &Outcome{
					SessionID:  session.ID,
					MessageID:  msgID,
					Content:    delegOutcome.Content,
					ReactTurns: delegOutcome.Turns,
				}, nil
			}
			// Partial/failed delegation — thread result back to inference.
			// Rust: seeds tool_results_acc with ("orchestrate-subagents", result).
			delegationResult = delegOutcome.Content
			tr.Annotate("delegation_complete", false)
			tr.Annotate("delegation_threaded", true)
			log.Info().Str("session", session.ID).Int("quality", delegOutcome.Quality.Score).Msg("delegation incomplete, threading to inference")
		}
		tr.EndSpan("fallthrough")
	}

	// ── Stage 10: Skill-first fulfillment ──────────────────────────────────
	tr.BeginSpan("skill_dispatch")
	if skillResult := p.trySkillFirst(ctx, cfg, secClaim.Authority, session, content); skillResult != nil {
		tr.Annotate("matched", true)
		tr.EndSpan("ok")
		p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
		p.tasks.Complete(taskID)
		return p.guardOutcome(cfg, skillResult), nil
	}
	tr.EndSpan("skipped")

	// ── Stage 11: Shortcut dispatch ────────────────────────────────────────
	// Rust parity: correction_turn is passed through to the shortcut handler
	// system so individual handlers can decide (e.g., AcknowledgementShortcut
	// skips on correction turns, IdentityShortcut does not).
	tr.BeginSpan("shortcut_dispatch")
	if cfg.ShortcutsEnabled {
		if result := p.tryShortcut(ctx, session, content, correctionTurn, cfg.ChannelLabel); result != nil {
			tr.Annotate("matched", true)
			tr.EndSpan("ok")
			p.recordShortcutCost(ctx, turnID, session.ID, cfg.ChannelLabel)
			p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
			p.tasks.Complete(taskID)
			return p.guardOutcome(cfg, result), nil
		}
	}
	tr.EndSpan("skipped")

	// ── Stage 11.5: Cache check (Rust: check_cache) ──────────────────────
	if cfg.CacheEnabled {
		tr.BeginSpan("cache_check")
		if hit := p.CheckCache(content); hit != nil {
			tr.Annotate("cache_hit", true)
			tr.Annotate("cache_model", hit.Model)
			tr.EndSpan("ok")

			// Apply cache-specific guards (reduced set).
			cacheOutcome := &Outcome{
				SessionID: session.ID,
				MessageID: msgID,
				Content:   hit.Content,
				Model:     hit.Model,
				FromCache: true,
				inferenceParams: &InferenceParams{
					FromCache:    true,
					ModelActual:  hit.Model,
				},
			}
			if p.guards != nil && cfg.CacheGuardSet != GuardSetNone {
				cacheOutcome.Content = p.guards.Apply(cacheOutcome.Content)
			}

			// Persist cached assistant response to session_messages.
			// Without this, subsequent turns lose the cached exchange from
			// their history, causing context drift and response looping.
			assistantMsgID := db.NewID()
			topicTag := p.deriveTopicTag(session, cacheOutcome.Content)
			_, cacheStoreErr := p.store.ExecContext(ctx,
				`INSERT INTO session_messages (id, session_id, role, content, topic_tag)
				 VALUES (?, ?, 'assistant', ?, ?)`,
				assistantMsgID, session.ID, cacheOutcome.Content, topicTag,
			)
			if cacheStoreErr != nil {
				log.Error().Err(cacheStoreErr).Str("session", session.ID).Msg("failed to store cached assistant message")
			}
			// Also update in-memory session so guard context and dedup
			// see the cached response within this request lifecycle.
			session.AddAssistantMessage(cacheOutcome.Content, nil)

			p.storeTraceWithArtifacts(ctx, tr, session.ID, msgID, cfg.ChannelLabel, cacheOutcome)
			p.tasks.Complete(taskID)
			return cacheOutcome, nil
		}
		tr.EndSpan("miss")
	}

	// ── Stage 11.75: Prepare for inference (Rust: prepare_for_inference) ──
	p.PrepareForInference(ctx, session, memoryBlock, cfg.BudgetTier)

	// Thread delegation result into inference context (Rust parity H8).
	// Rust seeds tool_results_acc with ("orchestrate-subagents", result)
	// so the LLM sees prior delegation work as an initial observation.
	if delegationResult != "" {
		session.AddSystemMessage(fmt.Sprintf(
			"[Prior delegation result from orchestrate-subagents]\n%s\n"+
				"[Incorporate the above delegation output into your response. "+
				"If it's incomplete, supplement with your own reasoning.]",
			delegationResult,
		))
	}

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

	// ── Stage 12.5: Cache store (Rust: store_in_cache) ────────────────────
	if cfg.CacheEnabled && outcome != nil && !outcome.Stream && outcome.Content != "" {
		p.bgWorker.Submit("storeCache", func(_ context.Context) {
			p.StoreInCache(content, outcome.Content, outcome.Model)
		})
	}

	// Empty response guard: if inference produced nothing (all models failed,
	// guard chain stripped everything, or deadline hit), provide a fallback
	// rather than sending an empty message to the channel.
	if outcome != nil && strings.TrimSpace(outcome.Content) == "" {
		outcome.Content = "I wasn't able to formulate a response right now. Could you try again?"
		log.Warn().Str("session", session.ID).Msg("pipeline produced empty content — injected fallback")
	}

	p.storeTraceWithArtifacts(ctx, tr, session.ID, msgID, cfg.ChannelLabel, outcome)

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
// If outcome is provided, also persists react_trace_json and inference_params_json.
func (p *Pipeline) storeTrace(ctx context.Context, tr *TraceRecorder, sessionID, msgID, channel string) {
	p.storeTraceWithArtifacts(ctx, tr, sessionID, msgID, channel, nil)
}

// storeTraceWithArtifacts persists a pipeline trace along with optional
// ReactTrace and InferenceParams artifacts from the inference stage.
func (p *Pipeline) storeTraceWithArtifacts(ctx context.Context, tr *TraceRecorder, sessionID, msgID, channel string, outcome *Outcome) {
	if p.store == nil {
		return
	}
	trace := tr.Finish(msgID, channel)

	var reactJSON, paramsJSON *string
	if outcome != nil {
		if outcome.reactTrace != nil {
			s := outcome.reactTrace.JSON()
			reactJSON = &s
		}
		if outcome.inferenceParams != nil {
			s := outcome.inferenceParams.JSON()
			paramsJSON = &s
		}
	}

	_, err := p.store.ExecContext(ctx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json, react_trace_json, inference_params_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		db.NewID(), trace.TurnID, sessionID, trace.Channel, trace.TotalMs, trace.StagesJSON(), reactJSON, paramsJSON)
	if err != nil {
		log.Warn().Err(err).Str("session", sessionID).Str("turn", msgID).Msg("failed to store pipeline trace")
	}
}
