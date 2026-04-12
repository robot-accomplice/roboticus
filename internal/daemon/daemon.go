package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
)

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
	tools.Register(agenttools.NewMemorySearchTool(store))

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
		Tools:           tools,
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

// Router returns the channel router for adapter registration.
func (d *Daemon) Router() *channel.Router { return d.router }
