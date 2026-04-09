package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// defaultTokenBudget is the target context window size in tokens for compaction.
const defaultTokenBudget = 8192

// runStandardInference executes the full ReAct loop via the ToolExecutor interface.
func (p *Pipeline) runStandardInference(ctx context.Context, cfg Config, session *Session, msgID, turnID string) (*Outcome, error) {
	if p.executor == nil {
		return nil, core.NewError(core.ErrConfig, "no tool executor configured")
	}

	// Compact context window before inference to stay within token budget.
	if msgs := session.Messages(); len(msgs) > 0 {
		compacted := CompactContext(msgs, defaultTokenBudget)
		if len(compacted) < len(msgs) {
			log.Debug().
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
		guardCtx := p.buildGuardContext(session)
		guardResult := p.guards.ApplyFullWithContext(result, guardCtx)
		result = guardResult.Content
		log.Debug().
			Str("session", session.ID).
			Bool("retry", guardResult.RetryRequested).
			Strs("violations", guardResult.Violations).
			Str("reason", guardResult.RetryReason).
			Msg("guard chain evaluated")

		// If guard requests retry, re-run inference once with the rejection reason.
		if guardResult.RetryRequested {
			session.AddSystemMessage(fmt.Sprintf(
				"Your previous response was rejected by the %s guard: %s. Please revise.",
				strings.Join(guardResult.Violations, ", "), guardResult.RetryReason,
			))
			retryContent, retryTurns, retryErr := p.executor.RunLoop(ctx, session)
			if retryErr != nil {
				log.Warn().Err(retryErr).Msg("guard retry inference failed, using original result")
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

	return &Outcome{
		SessionID:  session.ID,
		MessageID:  msgID,
		Content:    result,
		ReactTurns: turns,
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

// tryShortcut checks for simple shortcuts that don't need full LLM inference.
func (p *Pipeline) tryShortcut(_ context.Context, session *Session, content string) *Outcome {
	lower := strings.TrimSpace(strings.ToLower(content))

	if lower == "who are you" || lower == "who are you?" || lower == "what are you?" {
		return &Outcome{
			SessionID: session.ID,
			Content:   fmt.Sprintf("I am %s, an autonomous AI agent.", session.AgentName),
		}
	}

	switch lower {
	case "ok", "okay", "thanks", "thank you", "got it", "understood", "k", "ty":
		return &Outcome{
			SessionID: session.ID,
			Content:   "Acknowledged. Let me know if you need anything else.",
		}
	}

	if lower == "help" || lower == "/help" {
		return &Outcome{
			SessionID: session.ID,
			Content:   fmt.Sprintf("%s can help with:\n- General conversation and reasoning\n- File operations and code tasks\n- Web search and information retrieval\n- Scheduling and reminders\n- Financial operations\n\nJust describe what you need.", session.AgentName),
		}
	}

	return nil
}
