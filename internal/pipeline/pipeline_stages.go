package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"goboticus/internal/agent"
	"goboticus/internal/core"
	"goboticus/internal/db"
	"goboticus/internal/llm"
)

// runStandardInference executes the full ReAct loop.
func (p *Pipeline) runStandardInference(ctx context.Context, cfg Config, session *agent.Session, msgID string) (*Outcome, error) {
	ctxBuilder := agent.NewContextBuilder(p.ctxCfg)
	ctxBuilder.SetSystemPrompt(agent.BuildSystemPrompt(p.promptCfg))
	ctxBuilder.SetTools(p.tools.ToolDefs())

	if p.retriever != nil {
		memBlock := p.retriever.Retrieve(ctx, session.ID, session.LastAssistantContent(), p.ctxCfg.MaxTokens/4)
		if memBlock != "" {
			ctxBuilder.SetMemory(memBlock)
		}
	}

	deps := agent.LoopDeps{
		LLM:       p.llmSvc,
		Tools:     p.tools,
		Policy:    p.policy,
		Injection: p.injection,
		Memory:    p.memory,
		Context:   ctxBuilder,
	}
	loop := agent.NewLoop(p.loopCfg, deps)

	result, err := loop.Run(ctx, session)
	if err != nil {
		return nil, core.WrapError(core.ErrLLM, "inference failed", err)
	}

	// Guard chain.
	if p.guards != nil && cfg.GuardSet != GuardSetNone {
		result = p.guards.Apply(result)
	}

	// Post-turn ingest (background, tracked by worker pool).
	if cfg.PostTurnIngest && p.memory != nil {
		sess := session
		p.bgWorker.Submit("ingestTurn", func(bgCtx context.Context) {
			p.memory.IngestTurn(bgCtx, sess)
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
	if cfg.NicknameRefinement && session.TurnCount() >= 4 {
		sess := session
		p.bgWorker.Submit("refineNickname", func(bgCtx context.Context) {
			p.refineNickname(bgCtx, sess)
		})
	}

	return &Outcome{
		SessionID:  session.ID,
		MessageID:  msgID,
		Content:    result,
		ReactTurns: loop.TurnCount(),
	}, nil
}

// prepareStreamInference sets up streaming inference.
func (p *Pipeline) prepareStreamInference(_ context.Context, _ Config, session *agent.Session, msgID string) (*Outcome, error) {
	ctxBuilder := agent.NewContextBuilder(p.ctxCfg)
	ctxBuilder.SetSystemPrompt(agent.BuildSystemPrompt(p.promptCfg))

	req := ctxBuilder.BuildRequest(session)
	req.Stream = true

	return &Outcome{
		SessionID: session.ID,
		MessageID: msgID,
		Stream:    true,
	}, nil
}

// resolveSession finds or creates a session based on the resolution mode.
func (p *Pipeline) resolveSession(ctx context.Context, cfg Config, input Input) (*agent.Session, error) {
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

func (p *Pipeline) loadSession(ctx context.Context, input Input) (*agent.Session, error) {
	sess := agent.NewSession(input.SessionID, input.AgentID, input.AgentName)
	sess.Channel = input.Platform

	rows, err := p.store.QueryContext(ctx,
		`SELECT role, content FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT 50`,
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

func (p *Pipeline) loadSessionByID(ctx context.Context, sessionID string, input Input) (*agent.Session, error) {
	input.SessionID = sessionID
	return p.loadSession(ctx, input)
}

func (p *Pipeline) createSession(ctx context.Context, input Input) (*agent.Session, error) {
	return p.createSessionWithScope(ctx, input, input.Platform)
}

func (p *Pipeline) createSessionWithScope(ctx context.Context, input Input, scopeKey string) (*agent.Session, error) {
	id := db.NewID()
	_, err := p.store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		id, input.AgentID, scopeKey,
	)
	if err != nil {
		return nil, err
	}
	sess := agent.NewSession(id, input.AgentID, input.AgentName)
	sess.Channel = input.Platform
	return sess, nil
}

// expandShortFollowup detects short reactions and prepends prior context.
func (p *Pipeline) expandShortFollowup(session *agent.Session, content string) string {
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

// trySkillFirst checks if input matches any skill trigger.
func (p *Pipeline) trySkillFirst(_ context.Context, session *agent.Session, content string) *Outcome {
	if len(p.skills) == 0 {
		return nil
	}

	lower := strings.ToLower(content)
	var bestSkill *agent.LoadedSkill
	var bestScore int

	for _, skill := range p.skills {
		score := 0
		for _, trigger := range skill.Triggers() {
			if strings.Contains(lower, strings.ToLower(trigger)) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestSkill = skill
		}
	}

	if bestSkill == nil || bestScore == 0 {
		return nil
	}

	log.Info().Str("skill", bestSkill.Name()).Int("score", bestScore).Msg("skill matched")
	session.AddSystemMessage(fmt.Sprintf("[Skill: %s]\n%s", bestSkill.Name(), bestSkill.Body))
	return nil
}

// tryShortcut checks for simple shortcuts that don't need full LLM inference.
func (p *Pipeline) tryShortcut(_ context.Context, session *agent.Session, content string) *Outcome {
	lower := strings.TrimSpace(strings.ToLower(content))

	if lower == "who are you" || lower == "who are you?" || lower == "what are you?" {
		return &Outcome{
			SessionID: session.ID,
			Content:   fmt.Sprintf("I am %s, an autonomous AI agent.", p.promptCfg.AgentName),
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
			Content: fmt.Sprintf("%s can help with:\n- General conversation and reasoning\n- File operations and code tasks\n- Web search and information retrieval\n- Scheduling and reminders\n- Financial operations\n\nJust describe what you need.", p.promptCfg.AgentName),
		}
	}

	return nil
}

// refineNickname uses the first user message to generate a short session name.
func (p *Pipeline) refineNickname(ctx context.Context, session *agent.Session) {
	messages := session.Messages()
	if len(messages) == 0 {
		return
	}

	var firstUser string
	for _, m := range messages {
		if m.Role == "user" && m.Content != "" {
			firstUser = m.Content
			break
		}
	}
	if firstUser == "" {
		return
	}

	req := &llm.Request{
		Model:     "",
		MaxTokens: 20,
		Messages: []llm.Message{
			{Role: "system", Content: "Generate a 2-4 word title for this conversation. Reply with ONLY the title, nothing else."},
			{Role: "user", Content: firstUser},
		},
	}

	resp, err := p.llmSvc.Complete(ctx, req)
	if err != nil {
		log.Debug().Err(err).Msg("nickname refinement failed")
		return
	}

	nickname := strings.TrimSpace(resp.Content)
	if nickname == "" || len(nickname) > 50 {
		return
	}

	if p.store != nil {
		_, _ = p.store.ExecContext(ctx,
			`UPDATE sessions SET nickname = ? WHERE id = ?`,
			nickname, session.ID,
		)
	}
}
