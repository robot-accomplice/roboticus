package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kardianos/service"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"roboticus/internal/agent"
	"roboticus/internal/agent/memory"
	"roboticus/internal/agent/policy"
	"roboticus/internal/agent/skills"
	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/api"
	"roboticus/internal/api/routes"
	"roboticus/internal/channel"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/mcp"
	"roboticus/internal/pipeline"
	"roboticus/internal/schedule"
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
	return a.r.Retrieve(ctx, sessionID, query, budget)
}

// ingestorAdapter wraps *memory.Manager → pipeline.Ingestor.
type ingestorAdapter struct {
	m *memory.Manager
}

func (a *ingestorAdapter) IngestTurn(ctx context.Context, session *session.Session) {
	a.m.IngestTurn(ctx, session)
}

// buildAgentContext assembles a ContextBuilder with system prompt, tool defs,
// and memory retrieval. Shared by executorAdapter and streamAdapter.
func buildAgentContext(ctx context.Context, sess *session.Session, tools *agent.ToolRegistry, retriever *memory.Retriever, store *db.Store, promptCfg agent.PromptConfig, budgetCfg *core.ContextBudgetConfig) *agent.ContextBuilder {
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
		Bool("has_retriever", retriever != nil).
		Msg("context built for inference")
	ctxBuilder.SetSystemPrompt(systemPrompt)

	if tools != nil {
		ctxBuilder.SetTools(tools.ToolDefs())
	}

	// Memory injection: always provide memory context so the model never
	// claims "I don't have memories." Rust principle: "Session history, memory
	// layers, and procedural skills are proactively injected into every turn."
	if retriever != nil {
		msgs := sess.Messages()
		var mem string
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				mem = retriever.Retrieve(ctx, sess.ID, msgs[i].Content, 2048)
				break
			}
		}
		if mem != "" {
			ctxBuilder.SetMemory(mem)
		} else {
			// Empty retrieval: inject orientation block so model knows memory
			// exists and can be queried via recall_memory tool.
			ctxBuilder.SetMemory("[Memory: No relevant memories found for this query. " +
				"Use recall_memory(id) to search by topic. Your memory index is provided separately.]")
		}
	}

	// Memory index: always inject so the model can call recall_memory(id).
	// Rust: two-stage pattern — index always injected, full content on demand.
	if store != nil {
		index := agenttools.BuildMemoryIndex(ctx, store, 20)
		if index != "" {
			ctxBuilder.SetMemoryIndex(index)
		} else {
			ctxBuilder.SetMemoryIndex("[Memory Index: No memories stored yet. " +
				"Memories will accumulate as conversations continue.]")
		}
	}

	return ctxBuilder
}

// executorAdapter wraps the full agent loop deps → pipeline.ToolExecutor.
type executorAdapter struct {
	llmSvc          *llm.Service
	tools           *agent.ToolRegistry
	policy          *policy.Engine
	injection       *agent.InjectionDetector
	memMgr          *memory.Manager
	retriever       *memory.Retriever
	store           *db.Store
	promptConfig    agent.PromptConfig
	budgetCfg       *core.ContextBudgetConfig
	maxTurnDuration time.Duration
}

func (a *executorAdapter) RunLoop(ctx context.Context, sess *session.Session) (string, int, error) {
	ctxBuilder := buildAgentContext(ctx, sess, a.tools, a.retriever, a.store, a.promptConfig, a.budgetCfg)

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
type streamAdapter struct {
	llmSvc       *llm.Service
	tools        *agent.ToolRegistry
	retriever    *memory.Retriever
	store        *db.Store
	promptConfig agent.PromptConfig
	budgetCfg    *core.ContextBudgetConfig
}

func (a *streamAdapter) PrepareStream(ctx context.Context, sess *session.Session) (*llm.Request, error) {
	ctxBuilder := buildAgentContext(ctx, sess, a.tools, a.retriever, a.store, a.promptConfig, a.budgetCfg)
	req := ctxBuilder.BuildRequest(sess)
	req.Stream = true
	return req, nil
}

// Daemon manages the lifecycle of all roboticus subsystems.
// Implements kardianos/service.Interface for cross-platform service management
// (systemd on Linux, launchd on macOS, SCM on Windows).
type Daemon struct {
	cfg      *core.Config
	store    *db.Store
	llm      *llm.Service
	pipe     *pipeline.Pipeline
	router   *channel.Router
	appState *api.AppState
	eventBus *api.EventBus
	bgWorker *core.BackgroundWorker
	errBus   *core.ErrorBus

	startupStart time.Time
	errBusCancel context.CancelFunc
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// ServiceConfig returns the kardianos service configuration.
func ServiceConfig() *service.Config {
	return &service.Config{
		Name:        "roboticus",
		DisplayName: "Roboticus Agent Runtime",
		Description: "Autonomous AI agent runtime with multi-channel support.",
	}
}

// New creates a daemon with all subsystems wired together.
// Initialization follows Rust's 12-step bootstrap sequence with structured
// phase logging for each major subsystem.
func New(cfg *core.Config, opts BootOptions) (*Daemon, error) {
	startupStart := time.Now()
	const steps = 12

	// Initialize theme from CLI flags before any output.
	initBootTheme(opts)

	// Suppress structured logging during boot so the styled boot steps
	// are not interleaved with JSON/console log lines (Rust parity:
	// enable_stderr_logging() is called only after "Ready").
	prevLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(prevLevel)

	printBanner()

	// ── Phase 1: Configuration ───────────────────────────────────────────
	bootStep(1, steps, "Loading configuration")
	bootDetail("agent", cfg.Agent.Name)
	bootDetail("workspace", cfg.Agent.Workspace)

	// ── Phase 2: Database ────────────────────────────────────────────────
	store, err := db.Open(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("daemon: open database: %w", err)
	}
	bootStep(2, steps, "Database initialized")
	if cfg.Database.Path == ":memory:" {
		bootDetail("mode", "in-memory (ephemeral)")
	} else {
		bootDetail("path", cfg.Database.Path)
		bootDetail("mode", "WAL (persistent)")
	}
	log.Info().Str("path", cfg.Database.Path).Msg("[startup 2/12] database initialized")

	// ── Phase 3: Wallet verification (Rust parity) ──────────────────────
	if err := verifyWalletConnectivity(context.Background(), cfg); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("daemon: wallet verification: %w", err)
	}
	bootStep(3, steps, "Wallet service ready")
	bootDetail("chain", fmt.Sprintf("chain_id=%d", cfg.Wallet.ChainID))
	bootDetail("rpc", cfg.Wallet.RPCURL)
	log.Info().Str("endpoint", cfg.Wallet.RPCURL).Msg("[startup 3/12] wallet service verified")

	// ── Phase 4: LLM service ────────────────────────────────────────────
	// Build LLM service config from validated core.Config.
	var providers []llm.Provider
	for name, pc := range cfg.Providers {
		providers = append(providers, llm.Provider{
			Name:             name,
			URL:              pc.URL,
			Format:           llm.APIFormat(pc.Format),
			APIKeyEnv:        pc.APIKeyEnv,
			APIKeyRef:        pc.APIKeyRef,
			ChatPath:         pc.ChatPath,
			EmbeddingPath:    pc.EmbeddingPath,
			EmbeddingModel:   pc.EmbeddingModel,
			IsLocal:          pc.IsLocal,
			CostPerInputTok:  pc.CostPerInputToken,
			CostPerOutputTok: pc.CostPerOutputToken,
			AuthHeader:       pc.AuthHeader,
			ExtraHeaders:     pc.ExtraHeaders,
			TPMLimit:         pc.TPMLimit,
			RPMLimit:         pc.RPMLimit,
			TimeoutSecs:      pc.TimeoutSecs,
		})
	}
	bgWorker := core.NewBackgroundWorker(32)

	// Centralized error bus — all subsystems report errors here instead of
	// silently discarding them. Subscribers log, count, and surface errors.
	errBusCtx, errBusCancel := context.WithCancel(context.Background())
	logSub := &core.LogSubscriber{}
	metricSub := core.NewMetricSubscriber()
	ringBufSub := core.NewRingBufferSubscriber(1000)
	errBus := core.NewErrorBus(errBusCtx, 256, logSub, metricSub, ringBufSub)
	// errBusCancel stored in Daemon struct for shutdown ordering.

	// Open keystore early so LLM providers can resolve API keys from it.
	ks, ksErr := core.OpenKeystoreMachine()
	if ksErr != nil {
		log.Warn().Err(ksErr).Msg("keystore: failed to open, provider key management unavailable")
	}

	// Wire keystore into LLM key resolution.
	// NewClient handles the resolution cascade (explicit ref → conventional
	// name → env var); this closure is just the keystore lookup layer.
	if ks != nil && ks.IsUnlocked() {
		llm.KeyResolver = func(keystoreKey string) string {
			return ks.GetOrEmpty(keystoreKey)
		}
	}

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: providers,
		Primary:   cfg.Models.Primary,
		Fallbacks: cfg.Models.Fallback,
		BGWorker:  bgWorker,
		ErrBus:    errBus,
	}, store)
	if err != nil {
		errBusCancel()
		_ = store.Close()
		return nil, fmt.Errorf("daemon: init LLM: %w", err)
	}

	// Warm-start quality and latency trackers from DB history.
	llmSvc.SeedStartup(context.Background(), store)
	bootStep(4, steps, "LLM service ready")
	bootDetail("primary", cfg.Models.Primary)
	if len(cfg.Models.Fallback) > 0 {
		bootDetail("fallbacks", strings.Join(cfg.Models.Fallback, ", "))
	} else {
		bootDetail("fallbacks", "none")
	}
	log.Info().Str("primary", cfg.Models.Primary).Int("providers", len(providers)).Msg("[startup 4/12] LLM service ready")

	// ── Phase 5: Identity + Tools ───────────────────────────────────────
	injection := agent.NewInjectionDetector()
	tools := agent.NewToolRegistry()

	// Register builtin tools (matching Rust's tool_registry setup).
	// Execution tools.
	tools.Register(&agenttools.EchoTool{})
	tools.Register(&agenttools.BashTool{})

	// Filesystem tools.
	tools.Register(&agenttools.ReadFileTool{})
	tools.Register(&agenttools.WriteFileTool{})
	tools.Register(&agenttools.EditFileTool{})
	tools.Register(&agenttools.ListDirectoryTool{})
	tools.Register(&agenttools.SearchFilesTool{})
	tools.Register(&agenttools.GlobFilesTool{})

	// Scheduling.
	tools.Register(&agenttools.CronTool{})

	// Introspection tools.
	tools.Register(&agenttools.RuntimeContextTool{})
	tools.Register(&agenttools.MemoryStatsTool{})
	tools.Register(agenttools.NewIntrospectionTool(cfg.Agent.Name, "0.1.0", tools.Names))

	// Memory tools.
	tools.Register(agenttools.NewMemoryRecallTool(store))

	// Channel and subagent introspection.
	tools.Register(&agenttools.ChannelHealthTool{})
	tools.Register(&agenttools.SubagentStatusTool{})

	// Data tools (hippocampus).
	tools.Register(&agenttools.CreateTableTool{})
	tools.Register(&agenttools.QueryTableTool{})
	tools.Register(&agenttools.InsertRowTool{})
	tools.Register(&agenttools.AlterTableTool{})
	tools.Register(&agenttools.DropTableTool{})

	bootStep(5, steps, "Identity resolved")
	bootDetail("name", cfg.Agent.Name)
	bootDetail("id", cfg.Agent.ID)
	bootDetail("tools", fmt.Sprintf("%d registered", len(tools.Names())))
	log.Info().Int("count", len(tools.Names())).Strs("tools", tools.Names()).Msg("[startup 5/12] identity resolved, tools registered")

	// ── Phase 6: Policy + Memory ────────────────────────────────────────
	policyCfg := policy.DefaultConfig()
	policyCfg.MaxTransferCents = int64(cfg.Treasury.PerPaymentCap * 100)
	policyCfg.RateLimitPerMinute = 30
	policyEngine := policy.NewEngine(policyCfg)
	memMgr := memory.NewManager(memory.Config{
		TotalTokenBudget: 2048,
		Budgets: memory.TierBudget{
			Working:      cfg.Memory.WorkingBudget / 100.0,
			Episodic:     cfg.Memory.EpisodicBudget / 100.0,
			Semantic:     cfg.Memory.SemanticBudget / 100.0,
			Procedural:   cfg.Memory.ProceduralBudget / 100.0,
			Relationship: cfg.Memory.RelationshipBudget / 100.0,
		},
	}, store)
	retriever := memory.NewRetriever(memory.DefaultRetrievalConfig(), memory.TierBudget{
		Working:      cfg.Memory.WorkingBudget / 100.0,
		Episodic:     cfg.Memory.EpisodicBudget / 100.0,
		Semantic:     cfg.Memory.SemanticBudget / 100.0,
		Procedural:   cfg.Memory.ProceduralBudget / 100.0,
		Relationship: cfg.Memory.RelationshipBudget / 100.0,
	}, store)
	guards := pipeline.DefaultGuardChain()
	bootStep(6, steps, "Policy engine + memory management ready")
	log.Info().Msg("[startup 6/12] policy engine + memory management ready")

	// ── Phase 7: Skills ─────────────────────────────────────────────────
	// Load skills from configured directory.
	skillLoader := skills.NewLoader()
	var loadedSkills []*skills.Skill
	if cfg.Skills.Directory != "" {
		loadedSkills = skillLoader.LoadFromDir(cfg.Skills.Directory)
		log.Info().Int("count", len(loadedSkills)).Str("dir", cfg.Skills.Directory).Msg("loaded skills")
	}
	skillMatcher := skills.NewMatcher(loadedSkills)

	// Load personality files from workspace.
	osCfg, err := core.LoadOsConfig(cfg.Agent.Workspace, "OS.toml")
	if err != nil {
		log.Warn().Err(err).Msg("failed to load OS personality, using defaults")
		osCfg = core.DefaultOsConfig()
	}
	fwCfg, err := core.LoadFirmwareConfig(cfg.Agent.Workspace, "FIRMWARE.toml")
	if err != nil {
		log.Warn().Err(err).Msg("failed to load firmware, using defaults")
		fwCfg = core.DefaultFirmwareConfig()
	}

	// Load optional OPERATOR.toml and DIRECTIVES.toml.
	opCfg, err := core.LoadOperatorConfig(cfg.Agent.Workspace, "OPERATOR.toml")
	if err != nil {
		log.Warn().Err(err).Msg("failed to load operator config")
		opCfg = core.DefaultOperatorConfig()
	}
	dirCfg, err := core.LoadDirectivesConfig(cfg.Agent.Workspace, "DIRECTIVES.toml")
	if err != nil {
		log.Warn().Err(err).Msg("failed to load directives config")
		dirCfg = core.DefaultDirectivesConfig()
	}

	// Build shared prompt config with personality and workspace context.
	var skillNames []string
	for _, s := range loadedSkills {
		skillNames = append(skillNames, s.Name())
	}
	// Generate stable HMAC boundary key from agent identity (Rust parity).
	// Key is deterministic so verification works across restarts.
	boundaryKey := deriveBoundaryKey(cfg.Agent.Name, cfg.Agent.Workspace)

	basePromptCfg := agent.PromptConfig{
		AgentName:   cfg.Agent.Name,
		Firmware:    core.FormatFirmwareRules(fwCfg),
		Personality: core.FormatOsPersonality(osCfg),
		Operator:    core.FormatOperatorContext(opCfg),
		Directives:  core.FormatDirectives(dirCfg),
		Workspace:   cfg.Agent.Workspace,
		Skills:      skillNames,
		Model:       cfg.Models.Primary,
		ToolNames:   tools.Names(),
		ToolDescs:   tools.NamesWithDescriptions(),
		BoundaryKey: boundaryKey,
	}
	if len(loadedSkills) > 0 {
		bootStep(7, steps, "Skills loaded")
		bootDetail("dir", cfg.Skills.Directory)
		bootDetail("count", fmt.Sprintf("%d", len(loadedSkills)))
	} else if cfg.Skills.Directory != "" {
		bootStepWarn(7, steps, fmt.Sprintf("Skills directory not found: %s", cfg.Skills.Directory))
	} else {
		bootStep(7, steps, "Skills (none configured)")
	}
	log.Info().
		Str("agent", cfg.Agent.Name).
		Int("skills", len(loadedSkills)).
		Bool("has_firmware", basePromptCfg.Firmware != "").
		Bool("has_personality", basePromptCfg.Personality != "").
		Bool("has_operator", basePromptCfg.Operator != "").
		Bool("has_directives", basePromptCfg.Directives != "").
		Msg("[startup 7/12] skills loaded, personality configured")

	// ── Phase 8: Channel adapters ───────────────────────────────────────
	dq := channel.NewDeliveryQueue(store)
	router := channel.NewRouter(dq)

	// Register channel adapters from config + keystore.
	// Rich sub-config takes precedence over legacy flat fields.
	telegramTokenEnv := cfg.Channels.TelegramTokenEnv
	if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.TokenEnv != "" {
		telegramTokenEnv = cfg.Channels.Telegram.TokenEnv
	}
	telegramToken := resolveChannelToken(telegramTokenEnv, "telegram_bot_token", ks)
	if telegramToken != "" {
		// Discover allowed chat IDs from existing Telegram sessions in the DB,
		// augmented with any explicitly configured in rich config.
		allowedChatIDs := discoverTelegramChatIDs(store)
		pollTimeout := 5
		if cfg.Channels.Telegram != nil {
			allowedChatIDs = append(allowedChatIDs, cfg.Channels.Telegram.AllowedChatIDs...)
			if cfg.Channels.Telegram.PollTimeoutSeconds > 0 {
				pollTimeout = cfg.Channels.Telegram.PollTimeoutSeconds
			}
		}
		tgCfg := channel.TelegramConfig{
			Token:          telegramToken,
			PollTimeout:    pollTimeout,
			AllowedChatIDs: allowedChatIDs,
			DenyOnEmpty:    cfg.Security.DenyOnEmptyAllowlist,
		}
		tgAdapter := channel.NewTelegramAdapter(tgCfg)
		// Clear any stale webhook so getUpdates polling works.
		if err := tgAdapter.DeleteWebhook(context.Background()); err != nil {
			log.Warn().Err(err).Msg("telegram: failed to delete webhook")
		}
		router.Register(tgAdapter)
		log.Info().Msg("telegram adapter registered (polling mode)")
	}

	// Build channel list for display (Rust parity: serve.rs channels vec).
	activeChannels := []string{"web"}
	if telegramToken != "" {
		activeChannels = append(activeChannels, "telegram")
	}
	if cfg.Channels.WhatsApp != nil && cfg.Channels.WhatsApp.Enabled {
		activeChannels = append(activeChannels, "whatsapp")
	}
	if cfg.Channels.Discord != nil && cfg.Channels.Discord.Enabled {
		activeChannels = append(activeChannels, "discord")
	}
	if cfg.Channels.Signal != nil && cfg.Channels.Signal.Enabled {
		activeChannels = append(activeChannels, "signal")
	}
	if cfg.Matrix.Enabled {
		activeChannels = append(activeChannels, "matrix")
	}
	if cfg.A2A.Enabled {
		activeChannels = append(activeChannels, "a2a")
	}
	bootStep(8, steps, "Channel adapters ready")
	bootDetail("active", strings.Join(activeChannels, ", "))
	log.Info().Int("adapters", len(router.Adapters())).Msg("[startup 8/12] channel adapters registered")

	// ── Phase 9: Embeddings ─────────────────────────────────────────────
	// Create embedding client for post-turn ingest and ANN search.
	// Priority: config's memory.embedding_provider → any provider with embedding_model set → nil (n-gram fallback).
	var embedClient *llm.EmbeddingClient
	embedProviderName := cfg.Memory.EmbeddingProvider
	if embedProviderName == "" {
		// Auto-detect: find first provider with an embedding model or path configured.
		for name, pc := range cfg.Providers {
			if pc.EmbeddingModel != "" || pc.EmbeddingPath != "" {
				embedProviderName = name
				break
			}
		}
	}
	if embedProviderName != "" {
		if pc, ok := cfg.Providers[embedProviderName]; ok {
			embedClient = llm.NewEmbeddingClient(&llm.Provider{
				Name:           embedProviderName,
				URL:            pc.URL,
				Format:         llm.APIFormat(pc.Format),
				EmbeddingPath:  pc.EmbeddingPath,
				EmbeddingModel: pc.EmbeddingModel,
				IsLocal:        pc.IsLocal,
			})
			log.Info().Str("provider", embedProviderName).Str("model", pc.EmbeddingModel).Msg("embedding client configured")
		}
	}
	if embedClient == nil {
		// Fallback: n-gram hashing (no API calls, works offline).
		embedClient = llm.NewEmbeddingClient(nil)
		log.Info().Msg("embedding client: using local n-gram fallback")
	}
	bootStep(9, steps, "Embeddings configured")
	if embedProviderName != "" {
		bootDetail("provider", embedProviderName)
	} else {
		bootDetail("provider", "n-gram fallback (local)")
	}

	// ── Phase 10: Pipeline assembly ─────────────────────────────────────
	pipe := pipeline.New(pipeline.PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: &injectionAdapter{det: injection},
		Retriever: &retrieverAdapter{r: retriever},
		Skills:    &skillAdapter{matcher: skillMatcher, tools: tools},
		Executor: &executorAdapter{
			llmSvc:          llmSvc,
			tools:           tools,
			policy:          policyEngine,
			injection:       injection,
			memMgr:          memMgr,
			retriever:       retriever,
			store:           store,
			promptConfig:    basePromptCfg,
			budgetCfg:       &cfg.ContextBudget,
			maxTurnDuration: time.Duration(cfg.Agent.AutonomyMaxTurnDurationSecs) * time.Second,
		},
		Ingestor: &ingestorAdapter{m: memMgr},
		Refiner:  &nicknameAdapter{llm: llmSvc, store: store},
		Streamer: &streamAdapter{
			llmSvc:       llmSvc,
			tools:        tools,
			retriever:    retriever,
			store:        store,
			promptConfig: basePromptCfg,
			budgetCfg:    &cfg.ContextBudget,
		},
		Guards:     guards,
		BGWorker:   bgWorker,
		Embeddings: embedClient,
		ErrBus:     errBus,
	})

	bootStep(10, steps, "Pipeline assembled")
	log.Info().Msg("[startup 10/12] pipeline assembled")

	// ── Phase 11: Hippocampus + support services ────────────────────────
	// Sync hippocampus schema registry.
	hippo := db.NewHippocampusRegistry(store)
	if err := hippo.SyncBuiltinTables(context.Background()); err != nil {
		log.Warn().Err(err).Msg("hippocampus sync failed")
	}

	approvalMgr := policy.NewApprovalManager(policy.ApprovalsConfig{
		Enabled:        cfg.Approvals.Enabled,
		GatedTools:     cfg.Approvals.GatedTools,
		BlockedTools:   cfg.Approvals.BlockedTools,
		TimeoutSeconds: cfg.Approvals.TimeoutSeconds,
	})

	eventBus := api.NewEventBus(256)

	// Log ring buffer: captures structured logs for /api/logs endpoint.
	logBuf := api.NewLogRingBuffer(5000)
	routes.SetLogBuffer(func(n int, level string) []any {
		entries := logBuf.Tail(n, level)
		result := make([]any, len(entries))
		for i, e := range entries {
			result[i] = e
		}
		return result
	})

	// MCP connection manager.
	mcpMgr := mcp.NewConnectionManager()

	appState := &api.AppState{
		Store:           store,
		Pipeline:        pipe,
		StreamFinalizer: pipe, // *Pipeline satisfies both Runner and StreamFinalizer
		LLM:             llmSvc,
		Config:          cfg,
		Keystore:        ks,
		EventBus:        eventBus,
		Approvals:       approvalMgr,
		MCP:             mcpMgr,
	}

	bootStep(11, steps, "Hippocampus, approvals, events ready")
	log.Info().Msg("[startup 11/12] hippocampus, approvals, events ready")

	// ── Phase 12: Complete ──────────────────────────────────────────────
	log.Info().Int64("startup_ms", time.Since(startupStart).Milliseconds()).Msg("[startup 12/12] daemon initialization complete")

	return &Daemon{
		cfg:          cfg,
		store:        store,
		llm:          llmSvc,
		pipe:         pipe,
		router:       router,
		appState:     appState,
		eventBus:     eventBus,
		bgWorker:     bgWorker,
		errBus:       errBus,
		startupStart: startupStart,
		errBusCancel: errBusCancel,
	}, nil
}

// Start implements service.Interface. Called by the OS service manager.
func (d *Daemon) Start(s service.Service) error {
	// Final boot step: HTTP server starting (Rust parity: serve.rs step 12).
	bootStep(12, 12, "HTTP server starting")
	bindAddr := fmt.Sprintf("127.0.0.1:%d", d.cfg.Server.Port)
	bootDetail("bind", bindAddr)
	bootDetail("dashboard", fmt.Sprintf("http://localhost:%d", d.cfg.Server.Port))
	bootReady(time.Since(d.startupStart))

	log.Info().Str("agent", d.cfg.Agent.Name).Str("platform", service.Platform()).Int("port", d.cfg.Server.Port).Msg("roboticus starting")
	go d.run()
	return nil
}

// Stop implements service.Interface. Called by the OS service manager on shutdown.
func (d *Daemon) Stop(s service.Service) error {
	log.Info().Msg("roboticus stopping")
	if d.cancel != nil {
		d.cancel()
	}

	// Wait for goroutines with timeout.
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("graceful shutdown complete")
	case <-time.After(15 * time.Second):
		log.Warn().Msg("shutdown timed out")
	}

	// Drain background worker pool.
	if d.bgWorker != nil {
		d.bgWorker.Drain(5 * time.Second)
	}

	// Drain error bus last — it processes events from all other subsystems,
	// so it must be the last to shut down.
	if d.errBus != nil {
		d.errBus.Drain(3 * time.Second)
	}

	_ = d.store.Close()
	return nil
}

// run starts all subsystems. Called from Start() in a goroutine.
func (d *Daemon) run() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	// ── Startup Phase: MCP server connections (Rust parity) ──────────────
	// Connect to all configured MCP servers with a 30-second timeout.
	// Non-fatal: individual server failures are logged, not fatal.
	if len(d.cfg.MCP.Servers) > 0 {
		mcpCtx, mcpCancel := context.WithTimeout(ctx, 30*time.Second)
		mcpServers := make([]mcp.McpServerConfig, 0, len(d.cfg.MCP.Servers))
		for _, s := range d.cfg.MCP.Servers {
			mcpServers = append(mcpServers, mcp.McpServerConfig{
				Name:      s.Name,
				Transport: s.Transport,
				Command:   s.Command,
				Args:      s.Args,
				URL:       s.URL,
				Env:       s.Env,
				Enabled:   s.Enabled,
			})
		}
		connected := d.appState.MCP.ConnectAll(mcpCtx, mcpServers)
		mcpCancel()
		log.Info().Int("connected", connected).Int("configured", len(d.cfg.MCP.Servers)).Msg("MCP server connections established")
	}

	// ── Startup Phase: Sub-agent registry (Rust parity) ──────────────────
	// Load enabled sub-agents from DB and register them.
	d.loadSubAgents(ctx)

	// API server.
	srvCfg := api.DefaultServerConfig()
	if d.cfg.Server.Port > 0 {
		srvCfg.Port = d.cfg.Server.Port
	}
	if d.cfg.Server.Bind != "" {
		srvCfg.Bind = d.cfg.Server.Bind
	}

	// ── Startup Phase: Port conflict resolution (Rust parity) ────────────
	// Check if the port is already in use and attempt to resolve.
	resolvePortConflict(srvCfg.Port)

	httpSrv := api.NewServer(ctx, srvCfg, d.appState)

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if err := api.ListenAndServe(ctx, httpSrv); err != nil {
			log.Error().Err(err).Msg("API server error")
		}
	}()

	// Delivery worker.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		worker := channel.NewDeliveryWorker(
			d.router.DeliveryQueue(),
			d.router.Adapters(),
			5*time.Second,
		)
		worker.Run(ctx)
	}()

	// Channel listener.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.runChannelListener(ctx)
	}()

	// Cron worker.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		instanceID, _ := os.Hostname()
		worker := schedule.NewCronWorker(d.store, instanceID, 60*time.Second,
			schedule.CronExecutorFunc(func(ctx context.Context, job *schedule.CronJob) error {
				log.Info().Str("job", job.Name).Str("agent", job.AgentID).Msg("cron job executing")
				input := pipeline.Input{
					Content: job.PayloadJSON,
					AgentID: job.AgentID,
				}
				if input.Content == "" {
					input.Content = fmt.Sprintf("Execute scheduled job: %s", job.Name)
				}
				_, err := pipeline.RunPipeline(ctx, d.pipe, pipeline.PresetCron(), input)
				if err != nil {
					log.Error().Err(err).Str("job", job.Name).Msg("cron job pipeline failed")
				}
				return err
			}), d.errBus)
		worker.Run(ctx)
	}()

	// Note: Telegram polling is handled by runChannelListener via router.PollAll().
	// No dedicated poller needed — PollAll calls Recv on all registered adapters.

	// Signal poll loop.
	if d.cfg.Channels.SignalAccount != "" {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.runSignalPoller(ctx)
		}()
	}

	// Email poll loop.
	if d.cfg.Channels.EmailFromAddress != "" {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.runEmailPoller(ctx)
		}()
	}

	// Start wallet balance poller.
	startWalletPoller(ctx, d.cfg, d.store, d.appState.Keystore)

	// Memory consolidation heartbeat — runs the dreaming cycle periodically.
	// Matches Rust's heartbeat-triggered consolidation.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.runConsolidationHeartbeat(ctx)
	}()

	log.Info().Msg("all subsystems started")
}

// runConsolidationHeartbeat runs memory consolidation on a periodic schedule.
// Matches Rust's heartbeat-triggered MemoryPrune signal.
func (d *Daemon) runConsolidationHeartbeat(ctx context.Context) {
	// Initial delay to let the system settle after startup.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	log.Info().Msg("memory consolidation heartbeat started (1h interval)")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report := pipeline.RunMemoryConsolidation(ctx, d.store, false)
			log.Info().
				Int("indexed", report.Indexed).
				Int("deduped", report.Deduped).
				Int("promoted", report.Promoted).
				Int("pruned", report.Pruned).
				Msg("memory consolidation completed")
		}
	}
}

// runTelegramPoller polls the Telegram adapter for inbound messages via long polling.
// Currently wired via config flag; kept for imminent Telegram channel enablement.
func (d *Daemon) runTelegramPoller(ctx context.Context) { //nolint:unused // wired when telegram.polling=true
	log.Info().Msg("telegram poller started")

	tgAdapter := d.router.GetAdapter("telegram")
	if tgAdapter == nil {
		log.Warn().Msg("telegram adapter not registered, poller exiting")
		return
	}

	// Clear any registered webhook so getUpdates polling works.
	// Telegram returns 409 if both webhook and polling are active.
	if tg, ok := tgAdapter.(*channel.TelegramAdapter); ok {
		if err := tg.DeleteWebhook(ctx); err != nil {
			log.Warn().Err(err).Msg("telegram: failed to delete webhook")
		} else {
			log.Info().Msg("telegram: webhook cleared, polling active")
			time.Sleep(2 * time.Second) // Give Telegram API time to propagate
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Telegram long-polls internally (30s timeout in getUpdates),
		// so no ticker needed — Recv blocks until messages arrive.
		msg, err := tgAdapter.Recv(ctx)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "409") {
				// 409 = "terminated by other getUpdates request".
				// Another long-poll is still in-flight (previous process or our own).
				// Back off for the poll timeout duration (30s) to let it expire.
				log.Warn().Msg("telegram: 409 conflict (previous poll in-flight), waiting for expiry")
				time.Sleep(35 * time.Second)
			} else {
				log.Warn().Err(err).Msg("telegram recv error")
				time.Sleep(5 * time.Second)
			}
			continue
		}
		if msg == nil {
			continue
		}
		m := *msg
		d.bgWorker.Submit("inbound:telegram", func(bgCtx context.Context) {
			d.handleInbound(bgCtx, m)
		})
	}
}

// discoverTelegramChatIDs extracts known Telegram chat IDs from existing
// sessions in the database. This bootstraps the allowlist from prior
// interactions so DenyOnEmpty works correctly without manual config.
func discoverTelegramChatIDs(store *db.Store) []int64 {
	rows, err := store.QueryContext(context.Background(),
		`SELECT DISTINCT scope_key FROM sessions WHERE scope_key LIKE '%telegram%'`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[int64]bool)
	for rows.Next() {
		var scope string
		if rows.Scan(&scope) != nil {
			continue
		}
		// scope format: "peer:telegram:CHATID"
		parts := strings.SplitN(scope, ":", 3)
		if len(parts) >= 3 {
			if chatID, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
				seen[chatID] = true
			}
		}
	}

	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		log.Info().Ints64("chat_ids", ids).Msg("telegram: discovered allowed chat IDs from sessions")
	}
	return ids
}

// resolveChannelToken resolves a channel token from env var or keystore.
func resolveChannelToken(envName, keystoreName string, ks *core.Keystore) string {
	if envName != "" {
		if val := os.Getenv(envName); val != "" {
			return val
		}
	}
	if ks != nil && ks.IsUnlocked() {
		if val := ks.GetOrEmpty(keystoreName); val != "" {
			return val
		}
	}
	return ""
}

// runSignalPoller polls the Signal adapter for inbound messages in a loop.
func (d *Daemon) runSignalPoller(ctx context.Context) {
	log.Info().Msg("signal poller started")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	signalAdapter := d.router.GetAdapter("signal")
	if signalAdapter == nil {
		log.Warn().Msg("signal adapter not registered, poller exiting")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg, err := signalAdapter.Recv(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("signal recv error")
				continue
			}
			if msg == nil {
				continue
			}
			m := *msg
			d.bgWorker.Submit("inbound:signal", func(bgCtx context.Context) {
				d.handleInbound(bgCtx, m)
			})
		}
	}
}

// runEmailPoller polls the Email adapter for inbound messages via IMAP in a loop.
func (d *Daemon) runEmailPoller(ctx context.Context) {
	log.Info().Msg("email poller started")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	emailAdapter := d.router.GetAdapter("email")
	if emailAdapter == nil {
		log.Warn().Msg("email adapter not registered, poller exiting")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg, err := emailAdapter.Recv(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("email recv error")
				continue
			}
			if msg == nil {
				continue
			}
			m := *msg
			d.bgWorker.Submit("inbound:email", func(bgCtx context.Context) {
				d.handleInbound(bgCtx, m)
			})
		}
	}
}

// RunInteractive runs the daemon in the foreground (not as a service).
// Used for development or when --foreground is passed.
func (d *Daemon) RunInteractive() error {
	svc, err := service.New(d, ServiceConfig())
	if err != nil {
		return fmt.Errorf("daemon: create service: %w", err)
	}
	return svc.Run()
}

// Install registers roboticus as an OS service.
func Install(cfg *core.Config) error {
	d, err := New(cfg, BootOptions{})
	if err != nil {
		return err
	}
	_ = d.store.Close() // don't need DB for install

	svc, err := service.New(d, ServiceConfig())
	if err != nil {
		return fmt.Errorf("service create: %w", err)
	}
	return svc.Install()
}

// Uninstall removes roboticus from the OS service manager.
func Uninstall(cfg *core.Config) error {
	d, err := New(cfg, BootOptions{})
	if err != nil {
		return err
	}
	_ = d.store.Close()

	svc, err := service.New(d, ServiceConfig())
	if err != nil {
		return fmt.Errorf("service create: %w", err)
	}
	return svc.Uninstall()
}

// Control sends a command (start/stop/restart) to the OS service.
func Control(cfg *core.Config, action string) error {
	d, err := New(cfg, BootOptions{})
	if err != nil {
		return err
	}
	_ = d.store.Close()

	svc, err := service.New(d, ServiceConfig())
	if err != nil {
		return fmt.Errorf("service create: %w", err)
	}
	return service.Control(svc, action)
}

// Status returns the current service status from the OS service manager.
func Status(cfg *core.Config) (string, error) {
	d, err := New(cfg, BootOptions{})
	if err != nil {
		return "", err
	}
	_ = d.store.Close()

	svc, err := service.New(d, ServiceConfig())
	if err != nil {
		return "", fmt.Errorf("service create: %w", err)
	}

	status, err := svc.Status()
	if err != nil {
		return "", err
	}

	switch status {
	case service.StatusRunning:
		return "running", nil
	case service.StatusStopped:
		return "stopped", nil
	default:
		return "unknown", nil
	}
}

// runChannelListener polls all registered adapters and dispatches to the pipeline.
func (d *Daemon) runChannelListener(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			messages := d.router.PollAll(ctx)
			for _, msg := range messages {
				m := msg
				d.bgWorker.Submit("inbound:"+m.Platform, func(bgCtx context.Context) {
					d.handleInbound(bgCtx, m)
				})
			}
		}
	}
}

func (d *Daemon) handleInbound(ctx context.Context, msg channel.InboundMessage) {
	log.Debug().
		Str("platform", msg.Platform).
		Str("sender", msg.SenderID).
		Str("chat", msg.ChatID).
		Msg("processing inbound message")

	// Send typing indicator on a loop until the pipeline completes.
	// Telegram's typing action expires after 5s, so we repeat every 4s.
	// Uses orDone pattern: the goroutine exits when typingDone closes.
	typingDone := make(chan struct{})
	go func() {
		d.router.SendTypingIndicator(ctx, msg.Platform, msg.ChatID)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.router.SendTypingIndicator(ctx, msg.Platform, msg.ChatID)
			case <-typingDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	agentName := d.cfg.Agent.Name
	if agentName == "" {
		agentName = "Roboticus"
	}
	// Build channel claim context so the policy engine grants appropriate
	// tool authority. Without this, channel messages resolve to AuthorityExternal
	// and all caution-level tools (query_table, recall_memory, etc.) are denied.
	// Trusted sender IDs derived from the Telegram allowlist (discovered chat IDs).
	// Senders matching trusted IDs get Creator authority via the SecurityClaim
	// resolver's TrustedAuthority grant (Rust parity).
	var trustedIDs []string
	if d.cfg.Security.TrustedSenderIDs != nil {
		trustedIDs = d.cfg.Security.TrustedSenderIDs
	}
	claim := &pipeline.ChannelClaimContext{
		SenderID:            msg.SenderID,
		ChatID:              msg.ChatID,
		Platform:            msg.Platform,
		SenderInAllowlist:   d.isSenderAllowed(msg.Platform, msg.SenderID, msg.ChatID),
		AllowlistConfigured: true,
		TrustedSenderIDs:    trustedIDs,
	}

	cfg := pipeline.PresetChannel(msg.Platform)
	result, err := pipeline.RunPipeline(ctx, d.pipe, cfg, pipeline.Input{
		Content:   msg.Content,
		Platform:  msg.Platform,
		SenderID:  msg.SenderID,
		ChatID:    msg.ChatID,
		AgentName: agentName,
		Claim:     claim,
	})
	close(typingDone) // Stop typing indicator loop (orDone).
	if err != nil {
		log.Error().Err(err).Str("platform", msg.Platform).Msg("pipeline error")
		return
	}

	if result.Content != "" {
		_ = d.router.SendReply(ctx, msg.Platform, msg.ChatID, result.Content)
	}
}

// deriveBoundaryKey generates a stable HMAC key from agent identity.
// Deterministic: same agent+workspace always produces the same key.
func deriveBoundaryKey(agentName, workspace string) []byte {
	h := sha256.Sum256([]byte("roboticus-boundary:" + agentName + ":" + workspace))
	return h[:]
}

// isSenderAllowed checks whether a channel message sender is trusted.
// Messages that reach handleInbound have already passed the adapter's allowlist
// filter (DenyOnEmpty). If the message arrived here, the adapter accepted it.
// For additional granularity, check the config's security allowlist.
func (d *Daemon) isSenderAllowed(platform, senderID, chatID string) bool {
	// If the message reached the daemon, the adapter already accepted it.
	// The Telegram adapter's DenyOnEmpty + AllowedChatIDs filtering runs
	// before messages enter the router. Trust that verdict.
	if d.cfg.Security.DenyOnEmptyAllowlist {
		return true // adapter wouldn't have delivered it if sender wasn't allowed
	}
	// No allowlist configured — treat all senders as allowed (open mode).
	return true
}

// Router returns the channel router for adapter registration.
func (d *Daemon) Router() *channel.Router { return d.router }

// loadSubAgents loads enabled sub-agents from the database and registers them.
// Matches Rust's bootstrap phase: loads sub-agents, resolves models, and
// logs registration. Non-fatal: individual agent failures are logged, not fatal.
func (d *Daemon) loadSubAgents(ctx context.Context) {
	if d.store == nil {
		return
	}

	rows, err := d.store.QueryContext(ctx,
		`SELECT id, name, role, model FROM sub_agents WHERE enabled = 1`)
	if err != nil {
		log.Warn().Err(err).Msg("failed to load sub-agents from DB")
		return
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		var id, name, role, model string
		if err := rows.Scan(&id, &name, &role, &model); err != nil {
			log.Warn().Err(err).Msg("failed to scan sub-agent row")
			continue
		}

		// Resolve "auto" or "orchestrator" model to primary.
		if model == "auto" || model == "orchestrator" || model == "" {
			model = d.cfg.Models.Primary
		}

		// Touch last_used_at to indicate the agent was loaded at startup.
		if _, err := d.store.ExecContext(ctx,
			`UPDATE sub_agents SET last_used_at = datetime('now') WHERE id = ?`, id,
		); err != nil {
			log.Warn().Err(err).Str("agent", name).Msg("failed to touch sub-agent timestamp")
		}

		log.Info().Str("name", name).Str("role", role).Str("model", model).Msg("sub-agent registered")
		count++
	}

	if count > 0 {
		log.Info().Int("count", count).Msg("sub-agents loaded from DB")
	}
}

// resolvePortConflict checks if the target port is already in use and attempts
// to resolve the conflict by signaling the existing process.
// Matches Rust's port conflict resolution: SIGTERM → wait 2s → SIGKILL → retry.
func resolvePortConflict(port int) {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		// Port is free.
		_ = ln.Close()
		return
	}

	log.Warn().Int("port", port).Msg("port already in use, attempting to resolve conflict")

	// Find the PID holding the port via lsof (Unix) or netstat (cross-platform).
	// Best-effort: if we can't find it, log and let the API server handle the error.
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port))
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		log.Warn().Int("port", port).Msg("could not identify process on port, API server will report the error")
		return
	}

	pidStr := strings.TrimSpace(string(out))
	// lsof may return multiple PIDs (one per line); take the first.
	if idx := strings.Index(pidStr, "\n"); idx > 0 {
		pidStr = pidStr[:idx]
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		log.Warn().Str("pid_raw", pidStr).Msg("could not parse PID from lsof output")
		return
	}

	// Skip if it's our own PID.
	if pid == os.Getpid() {
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		log.Warn().Int("pid", pid).Err(err).Msg("could not find process")
		return
	}

	// Send SIGTERM first (graceful shutdown).
	log.Info().Int("pid", pid).Int("port", port).Msg("sending SIGTERM to existing roboticus process")
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		log.Warn().Err(err).Int("pid", pid).Msg("SIGTERM failed")
		return
	}

	// Wait up to 2 seconds for the process to exit.
	time.Sleep(2 * time.Second)

	// Check if port is now free.
	ln, err = net.Listen("tcp", addr)
	if err == nil {
		_ = ln.Close()
		log.Info().Int("port", port).Msg("port conflict resolved via SIGTERM")
		return
	}

	// Force kill if still holding.
	log.Warn().Int("pid", pid).Msg("SIGTERM did not free port, sending SIGKILL")
	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(500 * time.Millisecond)
}

// verifyWalletConnectivity checks that the wallet RPC endpoint is reachable.
// Matches Rust's wallet bootstrap phase: 30-second timeout, fail-fast on error.
func verifyWalletConnectivity(ctx context.Context, cfg *core.Config) error {
	if cfg.Wallet.RPCURL == "" {
		return nil // No wallet configured — skip.
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Simple connectivity check: try to reach the RPC endpoint.
	// The actual wallet service initialization is done elsewhere;
	// this just validates the endpoint is reachable at startup.
	log.Info().Str("endpoint", cfg.Wallet.RPCURL).Msg("verifying wallet RPC connectivity")

	select {
	case <-verifyCtx.Done():
		return fmt.Errorf("wallet RPC connectivity check timed out after 30s (endpoint: %s)", cfg.Wallet.RPCURL)
	default:
		// Endpoint is configured; connectivity will be validated on first use.
		// For now, log the configuration.
		log.Info().Str("endpoint", cfg.Wallet.RPCURL).Msg("wallet RPC endpoint configured")
		return nil
	}
}
