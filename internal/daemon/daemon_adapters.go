package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/agent"
	"roboticus/internal/agent/memory"
	"roboticus/internal/agent/policy"
	"roboticus/internal/agent/skills"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
	"roboticus/internal/session"
)

// ---------------------------------------------------------------------------
// Adapter types: bridge concrete agent types to pipeline interfaces.
// These are private wiring glue — not reusable outside the composition root.
// ---------------------------------------------------------------------------

// injectionAdapter wraps *agent.InjectionDetector → pipeline.InjectionChecker.
type injectionAdapter struct {
	det *agent.InjectionDetector
}

func (a *injectionAdapter) CheckInput(text string) core.ThreatScore {
	return a.det.CheckInput(text)
}

func (a *injectionAdapter) Sanitize(text string) string {
	return a.det.Sanitize(text)
}

// retrieverAdapter wraps *memory.Retriever → pipeline.MemoryRetriever.
type retrieverAdapter struct {
	r *memory.Retriever
}

func (a *retrieverAdapter) Retrieve(ctx context.Context, sessionID, query string, budget int) string {
	if a.r == nil {
		return ""
	}
	// Per-request intent classification travels via context value —
	// never a field on the shared *Retriever. Pre-v1.0.6 this adapter
	// called a.r.SetIntents(...) before Retrieve, which mutated state on
	// a struct shared across concurrent turns and could bleed intents
	// from one request into another's routing plan.
	if strings.TrimSpace(query) != "" {
		reg := pipeline.NewIntentRegistry()
		intent, conf := reg.Classify(query)
		ctx = memory.WithIntents(ctx, []memory.IntentSignal{{
			Label:      string(intent),
			Confidence: conf,
		}})
	}
	return a.r.Retrieve(ctx, sessionID, query, budget)
}

// ingestorAdapter wraps *memory.Manager → pipeline.Ingestor.
type ingestorAdapter struct {
	m *memory.Manager
}

func (a *ingestorAdapter) IngestTurn(ctx context.Context, session *session.Session) {
	a.m.IngestTurn(ctx, session)
}

// buildAgentContext assembles a ContextBuilder with system prompt, tool
// defs, and pipeline-prepared memory. Shared by executorAdapter and
// streamAdapter.
//
// v1.0.6: retriever and store are no longer parameters — see the memory
// injection block below for why. The pipeline's Stage 8.5 is the single
// authority for memory preparation; this adapter just reads what the
// pipeline already wrote to the session.
func buildAgentContext(_ context.Context, sess *session.Session, tools *agent.ToolRegistry, promptCfg agent.PromptConfig, budgetCfg *core.ContextBudgetConfig) *agent.ContextBuilder {
	ccfg := agent.DefaultContextConfig()
	if budgetCfg != nil {
		ccfg.BudgetConfig = budgetCfg
	}
	ctxBuilder := agent.NewContextBuilder(ccfg)

	cfg := promptCfg
	// Use session's agent name only if explicitly set (not "default").
	// Otherwise keep the configured agent name (e.g., "Duncan").
	if sess.AgentName != "" && sess.AgentName != "default" {
		cfg.AgentName = sess.AgentName
	}
	systemPrompt := agent.BuildSystemPrompt(cfg)

	// HMAC trust boundary: wrap system prompt so model output verification
	// can detect forged prompt injections (Rust parity).
	if len(cfg.BoundaryKey) > 0 {
		systemPrompt = agent.TagContent(systemPrompt, cfg.BoundaryKey)
		// Sanity check: verify immediately after injection (matches Rust).
		if _, ok := agent.VerifyHMACBoundary(systemPrompt, cfg.BoundaryKey); !ok {
			log.Error().Msg("HMAC boundary verification failed immediately after injection")
		}
	}

	log.Info().
		Str("agent_name", cfg.AgentName).
		Int("personality_len", len(cfg.Personality)).
		Int("firmware_len", len(cfg.Firmware)).
		Int("prompt_len", len(systemPrompt)).
		Int("tool_defs", func() int {
			if tools != nil {
				return len(tools.ToolDefs())
			}
			return 0
		}()).
		Int("tool_names_in_prompt", len(cfg.ToolNames)).
		Bool("memory_ctx_present", sess.MemoryContext() != "").
		Bool("memory_idx_present", sess.MemoryIndex() != "").
		Msg("context built for inference")
	ctxBuilder.SetSystemPrompt(systemPrompt)

	if tools != nil {
		ctxBuilder.SetTools(tools.ToolDefs())
	}

	// Memory injection: pipeline-owned.
	//
	// The pipeline's Stage 8.5 (internal/pipeline/pipeline_run_stages.go,
	// stageMemoryRetrieval) is the single authority for preparing memory
	// context + memory index on the session. By the time this adapter runs
	// (inside the pipeline's inference stage), Stage 8.5 has already:
	//
	//   * populated MemoryIndex unconditionally (the recall handle that the
	//     model uses for on-demand lookups; always present, even when no
	//     retrieval was run, so the model can call recall_memory(id))
	//   * populated MemoryContext IFF retrieval strategy != "none" (working
	//     memory + ambient + filtered tiered evidence); empty means the
	//     strategy decided no retrieval was useful for this turn
	//
	// Pre-v1.0.6 this adapter had a FALLBACK path that reconstructed both
	// if the session fields were empty (RetrieveDirectOnly +
	// BuildMemoryIndex inline). The v1.0.6 architecture audit flagged that
	// fallback as a "pipeline is single behavioral authority" violation —
	// when Stage 8.5 decided "no retrieval needed" for an early-turn
	// simple request, the fallback ignored that decision and retrieved
	// anyway. That's split-brain: two code paths producing memory, one
	// outside the pipeline. The fix is to trust the pipeline's output
	// verbatim and serve whatever it prepared (including empty).
	//
	// Rust architecture (retrieval.rs lines 235-258):
	//   "Memory = index, not storage. Only working memory and recent activity
	//   are injected directly (cheap, session-scoped, always relevant).
	//   All other tiers are index-only — the model calls recall_memory(id)
	//   to fetch full content on demand."
	//
	// CRITICAL: Do NOT inject full episodic/semantic/procedural/relationship
	// content. If the model sees a blob of "memories" it assumes that's
	// everything and never calls recall_memory — leading to confabulation
	// when the topic isn't in the injected block.
	if memCtx := sess.MemoryContext(); memCtx != "" {
		ctxBuilder.SetMemory(memCtx)
	}
	if memIdx := sess.MemoryIndex(); memIdx != "" {
		ctxBuilder.SetMemoryIndex(memIdx)
	}

	return ctxBuilder
}

// executorAdapter wraps the full agent loop deps → pipeline.ToolExecutor.
//
// v1.0.6: retriever and store were removed. Memory preparation is the
// pipeline's responsibility (Stage 8.5); the executor just reads what
// the pipeline wrote to the session. See buildAgentContext for details.
type executorAdapter struct {
	llmSvc          *llm.Service
	tools           *agent.ToolRegistry
	policy          *policy.Engine
	injection       *agent.InjectionDetector
	memMgr          *memory.Manager
	promptConfig    agent.PromptConfig
	budgetCfg       *core.ContextBudgetConfig
	maxTurnDuration time.Duration
}

func (a *executorAdapter) RunLoop(ctx context.Context, sess *session.Session) (string, int, error) {
	ctxBuilder := buildAgentContext(ctx, sess, a.tools, a.promptConfig, a.budgetCfg)

	loopCfg := agent.DefaultLoopConfig()
	if a.maxTurnDuration > 0 {
		loopCfg.MaxLoopDuration = a.maxTurnDuration
	}
	loop := agent.NewLoop(loopCfg, agent.LoopDeps{
		LLM:       a.llmSvc,
		Tools:     a.tools,
		Policy:    a.policy,
		Injection: a.injection,
		Memory:    a.memMgr,
		Context:   ctxBuilder,
	})

	content, err := loop.Run(ctx, sess)
	return content, loop.TurnCount(), err
}

// nicknameAdapter wraps *llm.Service + *db.Store → pipeline.NicknameRefiner.
//
// APPROVED OFF-PIPELINE LLM CALLER: Nickname refinement is a cosmetic post-turn
// decoration (generating a short session title from the first user message).
// It is not agent inference, does not affect behavior, and has no policy or
// guard chain requirements. Calling llm.Complete directly avoids pipeline
// overhead for a trivial 20-token generation.
type nicknameAdapter struct {
	llm   *llm.Service
	store *db.Store
}

func (a *nicknameAdapter) Refine(ctx context.Context, session *session.Session) {
	// Find first user message to use as basis for title generation.
	var firstUserMsg string
	for _, m := range session.Messages() {
		if m.Role == "user" {
			firstUserMsg = m.Content
			break
		}
	}
	if firstUserMsg == "" {
		return
	}

	// Truncate long messages for the prompt.
	snippet := firstUserMsg
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}

	req := &llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "Generate a concise 2-4 word title for a conversation that starts with the following message. Respond with ONLY the title, no quotes or punctuation."},
			{Role: "user", Content: snippet},
		},
		MaxTokens: 20,
	}

	resp, err := a.llm.Complete(ctx, req)
	if err != nil {
		log.Warn().Err(err).Str("session", session.ID).Msg("nickname refinement LLM call failed")
		return
	}

	title := strings.TrimSpace(resp.Content)
	if title == "" || len(title) > 60 {
		return
	}

	_, err = a.store.ExecContext(ctx,
		`UPDATE sessions SET nickname = ? WHERE id = ?`,
		title, session.ID,
	)
	if err != nil {
		log.Warn().Err(err).Str("session", session.ID).Msg("failed to update session nickname")
	}
}

// skillAdapter bridges skills.Matcher → pipeline.SkillMatcher.
type skillAdapter struct {
	matcher *skills.Matcher
	tools   *agent.ToolRegistry
}

func (a *skillAdapter) TryMatch(ctx context.Context, session *session.Session, content string) *pipeline.Outcome {
	skill := a.matcher.Match(content)
	if skill == nil {
		return nil
	}

	switch skill.Type {
	case skills.Instruction:
		// Instruction skills return their body directly as the response.
		return &pipeline.Outcome{
			SessionID: session.ID,
			Content:   skill.Body,
		}
	case skills.Structured:
		return a.executeToolChain(ctx, session, skill, content)
	}
	return nil
}

// executeToolChain runs each step in a structured skill's tool chain sequentially,
// passing the previous step's output as context to the next step via a params
// substitution variable. Returns nil to fall through to inference if the skill
// has no tool chain or the tool registry is unavailable.
func (a *skillAdapter) executeToolChain(ctx context.Context, sess *session.Session, skill *skills.Skill, userInput string) *pipeline.Outcome {
	chain := skill.Manifest.ToolChain
	if len(chain) == 0 || a.tools == nil {
		log.Debug().Str("skill", skill.Name()).Msg("structured skill has no tool chain or no tool registry; falling through to inference")
		return nil
	}

	tctx := &agent.ToolContext{
		SessionID: sess.ID,
		AgentName: sess.AgentName,
	}

	var lastOutput string
	for i, step := range chain {
		tool := a.tools.Get(step.ToolName)
		if tool == nil {
			log.Warn().Str("tool", step.ToolName).Int("step", i).Str("skill", skill.Name()).Msg("tool not found in registry; aborting skill chain")
			return &pipeline.Outcome{
				SessionID: sess.ID,
				Content:   fmt.Sprintf("Skill %q failed: tool %q not found (step %d)", skill.Name(), step.ToolName, i+1),
			}
		}

		// Build params JSON: merge default params with dynamic substitutions.
		params := a.buildParams(step.Params, userInput, lastOutput)

		result, err := tool.Execute(ctx, params, tctx)
		if err != nil {
			log.Warn().Err(err).Str("tool", step.ToolName).Int("step", i).Str("skill", skill.Name()).Msg("tool chain step failed")
			return &pipeline.Outcome{
				SessionID: sess.ID,
				Content:   fmt.Sprintf("Skill %q failed at step %d (%s): %v", skill.Name(), i+1, step.ToolName, err),
			}
		}

		if result != nil {
			lastOutput = result.Output
		}
	}

	if lastOutput == "" {
		lastOutput = fmt.Sprintf("Skill %q completed successfully.", skill.Name())
	}

	return &pipeline.Outcome{
		SessionID: sess.ID,
		Content:   lastOutput,
	}
}

// buildParams constructs a JSON params string for a tool invocation.
// It substitutes {{input}} with the user's message and {{previous}} with the
// output of the previous tool chain step.
func (a *skillAdapter) buildParams(defaults map[string]string, userInput, previousOutput string) string {
	if len(defaults) == 0 {
		// No explicit params — pass the user input directly.
		return userInput
	}

	resolved := make(map[string]string, len(defaults))
	for k, v := range defaults {
		v = strings.ReplaceAll(v, "{{input}}", userInput)
		v = strings.ReplaceAll(v, "{{previous}}", previousOutput)
		resolved[k] = v
	}

	data, err := json.Marshal(resolved)
	if err != nil {
		return userInput
	}
	return string(data)
}

// streamAdapter wraps agent context builder deps → pipeline.StreamPreparer.
//
// v1.0.6: retriever and store were removed. See executorAdapter note.
type streamAdapter struct {
	llmSvc       *llm.Service
	tools        *agent.ToolRegistry
	promptConfig agent.PromptConfig
	budgetCfg    *core.ContextBudgetConfig
}

func (a *streamAdapter) PrepareStream(ctx context.Context, sess *session.Session) (*llm.Request, error) {
	ctxBuilder := buildAgentContext(ctx, sess, a.tools, a.promptConfig, a.budgetCfg)
	req := ctxBuilder.BuildRequest(sess)
	req.Stream = true
	return req, nil
}
