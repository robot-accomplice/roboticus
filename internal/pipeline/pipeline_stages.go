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

// runStandardInference executes the full ReAct loop via the ToolExecutor interface.
func (p *Pipeline) runStandardInference(ctx context.Context, cfg Config, session *Session, msgID string) (*Outcome, error) {
	if p.executor == nil {
		return nil, core.NewError(core.ErrConfig, "no tool executor configured")
	}

	result, turns, err := p.executor.RunLoop(ctx, session)
	if err != nil {
		return nil, core.WrapError(core.ErrLLM, "inference failed", err)
	}

	// Guard chain.
	if p.guards != nil && cfg.GuardSet != GuardSetNone {
		result = p.guards.Apply(result)
	}

	// Post-turn ingest (background, tracked by worker pool).
	if cfg.PostTurnIngest && p.ingestor != nil {
		sess := session
		p.bgWorker.Submit("ingestTurn", func(bgCtx context.Context) {
			p.ingestor.IngestTurn(bgCtx, sess)
		})
	}

	// Store assistant response.
	assistantMsgID := db.NewID()
	_, storeErr := p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content)
		 VALUES (?, ?, 'assistant', ?)`,
		assistantMsgID, session.ID, result,
	)
	if storeErr != nil {
		log.Error().Err(storeErr).Str("session", session.ID).Msg("failed to store assistant message")
	}

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
func (p *Pipeline) prepareStreamInference(ctx context.Context, _ Config, session *Session, msgID string) (*Outcome, error) {
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
	}, nil
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
