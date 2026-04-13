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

// defaultTokenBudget is the target context window size in tokens for compaction.
const defaultTokenBudget = 8192

// runStandardInference executes the full ReAct loop via the ToolExecutor interface.
func (p *Pipeline) runStandardInference(ctx context.Context, cfg Config, session *Session, msgID, turnID string) (*Outcome, error) {
	return p.runStandardInferenceWithTrace(ctx, cfg, session, msgID, turnID, nil)
}

// runStandardInferenceWithTrace is the trace-aware variant of runStandardInference.
// When tr is non-nil, guard evaluation results are annotated to the trace.
func (p *Pipeline) runStandardInferenceWithTrace(ctx context.Context, cfg Config, session *Session, msgID, turnID string, tr *TraceRecorder) (*Outcome, error) {
	if p.executor == nil {
		return nil, core.NewError(core.ErrConfig, "no tool executor configured")
	}

	// Compact context window before inference to stay within token budget.
	if msgs := session.Messages(); len(msgs) > 0 {
		compacted := CompactContext(msgs, defaultTokenBudget)
		if len(compacted) < len(msgs) {
			log.Trace().
				Int("before", len(msgs)).
				Int("after", len(compacted)).
				Msg("context compacted before inference")
		}
	}

	// Thread model override into context for the LLM service to read.
	if cfg.ModelOverride != "" {
		ctx = core.WithModelOverride(ctx, cfg.ModelOverride)
	}

	result, turns, err := p.executor.RunLoop(ctx, session)
	if err != nil {
		return nil, core.WrapError(core.ErrLLM, "inference failed", err)
	}

	// Guard chain with full context and retry support.
	if p.guards != nil && cfg.GuardSet != GuardSetNone {
		guardStart := time.Now()
		guardCtx := p.buildGuardContext(session)
		guardResult := p.guards.ApplyFullWithContext(result, guardCtx)
		result = guardResult.Content
		guardDur := time.Since(guardStart).Milliseconds()
		log.Debug().
			Str("session", session.ID).
			Bool("retry", guardResult.RetryRequested).
			Strs("violations", guardResult.Violations).
			Str("reason", guardResult.RetryReason).
			Msg("guard chain evaluated")

		// Build per-guard trace entries for the dashboard.
		if tr != nil {
			guardResults := make(map[string]GuardTraceEntry)
			for _, v := range guardResult.Violations {
				// Violations are in "name: reason" format from ApplyFull,
				// or just "name" from ApplyFullWithContext.
				parts := strings.SplitN(v, ":", 2)
				name := strings.TrimSpace(parts[0])
				reason := ""
				if len(parts) > 1 {
					reason = strings.TrimSpace(parts[1])
				}
				outcome := "fail"
				if guardResult.RetryRequested && name == guardResult.RetryReason {
					outcome = "retry"
				}
				guardResults[name] = GuardTraceEntry{Outcome: outcome, Reason: reason}
			}
			// Determine chain type based on guard set config.
			chainType := "full"
			if cfg.GuardSet == GuardSetCached {
				chainType = "cached"
			} else if cfg.InferenceMode == InferenceStreaming {
				chainType = "stream"
			}
			AnnotateGuardTrace(tr, guardResults, chainType, guardDur)
		}

		// If guard requests retry, re-run inference once with the rejection reason.
		if guardResult.RetryRequested {
			session.AddSystemMessage(fmt.Sprintf(
				"Your previous response was rejected by the %s guard: %s. Please revise.",
				strings.Join(guardResult.Violations, ", "), guardResult.RetryReason,
			))
			retryContent, retryTurns, retryErr := p.executor.RunLoop(ctx, session)
			if retryErr != nil {
				log.Debug().Err(retryErr).Msg("guard retry inference failed, using original result")
			} else {
				turns += retryTurns
				// Apply guards again on the retry result (no further retries).
				retryGuardResult := p.guards.ApplyFullWithContext(retryContent, guardCtx)
				result = retryGuardResult.Content
			}
		}
	}

	// Store assistant response with topic tag (matching Rust: append_message_with_topic).
	assistantMsgID := db.NewID()
	topicTag := p.deriveTopicTag(session, result)
	_, storeErr := p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, topic_tag)
		 VALUES (?, ?, 'assistant', ?, ?)`,
		assistantMsgID, session.ID, result, topicTag,
	)
	if storeErr != nil {
		log.Error().Err(storeErr).Str("session", session.ID).Msg("failed to store assistant message")
	}

	// Update turn record with inference metadata.
	if _, err := p.store.ExecContext(ctx,
		`UPDATE turns SET tokens_in = ?, tokens_out = ?, model = ? WHERE id = ?`,
		0, 0, "", turnID, // tokens are tracked in inference_costs
	); err != nil {
		p.errBus.ReportIfErr(err, "pipeline", "update_turn_metadata", core.SevWarning)
	}

	// Post-turn ingest (background, tracked by worker pool).
	if cfg.PostTurnIngest && p.ingestor != nil {
		sess := session
		p.bgWorker.Submit("ingestTurn", func(bgCtx context.Context) {
			p.ingestor.IngestTurn(bgCtx, sess)
		})
	}

	// Post-turn embedding ingest + context checkpoint (background).
	p.PostTurnIngest(ctx, session, turnID, result)

	// Nickname refinement (background, tracked by worker pool).
	if cfg.NicknameRefinement && session.TurnCount() >= 4 && p.refiner != nil {
		sess := session
		p.bgWorker.Submit("refineNickname", func(bgCtx context.Context) {
			p.refiner.Refine(bgCtx, sess)
		})
	}

	// Build inference params for trace persistence (Rust parity).
	params := &InferenceParams{
		ModelRequested: cfg.ModelOverride,
		ReactTurns:     turns,
	}
	if p.guards != nil && cfg.GuardSet != GuardSetNone {
		// Guard violations were logged above; capture in params for tracing.
		guardCtx2 := p.buildGuardContext(session)
		guardResult2 := p.guards.ApplyFullWithContext(result, guardCtx2)
		if len(guardResult2.Violations) > 0 {
			params.GuardViolations = guardResult2.Violations
		}
	}

	return &Outcome{
		SessionID:       session.ID,
		MessageID:       msgID,
		Content:         result,
		ReactTurns:      turns,
		inferenceParams: params,
	}, nil
}

// prepareStreamInference sets up streaming inference via the StreamPreparer interface.
// Returns the fully-prepared LLM request in Outcome.StreamRequest so the SSE handler
// uses the same context (session history, memory, tools, system prompt) as standard inference.
//
// IMPORTANT (Rule 7.2): The SSE handler MUST call Pipeline.FinalizeStream after
// streaming completes, passing the assembled content. This ensures post-turn
// behavior (memory ingest, embedding, observer dispatch, assistant storage,
// nickname refinement) runs identically to the standard path.
func (p *Pipeline) prepareStreamInference(ctx context.Context, cfg Config, session *Session, msgID string) (*Outcome, error) {
	var streamReq *llm.Request
	if p.streamer != nil {
		req, err := p.streamer.PrepareStream(ctx, session)
		if err != nil {
			return nil, core.WrapError(core.ErrLLM, "stream preparation failed", err)
		}
		streamReq = req
	}

	return &Outcome{
		SessionID:     session.ID,
		MessageID:     msgID,
		Stream:        true,
		StreamRequest: streamReq,
		streamSession: session,
		streamConfig:  &cfg,
	}, nil
}

// FinalizeStream runs post-turn pipeline work after streaming completes.
// This is the streaming-parity guarantee (Rule 7.2): the same post-turn
// behavior that runs for standard inference also runs for streaming.
//
// The SSE handler MUST call this after the chunk loop closes, passing
// the assembled full content. Without this call, streaming turns will
// not have memory ingestion, embeddings, observer dispatch, assistant
// message storage, or nickname refinement.
func (p *Pipeline) FinalizeStream(ctx context.Context, outcome *Outcome, assembledContent string) {
	if outcome == nil || outcome.streamSession == nil {
		return
	}
	session := outcome.streamSession
	cfg := outcome.streamConfig
	if cfg == nil {
		defaultCfg := PresetStreaming()
		cfg = &defaultCfg
	}
	turnID := outcome.TurnID

	// Store assistant response with topic tag.
	assistantMsgID := db.NewID()
	topicTag := p.deriveTopicTag(session, assembledContent)
	_, storeErr := p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, topic_tag)
		 VALUES (?, ?, 'assistant', ?, ?)`,
		assistantMsgID, session.ID, assembledContent, topicTag,
	)
	if storeErr != nil {
		log.Error().Err(storeErr).Str("session", session.ID).Msg("failed to store streaming assistant message")
	}

	// Post-turn ingest (background, tracked by worker pool).
	if cfg.PostTurnIngest && p.ingestor != nil {
		sess := session
		p.bgWorker.Submit("streamIngestTurn", func(bgCtx context.Context) {
			p.ingestor.IngestTurn(bgCtx, sess)
		})
	}

	// Post-turn embedding ingest + context checkpoint (background).
	p.PostTurnIngest(ctx, session, turnID, assembledContent)

	// Nickname refinement (background).
	if cfg.NicknameRefinement && session.TurnCount() >= 4 && p.refiner != nil {
		sess := session
		p.bgWorker.Submit("streamRefineNickname", func(bgCtx context.Context) {
			p.refiner.Refine(bgCtx, sess)
		})
	}

	log.Debug().Str("session", session.ID).Int("content_len", len(assembledContent)).Msg("stream post-turn finalized")
}

// ── Inference Preparation (Rust parity) ───────────────────────────────────

// InferencePrep holds the result of BuildAndPrepareInference.
// Carries the selected model, trace annotations, and any system notes
// that should be injected before running the LLM.
type InferencePrep struct {
	SelectedModel string   // Model chosen by router or override
	SystemNotes   []string // System-level notes to inject (retrieval context, task state)
	Escalated     bool     // Whether model was escalated from a lower tier
	TraceModel    string   // Model name for trace annotation
	TraceProvider string   // Provider name for trace annotation
}

// BuildAndPrepareInference performs structured inference preparation:
// model selection, trace annotation, and system note collection.
// Matches Rust's build_and_prepare_inference().
//
// This is the boundary between pipeline orchestration and LLM execution:
// everything before this point is routing/context, everything after is inference.
func (p *Pipeline) BuildAndPrepareInference(ctx context.Context, cfg Config, session *Session, tr *TraceRecorder, turnID string) *InferencePrep {
	prep := &InferencePrep{}

	// Model selection: override takes precedence, then router.
	if cfg.ModelOverride != "" {
		prep.SelectedModel = cfg.ModelOverride
		prep.TraceModel = cfg.ModelOverride
		prep.TraceProvider = "override"
	} else if p.llmSvc != nil {
		// Use the router to select the best model.
		userContent := ""
		msgs := session.Messages()
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				userContent = msgs[i].Content
				break
			}
		}
		target := p.llmSvc.Router().Select(&llm.Request{
			Messages: []llm.Message{{Role: "user", Content: userContent}},
		})
		model := llm.ModelSpecForTarget(target)
		if model == "" {
			model = p.llmSvc.Primary()
		}
		prep.SelectedModel = model
		prep.TraceModel = model
		prep.TraceProvider = "routed"
	}

	// Trace annotation.
	AnnotateInferenceTrace(tr, prep.TraceModel, prep.TraceProvider, prep.Escalated)

	return prep
}

// PrepareForInference performs post-retrieval preparation before inference:
// injects retrieval context as system notes, persists a context snapshot,
// and optionally compresses the prompt to fit the context budget.
// Matches Rust's prepare_for_inference().
func (p *Pipeline) PrepareForInference(ctx context.Context, session *Session, memoryBlock string, budgetTier int) {
	// 1. Inject memory retrieval context as a system note.
	// This was already done in the memory retrieval stage (session.SetMemoryContext),
	// but we ensure it's present in the messages.
	if memoryBlock != "" {
		// Memory context is already set; verify it appears in messages.
		found := false
		for _, m := range session.Messages() {
			if m.Role == "system" && strings.Contains(m.Content, "[Memory Context]") {
				found = true
				break
			}
		}
		if !found {
			session.AddSystemMessage("[Memory Context]\n" + memoryBlock)
		}
	}

	// 2. Context compaction: trim to fit budget tier.
	if msgs := session.Messages(); len(msgs) > 0 {
		// Resolve budget from config tier — no more hardcoded values.
		budget := defaultTokenBudget
		cfg := Config{BudgetTier: budgetTier}
		if resolved := cfg.ResolveBudget(); resolved > 0 {
			budget = resolved
		}
		compacted := CompactContext(msgs, budget)
		if len(compacted) < len(msgs) {
			log.Trace().
				Int("before", len(msgs)).
				Int("after", len(compacted)).
				Int("budget_tier", budgetTier).
				Msg("pre-inference context compacted")
		}
	}

	// 3. Context snapshot for checkpoint persistence (handled by post-turn).
	// The snapshot is persisted asynchronously in PostTurnIngest → maybeCheckpoint.
}

// precomputeGuardScores runs lightweight pre-computation before the guard chain,
// populating the GuardContext with scores that individual guards can use (Wave 8, #71).
// This avoids redundant work when multiple guards need the same signals.
func precomputeGuardScores(ctx *GuardContext, content string) {
	if ctx == nil {
		return
	}

	// Pre-compute intent classification if not already set.
	if len(ctx.Intents) == 0 && ctx.UserPrompt != "" {
		registry := NewIntentRegistry()
		intent, _ := registry.Classify(ctx.UserPrompt)
		ctx.Intents = append(ctx.Intents, string(intent))
	}

	// Pre-compute semantic scores for common guard signals.
	if ctx.SemanticScores == nil {
		ctx.SemanticScores = make(map[string]float64)
	}

	lower := strings.ToLower(content)

	// Financial claim score: how strongly does the content claim financial actions?
	financialScore := 0.0
	for _, claim := range financialActionClaims {
		if strings.Contains(lower, claim) {
			financialScore += 0.2
		}
	}
	if financialScore > 1.0 {
		financialScore = 1.0
	}
	ctx.SemanticScores["financial_claim"] = financialScore

	// Repetition score: overlap with previous assistant message.
	if ctx.PreviousAssistant != "" {
		ctx.SemanticScores["prev_overlap"] = tokenOverlapRatio(content, ctx.PreviousAssistant)
	}

	// Identity claim score: does the content make identity claims?
	identityScore := 0.0
	for _, marker := range foreignIdentityMarkers {
		if strings.Contains(lower, marker) {
			identityScore = 1.0
			break
		}
	}
	ctx.SemanticScores["identity_claim"] = identityScore
}

// resolveSession finds or creates a session based on the resolution mode.
func (p *Pipeline) resolveSession(ctx context.Context, cfg Config, input Input) (*Session, error) {
	switch cfg.SessionResolution {
	case SessionFromBody:
		if input.SessionID != "" {
			return p.loadSession(ctx, input)
		}
		return p.createSession(ctx, input)

	case SessionFromChannel:
		scope := fmt.Sprintf("%s:%s", input.Platform, input.ChatID)
		row := p.store.QueryRowContext(ctx,
			`SELECT id FROM sessions WHERE agent_id = ? AND scope_key = ? AND status = 'active'
			 ORDER BY created_at DESC LIMIT 1`,
			input.AgentID, scope,
		)
		var sessionID string
		if err := row.Scan(&sessionID); err == nil {
			return p.loadSessionByID(ctx, sessionID, input)
		}
		return p.createSessionWithScope(ctx, input, scope)

	case SessionDedicated:
		return p.createSession(ctx, input)
	}
	return nil, core.NewError(core.ErrConfig, "unknown session resolution mode")
}

func (p *Pipeline) loadSession(ctx context.Context, input Input) (*Session, error) {
	sess := NewSession(input.SessionID, input.AgentID, input.AgentName)
	sess.Channel = input.Platform

	rows, err := p.store.QueryContext(ctx,
		`SELECT role, content FROM session_messages WHERE session_id = ? ORDER BY created_at ASC LIMIT 50`,
		input.SessionID,
	)
	if err != nil {
		log.Warn().Err(err).Str("session_id", input.SessionID).Msg("failed to load session history, continuing without context")
		return sess, nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			continue
		}
		switch role {
		case "user":
			sess.AddUserMessage(content)
		case "assistant":
			sess.AddAssistantMessage(content, nil)
		case "system":
			sess.AddSystemMessage(content)
		}
	}
	return sess, nil
}

func (p *Pipeline) loadSessionByID(ctx context.Context, sessionID string, input Input) (*Session, error) {
	input.SessionID = sessionID
	return p.loadSession(ctx, input)
}

func (p *Pipeline) createSession(ctx context.Context, input Input) (*Session, error) {
	// Use a unique scope per session (platform + session ID) to avoid
	// UNIQUE constraint on (agent_id, scope_key) for active sessions.
	id := db.NewID()
	scopeKey := input.Platform + ":" + id
	return p.createSessionWithID(ctx, input, id, scopeKey)
}

func (p *Pipeline) createSessionWithScope(ctx context.Context, input Input, scopeKey string) (*Session, error) {
	return p.createSessionWithID(ctx, input, db.NewID(), scopeKey)
}

func (p *Pipeline) createSessionWithID(ctx context.Context, input Input, id, scopeKey string) (*Session, error) {
	_, err := p.store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		id, input.AgentID, scopeKey,
	)
	if err != nil {
		return nil, err
	}
	sess := NewSession(id, input.AgentID, input.AgentName)
	sess.Channel = input.Platform
	return sess, nil
}

// expandShortFollowup detects short reactions and prepends prior context.
func (p *Pipeline) expandShortFollowup(session *Session, content string) string {
	if len(content) < 20 && session.TurnCount() > 0 {
		prior := session.LastAssistantContent()
		if prior != "" {
			prefix := prior
			if len(prefix) > 200 {
				prefix = prefix[:200] + "..."
			}
			return fmt.Sprintf("[Regarding your previous response: %q]\n\n%s", prefix, content)
		}
	}
	return content
}

// tryShortcut dispatches shortcuts via the ShortcutHandler system.
// trySkillFirst attempts to match user input against registered skill triggers.
// Skills are only dispatched if skill-first is enabled, authority is Creator,
// and the skill matcher is wired. Mirrors Rust's try_skill_first() in inference.rs.
func (p *Pipeline) trySkillFirst(ctx context.Context, cfg Config, authority core.AuthorityLevel, session *Session, content string) *Outcome {
	if !cfg.SkillFirstEnabled || authority != core.AuthorityCreator || p.skills == nil {
		return nil
	}
	return p.skills.TryMatch(ctx, session, content)
}

// tryShortcut evaluates the shortcut handler system against user input.
// Uses DispatchShortcut with rich context (correction_turn, delegation_provenance)
// so handlers can make context-aware decisions about whether to match.
func (p *Pipeline) tryShortcut(_ context.Context, session *Session, content string, correctionTurn bool, channelLabel string) *Outcome {
	ctx := &ShortcutContext{
		CorrectionTurn:         correctionTurn,
		DelegationProvenance:   false, // Set by caller when applicable
		HasConversationContext: session.TurnCount() > 0,
		AgentName:              session.AgentName,
		SessionTurnCount:       session.TurnCount(),
		PreviousAssistantText:  session.LastAssistantContent(),
		ChannelLabel:           channelLabel,
	}

	result := DispatchShortcut(DefaultShortcutHandlers(), content, ctx)
	if result == nil {
		return nil
	}

	return &Outcome{
		SessionID: session.ID,
		Content:   result.Content,
	}
}
