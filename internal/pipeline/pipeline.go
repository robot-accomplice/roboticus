package pipeline

import (
	"context"
	"fmt"
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
}

// Input is the raw request to the pipeline.
type Input struct {
	Content   string
	SessionID string // empty for auto-resolution
	AgentID   string
	AgentName string
	Platform  string // channel platform name
	SenderID  string // channel sender identifier
	ChatID    string // channel chat identifier
	Claim     *ChannelClaimContext
}

// Runner is the interface for executing the pipeline.
// Routes and tests should depend on this interface, not the concrete Pipeline.
type Runner interface {
	Run(ctx context.Context, cfg Config, input Input) (*Outcome, error)
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
	}
}

// RunPipeline is the canonical package-level entry point for all connectors.
func RunPipeline(ctx context.Context, p Runner, cfg Config, input Input) (*Outcome, error) {
	return p.Run(ctx, cfg, input)
}

// Run executes the full pipeline with the given config and input.
//
// Stage order:
//  1. Input validation
//  2. Injection defense (L1 score, L2 sanitize)
//  3. Dedup tracking (reject concurrent identical requests)
//  4. Session resolution
//  5. Short-followup expansion
//  6. User message storage
//  7. Authority resolution
//  8. Decomposition gate (classify + potentially delegate)
//  9. Skill-first fulfillment
//  10. Shortcut dispatch -> Inference
//  11. Guard chain -> Post-turn ingest -> Response
func (p *Pipeline) Run(ctx context.Context, cfg Config, input Input) (*Outcome, error) {
	tr := NewTraceRecorder()
	pipelineStart := time.Now()
	log.Info().Str("channel", cfg.ChannelLabel).Str("agent", input.AgentID).Msg("pipeline started")

	// Stage 1: Input validation.
	tr.BeginSpan("validation")
	if input.Content == "" {
		tr.EndSpan("error")
		return nil, core.NewError(core.ErrConfig, "empty message content")
	}
	if len(input.Content) > core.MaxUserMessageBytes {
		tr.EndSpan("error")
		return nil, core.NewError(core.ErrConfig, fmt.Sprintf("message exceeds %d bytes", core.MaxUserMessageBytes))
	}
	tr.EndSpan("ok")

	// Stage 2: Injection defense.
	tr.BeginSpan("injection_defense")
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
			log.Warn().Float64("score", float64(score)).Str("session", input.SessionID).Str("channel", cfg.ChannelLabel).Str("agent", input.AgentID).Msg("input sanitized")
		}
	}
	tr.EndSpan("ok")

	// Stage 3: Dedup tracking — reject concurrent identical requests.
	var dedupFP string
	if cfg.DedupTracking && p.dedup != nil {
		tr.BeginSpan("dedup_check")
		dedupFP = Fingerprint(input.Content, input.AgentID, input.SessionID)
		if !p.dedup.CheckAndTrack(dedupFP) {
			tr.EndSpan("rejected")
			return nil, core.NewError(core.ErrConfig, "duplicate request already in flight")
		}
		defer p.dedup.Release(dedupFP)
		tr.EndSpan("ok")
	}

	// Create task for lifecycle tracking.
	taskID := db.NewID()
	task := p.tasks.Create(taskID, input.SessionID, input.Content)
	_ = task

	// Stage 4: Session resolution.
	tr.BeginSpan("session_resolution")
	session, err := p.resolveSession(ctx, cfg, input)
	if err != nil {
		tr.EndSpan("error")
		return nil, core.WrapError(core.ErrDatabase, "session resolution failed", err)
	}
	tr.Annotate("session_id", session.ID)
	tr.EndSpan("ok")

	// Stage 4: Short-followup expansion.
	content := input.Content
	if cfg.ShortFollowupExpansion {
		content = p.expandShortFollowup(session, content)
	}

	// Stage 5: User message storage.
	tr.BeginSpan("message_storage")
	msgID := db.NewID()
	_, err = p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content)
		 VALUES (?, ?, 'user', ?)`,
		msgID, session.ID, content,
	)
	if err != nil {
		tr.EndSpan("error")
		return nil, core.WrapError(core.ErrDatabase, "failed to store user message", err)
	}
	session.AddUserMessage(content)
	tr.EndSpan("ok")

	// Stage 6: Authority resolution.
	tr.BeginSpan("authority_resolution")
	authority := ResolveAuthority(cfg.AuthorityMode, input.Claim)
	session.Authority = authority
	tr.Annotate("authority", authority.String())
	tr.EndSpan("ok")

	// Stage 7: Decomposition gate — classify and potentially delegate.
	tr.BeginSpan("decomposition_gate")
	p.tasks.Start(taskID, msgID)
	decomp := EvaluateDecomposition(content, len(session.Messages()))
	p.tasks.Classify(taskID, TaskClassification(decomp.Decision))
	tr.Annotate("decision", decomp.Decision.String())
	if decomp.Decision == DecompDelegated && len(decomp.Subtasks) > 0 {
		tr.Annotate("subtask_count", len(decomp.Subtasks))
		// Record the delegation in task state. The executor will handle
		// actual subagent dispatch if the agent has orchestration tools.
		p.tasks.Delegate(taskID, input.AgentID, nil)
		log.Info().
			Str("task", taskID).
			Str("session", session.ID).
			Str("agent", input.AgentID).
			Int("subtasks", len(decomp.Subtasks)).
			Msg("task delegated via decomposition gate")
	}
	tr.EndSpan("ok")

	// Stage 8: Skill-first fulfillment.
	tr.BeginSpan("skill_dispatch")
	if cfg.SkillFirstEnabled && authority == core.AuthorityCreator && p.skills != nil {
		if result := p.skills.TryMatch(ctx, session, content); result != nil {
			tr.Annotate("matched", true)
			tr.EndSpan("ok")
			p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
			return p.guardOutcome(cfg, result), nil
		}
	}
	tr.EndSpan("skipped")

	// Stage 8: Shortcut dispatch.
	tr.BeginSpan("shortcut_dispatch")
	if cfg.ShortcutsEnabled {
		if result := p.tryShortcut(ctx, session, content); result != nil {
			tr.Annotate("matched", true)
			tr.EndSpan("ok")
			p.storeTrace(ctx, tr, session.ID, msgID, cfg.ChannelLabel)
			return p.guardOutcome(cfg, result), nil
		}
	}
	tr.EndSpan("skipped")

	// Stage 9: Inference.
	tr.BeginSpan("inference")
	var outcome *Outcome
	switch cfg.InferenceMode {
	case InferenceStandard:
		outcome, err = p.runStandardInference(ctx, cfg, session, msgID)
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
