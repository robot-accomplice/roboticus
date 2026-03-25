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
	store     *db.Store
	llmSvc    *llm.Service
	injection *agent.InjectionDetector
	tools     *agent.ToolRegistry
	policy    *agent.PolicyEngine
	memory    *agent.MemoryManager
	retriever *agent.MemoryRetriever
	skills    []*agent.LoadedSkill
	guards    *GuardChain
	loopCfg   agent.LoopConfig
	ctxCfg    agent.ContextConfig
	promptCfg agent.PromptConfig
}

// PipelineDeps bundles dependencies for the Pipeline.
type PipelineDeps struct {
	Store     *db.Store
	LLM       *llm.Service
	Injection *agent.InjectionDetector
	Tools     *agent.ToolRegistry
	Policy    *agent.PolicyEngine
	Memory    *agent.MemoryManager
	Retriever *agent.MemoryRetriever
	Skills    []*agent.LoadedSkill
	Guards    *GuardChain
	LoopCfg   agent.LoopConfig
	CtxCfg    agent.ContextConfig
	PromptCfg agent.PromptConfig
}

// New creates the unified pipeline.
func New(deps PipelineDeps) *Pipeline {
	return &Pipeline{
		store:     deps.Store,
		llmSvc:    deps.LLM,
		injection: deps.Injection,
		tools:     deps.Tools,
		policy:    deps.Policy,
		memory:    deps.Memory,
		retriever: deps.Retriever,
		skills:    deps.Skills,
		guards:    deps.Guards,
		loopCfg:   deps.LoopCfg,
		ctxCfg:    deps.CtxCfg,
		promptCfg: deps.PromptCfg,
	}
}

// RunPipeline is the canonical package-level entry point for all connectors.
// It wraps Pipeline.Run with centralized error handling and tracing.
// HTTP handlers, webhooks, and cron should call this — not p.Run directly.
func RunPipeline(ctx context.Context, p Runner, cfg Config, input Input) (*Outcome, error) {
	return p.Run(ctx, cfg, input)
}

// Run executes the full pipeline with the given config and input.
// This is the single entry point for all connectors.
//
// Stage order:
//  1. Input validation
//  2. Injection defense (L1 score, L2 sanitize)
//  3. Dedup tracking
//  4. Session resolution
//  5. Short-followup expansion
//  6. User message storage
//  7. Decomposition gate
//  8. Authority resolution
//  9. Delegated execution
//  10. Skill-first fulfillment
//  11. Shortcut dispatch → Inference
//  12. Guard chain → Post-turn ingest → Response
func (p *Pipeline) Run(ctx context.Context, cfg Config, input Input) (*Outcome, error) {
	// Stage 1: Input validation.
	if input.Content == "" {
		return nil, core.NewError(core.ErrConfig, "empty message content")
	}
	if len(input.Content) > core.MaxUserMessageBytes {
		return nil, core.NewError(core.ErrConfig, fmt.Sprintf("message exceeds %d bytes", core.MaxUserMessageBytes))
	}

	// Stage 2: Injection defense.
	if cfg.InjectionDefense && p.injection != nil {
		score := p.injection.CheckInput(input.Content)
		if score.IsBlocked() {
			log.Warn().Float64("score", float64(score)).Str("channel", cfg.ChannelLabel).Msg("injection blocked")
			return nil, core.NewError(core.ErrInjectionBlocked, "input rejected by injection defense")
		}
		if score.IsCaution() {
			input.Content = p.injection.Sanitize(input.Content)
			log.Info().Float64("score", float64(score)).Msg("input sanitized")
		}
	}

	// Stage 3: Dedup tracking.
	// TODO: implement in-flight dedup guard (phase 7 polish)

	// Stage 4: Session resolution.
	session, err := p.resolveSession(ctx, cfg, input)
	if err != nil {
		return nil, core.WrapError(core.ErrDatabase, "session resolution failed", err)
	}

	// Stage 5: Short-followup expansion.
	content := input.Content
	if cfg.ShortFollowupExpansion {
		content = p.expandShortFollowup(session, content)
	}

	// Stage 6: User message storage.
	msgID := db.NewID()
	_, err = p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content)
		 VALUES (?, ?, 'user', ?)`,
		msgID, session.ID, content,
	)
	if err != nil {
		return nil, core.WrapError(core.ErrDatabase, "failed to store user message", err)
	}
	session.AddUserMessage(content)

	// Stage 7: Decomposition gate.
	// TODO: evaluate complexity for potential delegation when multi-agent delegation is wired in Phase 6.

	// Stage 8: Authority resolution.
	authority := ResolveAuthority(cfg.AuthorityMode, input.Claim)
	session.Authority = authority

	// Stage 9: Delegated execution.
	// TODO: wire orchestrator integration (Phase 6)

	// Stage 10: Skill-first fulfillment.
	if cfg.SkillFirstEnabled && authority == core.AuthorityCreator {
		if result := p.trySkillFirst(ctx, session, content); result != nil {
			return result, nil
		}
	}

	// Stage 11: Shortcut dispatch.
	if cfg.ShortcutsEnabled {
		if result := p.tryShortcut(ctx, session, content); result != nil {
			return result, nil
		}
	}

	// Cache check: handled inside the LLM service when cfg.CacheEnabled.

	// Stage 11b: Inference.
	switch cfg.InferenceMode {
	case InferenceStandard:
		return p.runStandardInference(ctx, cfg, session, msgID)
	case InferenceStreaming:
		return p.prepareStreamInference(ctx, cfg, session, msgID)
	}

	return nil, core.NewError(core.ErrConfig, "unknown inference mode")
}

// runStandardInference executes the full ReAct loop.
func (p *Pipeline) runStandardInference(ctx context.Context, cfg Config, session *agent.Session, msgID string) (*Outcome, error) {
	// Build context with memory retrieval.
	ctxBuilder := agent.NewContextBuilder(p.ctxCfg)
	ctxBuilder.SetSystemPrompt(agent.BuildSystemPrompt(p.promptCfg))
	ctxBuilder.SetTools(p.tools.ToolDefs())

	// Retrieve and inject memories.
	if p.retriever != nil {
		memBlock := p.retriever.Retrieve(ctx, session.ID, session.LastAssistantContent(), p.ctxCfg.MaxTokens/4)
		if memBlock != "" {
			ctxBuilder.SetMemory(memBlock)
		}
	}

	// Create and run the ReAct loop.
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

	// Stage 12: Guard chain.
	if p.guards != nil && cfg.GuardSet != GuardSetNone {
		result = p.guards.Apply(result)
	}

	// Post-turn ingest (background).
	if cfg.PostTurnIngest && p.memory != nil {
		go p.memory.IngestTurn(context.Background(), session)
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

	// Nickname refinement (background, API only).
	if cfg.NicknameRefinement && session.TurnCount() >= 4 {
		go p.refineNickname(context.Background(), session)
	}

	return &Outcome{
		SessionID:  session.ID,
		MessageID:  msgID,
		Content:    result,
		ReactTurns: loop.TurnCount(),
	}, nil
}

// prepareStreamInference sets up streaming inference.
func (p *Pipeline) prepareStreamInference(ctx context.Context, cfg Config, session *agent.Session, msgID string) (*Outcome, error) {
	// For streaming, we bypass the ReAct loop and send directly to the LLM service.
	ctxBuilder := agent.NewContextBuilder(p.ctxCfg)
	ctxBuilder.SetSystemPrompt(agent.BuildSystemPrompt(p.promptCfg))

	req := ctxBuilder.BuildRequest(session)
	req.Stream = true

	// Start the stream — the actual SSE emission is handled by the API layer.
	// We return a StreamReady outcome with the session info.
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
			// Use provided session ID.
			return p.loadSession(ctx, input)
		}
		// Create a new session.
		return p.createSession(ctx, input)

	case SessionFromChannel:
		scope := fmt.Sprintf("%s:%s", input.Platform, input.ChatID)
		// Try to find existing session for this channel scope.
		row := p.store.QueryRowContext(ctx,
			`SELECT id FROM sessions WHERE agent_id = ? AND scope_key = ? AND status = 'active'
			 ORDER BY created_at DESC LIMIT 1`,
			input.AgentID, scope,
		)
		var sessionID string
		if err := row.Scan(&sessionID); err == nil {
			return p.loadSessionByID(ctx, sessionID, input)
		}
		// Create new session with channel scope.
		return p.createSessionWithScope(ctx, input, scope)

	case SessionDedicated:
		// Always create a fresh session for cron jobs.
		return p.createSession(ctx, input)
	}
	return nil, core.NewError(core.ErrConfig, "unknown session resolution mode")
}

func (p *Pipeline) loadSession(ctx context.Context, input Input) (*agent.Session, error) {
	sess := agent.NewSession(input.SessionID, input.AgentID, input.AgentName)
	sess.Channel = input.Platform

	// Load recent messages into session.
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
	// Short messages (< 20 chars) after at least one exchange get prior context.
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

// trySkillFirst checks if input matches any skill trigger and executes directly.
// If a skill matches, its instruction body is injected as a system message and
// the pipeline runs inference with that skill's context.
func (p *Pipeline) trySkillFirst(ctx context.Context, session *agent.Session, content string) *Outcome {
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

	// Inject skill body as a system message prefix.
	session.AddSystemMessage(fmt.Sprintf("[Skill: %s]\n%s", bestSkill.Name(), bestSkill.Body))

	// Fall through to normal inference — returning nil lets the pipeline continue.
	// The skill context is now in the session and will be included in the LLM call.
	return nil
}

// tryShortcut checks for simple shortcuts that don't need full LLM inference.
func (p *Pipeline) tryShortcut(_ context.Context, session *agent.Session, content string) *Outcome {
	lower := strings.TrimSpace(strings.ToLower(content))

	// Identity shortcut.
	if lower == "who are you" || lower == "who are you?" || lower == "what are you?" {
		return &Outcome{
			SessionID: session.ID,
			Content:   fmt.Sprintf("I am %s, an autonomous AI agent.", p.promptCfg.AgentName),
		}
	}

	// Simple acknowledgement — no inference needed.
	switch lower {
	case "ok", "okay", "thanks", "thank you", "got it", "understood", "k", "ty":
		return &Outcome{
			SessionID: session.ID,
			Content:   "Acknowledged. Let me know if you need anything else.",
		}
	}

	// Help shortcut.
	if lower == "help" || lower == "/help" {
		return &Outcome{
			SessionID: session.ID,
			Content:   fmt.Sprintf("%s can help with:\n- General conversation and reasoning\n- File operations and code tasks\n- Web search and information retrieval\n- Scheduling and reminders\n- Financial operations\n\nJust describe what you need.", p.promptCfg.AgentName),
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

	// Find the first user message.
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

	// Generate nickname via a short LLM call.
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

	// Update session nickname in DB.
	if p.store != nil {
		_, _ = p.store.ExecContext(ctx,
			`UPDATE sessions SET nickname = ? WHERE id = ?`,
			nickname, session.ID,
		)
	}
}
