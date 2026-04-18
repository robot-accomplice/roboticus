package pipeline

import (
	"context"
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
	NoCache       bool   // skip semantic cache (used by exercise/baseline)
	NoEscalate    bool   // disable routing escalation/fallback contamination for benchmark paths
	Claim         *ChannelClaimContext
}

// Runner is the interface for executing the pipeline.
// Routes and tests should depend on this interface, not the concrete Pipeline.
type Runner interface {
	Run(ctx context.Context, cfg Config, input Input) (*Outcome, error)
}

// DashboardNotifier publishes typed events to the dashboard WebSocket bus.
// The api.EventBus satisfies this interface — defined here to avoid circular imports.
type DashboardNotifier interface {
	PublishEvent(eventType string, data any)
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
	store           *db.Store
	llmSvc          *llm.Service
	injection       InjectionChecker
	retriever       MemoryRetriever
	skills          SkillMatcher
	executor        ToolExecutor
	ingestor        Ingestor
	refiner         NicknameRefiner
	streamer        StreamPreparer
	pruner          ToolPruner
	guards          *GuardChain
	guardRegistry   *GuardRegistry
	usePresetGuards bool
	bgWorker        *core.BackgroundWorker
	dedup           *DedupTracker
	tasks           *TaskTracker
	botCmds         *BotCommandHandler
	embeddings      *llm.EmbeddingClient
	// certaintyClass is the embedding-backed semantic claim certainty
	// classifier (M6 follow-on). Built once per pipeline so the corpus
	// embedding cost is amortised across every turn.
	certaintyClass   *llm.SemanticClassifier
	errBus           *core.ErrorBus
	dashboard        DashboardNotifier
	workspace        string   // agent workspace root — propagated to sessions for tool sandbox
	allowedPaths     []string // extra paths outside workspace that tools may access
	cacheTTL         time.Duration
	checkpointPolicy CheckpointPolicy
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
	Pruner     ToolPruner
	Guards     *GuardChain
	BGWorker   *core.BackgroundWorker
	Embeddings *llm.EmbeddingClient
	ErrBus     *core.ErrorBus
	Dashboard  DashboardNotifier

	// Sandbox: workspace root and extra allowed paths propagated to every session.
	Workspace    string
	AllowedPaths []string
	CacheTTL     time.Duration

	// Optional lifecycle policy. Nil means use package defaults so tests and
	// ad-hoc callers keep the historical behavior unless they opt in.
	CheckpointPolicy *CheckpointPolicy
}

// CheckpointPolicy controls periodic context checkpoint behavior.
type CheckpointPolicy struct {
	Enabled       bool
	IntervalTurns int
}

// New creates the unified pipeline.
func New(deps PipelineDeps) *Pipeline {
	bgw := deps.BGWorker
	if bgw == nil {
		bgw = core.NewBackgroundWorker(16)
	}
	cp := CheckpointPolicy{
		Enabled:       true,
		IntervalTurns: checkpointIntervalTurns,
	}
	cacheTTL := llm.DefaultCacheConfig().TTL
	if deps.CacheTTL > 0 {
		cacheTTL = deps.CacheTTL
	}
	if deps.CheckpointPolicy != nil {
		cp.Enabled = deps.CheckpointPolicy.Enabled
		if deps.CheckpointPolicy.IntervalTurns > 0 {
			cp.IntervalTurns = deps.CheckpointPolicy.IntervalTurns
		}
	}
	return &Pipeline{
		store:            deps.Store,
		llmSvc:           deps.LLM,
		injection:        deps.Injection,
		retriever:        deps.Retriever,
		skills:           deps.Skills,
		executor:         deps.Executor,
		ingestor:         deps.Ingestor,
		refiner:          deps.Refiner,
		streamer:         deps.Streamer,
		pruner:           deps.Pruner,
		guards:           deps.Guards,
		guardRegistry:    NewDefaultGuardRegistry(),
		usePresetGuards:  deps.Guards == nil,
		bgWorker:         bgw,
		dedup:            NewDedupTracker(60 * time.Second),
		tasks:            NewTaskTracker(),
		embeddings:       deps.Embeddings,
		certaintyClass:   NewClaimCertaintyClassifier(deps.Embeddings),
		errBus:           deps.ErrBus,
		dashboard:        deps.Dashboard,
		botCmds:          NewBotCommandHandler(deps.LLM, deps.Store),
		workspace:        deps.Workspace,
		allowedPaths:     deps.AllowedPaths,
		cacheTTL:         cacheTTL,
		checkpointPolicy: cp,
	}
}

func (p *Pipeline) guardsForPreset(preset GuardSetPreset) *GuardChain {
	if preset == GuardSetNone {
		return nil
	}
	if p.usePresetGuards && p.guardRegistry != nil {
		chain := p.guardRegistry.Chain(preset)
		if chain == nil || chain.Len() == 0 {
			return nil
		}
		return chain
	}
	if p.guards != nil {
		return p.guards
	}
	if p.guardRegistry != nil {
		chain := p.guardRegistry.Chain(preset)
		if chain == nil || chain.Len() == 0 {
			return nil
		}
		return chain
	}
	return nil
}

// RunPipeline is the canonical package-level entry point for all connectors.
func RunPipeline(ctx context.Context, p Runner, cfg Config, input Input) (*Outcome, error) {
	return p.Run(ctx, cfg, input)
}

// stageLivenessThreshold is the minimum stage duration before the
// watchdog starts logging "stage X has been running for Y" warnings.
// Set generously: most stages complete in milliseconds, but
// stage_inference legitimately takes 30s+ for cold-start LLM loads
// (especially Ollama 32B-class models). A 20s threshold catches real
// hangs without spamming on legitimate slow stages — the first probe
// fires at 20s, the second at 30s, etc.
const stageLivenessThreshold = 20 * time.Second

// stageLivenessProbeInterval is how often the watchdog checks the
// in-flight stage. Short enough that operators see updates roughly
// every 10s during a hang; long enough that the watchdog adds no
// observable load on healthy runs (which complete sub-second).
const stageLivenessProbeInterval = 10 * time.Second

// runStageWatchdog logs a warning when a single pipeline stage runs
// longer than stageLivenessThreshold. Exits when done is closed
// (set up by Pipeline.Run via defer) or when the pipeline context
// is cancelled.
//
// The function is intentionally simple: it does not attempt to
// interrupt the stage. Operators get loud signal; the surrounding
// timeout / context-cancel infrastructure handles the actual
// recovery. This keeps the watchdog out of the hot path and out of
// the recovery decisions (which are already nuanced — a cold-start
// LLM is "slow" but not "stuck").
func (p *Pipeline) runStageWatchdog(ctx context.Context, tr *TraceRecorder, done <-chan struct{}) {
	if tr == nil {
		return
	}
	ticker := time.NewTicker(stageLivenessProbeInterval)
	defer ticker.Stop()

	// Track the previous span so we don't re-log the same warning
	// every probe. We log the FIRST time a stage exceeds the
	// threshold, then again every probe interval thereafter — that
	// gives operators a fresh "still hung" signal without blasting
	// duplicates.
	var lastLoggedSpan string
	var lastLogTime time.Time

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			cs := tr.CurrentSpan()
			if cs.Name == "" {
				lastLoggedSpan = ""
				continue
			}
			if cs.Duration < stageLivenessThreshold {
				continue
			}
			// Log on first detection OR after another probe
			// interval has elapsed. Prevents both spam (every
			// probe) and silence (only on transitions).
			if cs.Name != lastLoggedSpan || time.Since(lastLogTime) >= stageLivenessProbeInterval {
				log.Warn().
					Str("stage", cs.Name).
					Dur("running_for", cs.Duration).
					Msg("pipeline stage running longer than expected — possible cold-start latency or hang")
				lastLoggedSpan = cs.Name
				lastLogTime = time.Now()
			}
		}
	}
}

// dashNotify publishes a typed event to the dashboard if a notifier is configured.
func (p *Pipeline) dashNotify(eventType string, data any) {
	if p.dashboard != nil {
		p.dashboard.PublishEvent(eventType, data)
	}
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

	pc := &pipelineContext{
		cfg:   cfg,
		input: input,
		start: time.Now(),
		tr:    NewTraceRecorder(),
	}
	log.Info().Str("channel", pc.cfg.ChannelLabel).Str("agent", pc.input.AgentID).Msg("pipeline started")
	p.dashNotify("agent_working", map[string]string{
		"agent_id": pc.input.AgentID, "workstation": "llm", "skill": "inference",
	})

	// v1.0.6: per-stage liveness watchdog. If any single stage runs
	// longer than stageLivenessThreshold, a goroutine logs which
	// stage is in-flight and how long it's been running. This turns
	// the cold-start hang reported in the v1.0.5 fresh-state soak
	// (where the first turn started but never completed and the
	// operator had no signal about which stage was stalling) into
	// an actionable log line: operators can now identify the stuck
	// stage from the daemon's running output rather than having to
	// kill -QUIT and parse a goroutine dump.
	//
	// The watchdog runs at stageLivenessProbeInterval and re-checks
	// the in-flight span; if it's the SAME span as the previous
	// probe AND it's exceeded the threshold, log it. Polls are
	// cheap (one RWMutex.RLock + tiny snapshot copy) so the
	// instrumentation has no measurable steady-state cost.
	watchdogDone := make(chan struct{})
	go p.runStageWatchdog(ctx, pc.tr, watchdogDone)
	defer close(watchdogDone)

	// Stages 1-2: validation + injection defense.
	if err := p.stageValidation(ctx, pc); err != nil {
		return nil, err
	}
	if err := p.stageInjectionDefense(ctx, pc); err != nil {
		return nil, err
	}

	// Stage 3: dedup + task creation. Dedup release is deferred here in Run()
	// because stage methods must not defer across the call boundary.
	if err := p.stageDedup(ctx, pc); err != nil {
		return nil, err
	}
	if pc.dedupFP != "" {
		defer p.dedup.Release(pc.dedupFP)
	}

	// Stage 4: session resolution, consent, bot command, short-followup.
	if out, err := p.stageSessionResolution(ctx, pc); out != nil || err != nil {
		return out, err
	}

	// Stage 5-6: message storage + turn creation.
	if err := p.stageMessageStorage(ctx, pc); err != nil {
		return nil, err
	}
	p.stageTurnCreation(ctx, pc)

	// Stage 7 + 7.5: decomposition gate + task synthesis.
	p.stageDecomposition(ctx, pc)

	// Stage 8 + 8.5: authority + memory retrieval.
	p.stageAuthority(ctx, pc)
	p.stageMemoryRetrieval(ctx, pc)

	// Stage 9: delegated execution (may return early).
	if out, err := p.stageDelegation(ctx, pc); out != nil || err != nil {
		return out, err
	}

	// Stage 10-11.65: skill-first, shortcut, request shaping, cache.
	if out, err := p.stageSkillFirst(ctx, pc); out != nil || err != nil {
		return out, err
	}
	if out, err := p.stageShortcut(ctx, pc); out != nil || err != nil {
		return out, err
	}

	// Stage 11.6: tool pruning (query-time semantic ranking + budget).
	p.stageToolPruning(ctx, pc)

	// Stage 11.65: hippocampus summary (database surface ambient note).
	p.stageHippocampusSummary(ctx, pc)

	// Stage 11.7: cache check on the shaped pre-inference surface.
	if out, err := p.stageCacheCheck(ctx, pc); out != nil || err != nil {
		return out, err
	}

	// Stage 11.75: prepare inference context.
	p.stagePrepareInference(ctx, pc)

	// Stage 12: inference.
	outcome, err := p.stageInference(ctx, pc)
	if err != nil {
		return nil, err
	}

	// Stage 12.5: post-inference (cache store, empty guard, trace, task complete).
	p.stagePostInference(ctx, pc, outcome)

	log.Info().Str("session", pc.session.ID).Str("model", outcome.Model).Int("tokens_out", outcome.TokensOut).Int64("duration_ms", time.Since(pc.start).Milliseconds()).Msg("pipeline completed")
	p.dashNotify("stream_end", map[string]any{
		"session_id": pc.session.ID, "model": outcome.Model,
		"tokens_in": outcome.TokensIn, "tokens_out": outcome.TokensOut,
	})
	p.dashNotify("agent_idle", map[string]string{"agent_id": pc.input.AgentID})

	return outcome, nil
}

// guardOutcome applies the guard chain to an outcome if guards are configured.
// This ensures skill, shortcut, and all other early-return paths are filtered.
// Uses full context when a session is available for contextual guard evaluation.
func (p *Pipeline) guardOutcome(cfg Config, session *Session, outcome *Outcome) *Outcome {
	if guards := p.guardsForPreset(cfg.GuardSet); guards != nil && outcome != nil {
		guardCtx := p.buildGuardContext(session)
		outcome.Content = guards.ApplyFullWithContext(outcome.Content, guardCtx).Content
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

	if intent := strings.TrimSpace(session.TaskIntent()); intent != "" {
		ctx.Intents = append(ctx.Intents, intent)
	}
	if action := strings.TrimSpace(session.TaskPlannedAction()); action == "delegate_to_specialist" || action == "compose_subagent" {
		ctx.Intents = append(ctx.Intents, "delegation")
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
				if strings.Contains(msgs[i].Name, "delegat") || strings.Contains(msgs[i].Name, "subagent") {
					ctx.DelegationProvenance.SubagentTaskStarted = true
					ctx.DelegationProvenance.SubagentTaskCompleted = true
					if strings.TrimSpace(msgs[i].Content) != "" {
						ctx.DelegationProvenance.SubagentResultAttached = true
					}
				}
			}
		}
	}

	if p.store != nil {
		rows, err := p.store.QueryContext(context.Background(),
			`SELECT name FROM sub_agents WHERE enabled = 1 ORDER BY name`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var name string
				if scanErr := rows.Scan(&name); scanErr == nil && strings.TrimSpace(name) != "" {
					ctx.SubagentNames = append(ctx.SubagentNames, strings.ToLower(name))
				}
			}
		}

		_ = p.store.QueryRowContext(context.Background(),
			`SELECT selected_model
			   FROM model_selection_events
			  WHERE session_id = ?
			  ORDER BY created_at DESC, rowid DESC
			  LIMIT 1`,
			session.ID,
		).Scan(&ctx.ResolvedModel)
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
