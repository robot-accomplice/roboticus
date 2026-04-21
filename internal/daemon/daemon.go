package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"roboticus/internal/plugin"
	"roboticus/internal/update"
)

// Daemon manages the lifecycle of all roboticus subsystems.
// Implements kardianos/service.Interface for cross-platform service management
// (systemd on Linux, launchd on macOS, SCM on Windows).
type Daemon struct {
	cfg         *core.Config
	store       *db.Store
	llm         *llm.Service
	pipe        *pipeline.Pipeline
	router      *channel.Router
	appState    *api.AppState
	eventBus    *api.EventBus
	bgWorker    *core.BackgroundWorker
	errBus      *core.ErrorBus
	embedClient *llm.EmbeddingClient
	memMgr      *memory.Manager

	startupStart time.Time
	errBusCancel context.CancelFunc
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	// pidFilePath is the resolved location for the v1.0.6 PID file.
	// Set in New() from cfg.Daemon.PIDFile (or the default path
	// returned by PIDFilePath). Daemon.Start writes os.Getpid() here
	// after the HTTP server is ready; Daemon.Stop removes it as the
	// last cleanup step. The PID file is what `roboticus daemon
	// stop` reads to find a running daemon without re-booting the
	// 12-step subsystem stack.
	pidFilePath string
}

// ServiceConfig returns the baseline kardianos service identity — Name,
// DisplayName, Description. This is the lookup-only config used by
// Uninstall / Control / Status, which don't need to re-specify how the
// service starts (the service manager already has the registered args
// from Install). For Install itself, see ServiceInstallConfig.
func ServiceConfig() *service.Config {
	return &service.Config{
		Name:        "roboticus",
		DisplayName: "Roboticus Agent Runtime",
		Description: "Autonomous AI agent runtime with multi-channel support.",
	}
}

// ServiceInstallConfig returns a service config that embeds the operator's
// current invocation context — the exact config file path, any active
// ROBOTICUS_* environment variables, and the operator's PATH at install
// time — so the installed service starts against the same agent runtime
// the operator just validated at install time.
//
// Why this exists: v1.0.6 audit flagged that NewServiceOnly discarded the
// loaded cfg and registered the service with only Name/Display/Description
// set. The service manager then started the binary with no arguments,
// which on macOS/Linux/Windows resolves to the default config location —
// NOT the --config the operator passed to `roboticus daemon install`. The
// operator's install intent got silently dropped, so installed services
// could pick up the wrong database, wrong agent identity, wrong workspace.
//
// Arguments: we embed `serve --config <path>` so the binary's serve path
// loads explicitly against the configured file. An empty configPath is
// accepted (falls back to default lookup) for callers that don't have one
// — but the install command always passes cmdutil.EffectiveConfigPath().
//
// Env: we snapshot three categories of environment at install time:
//  1. All ROBOTICUS_* vars (feature flags, profile, config override).
//  2. PATH — specifically called out because the v1.0.6 self-audit
//     caught that systemd/launchd services inherit a minimal PATH
//     (typically /usr/bin:/bin), NOT the operator's shell PATH. If the
//     operator had /opt/homebrew/bin (for `ollama`), $HOME/.local/bin
//     (pip installs), or a virtualenv bin dir on PATH, subprocess
//     launches from the service (Ollama, Playwright MCP via npx,
//     Python-based MCP servers) would silently fail with "not found."
//     Copying PATH at install freezes the operator's working PATH
//     into the service environment.
//  3. The installer's own HOME — so default-config resolution inside
//     the service targets the operator's home, not the service user's
//     home (on macOS launchd that's often a system account). This is
//     belt-and-suspenders alongside the explicit --config path, for
//     any code path that reads $HOME directly (tilde expansion in
//     user-edited paths, etc.).
//
// We intentionally DON'T copy everything in os.Environ() — dragging the
// operator's shell-local aliases, OAuth tokens, and unrelated env into a
// long-running service would be a leak vector. The three whitelists above
// cover observed footguns; if a new env becomes load-bearing for the
// agent, add it here with a note explaining why.
func ServiceInstallConfig(_ *core.Config, configPath string) *service.Config {
	cfg := ServiceConfig()
	args := []string{"serve"}
	if strings.TrimSpace(configPath) != "" {
		args = append(args, "--config", configPath)
	}
	cfg.Arguments = args

	envVars := map[string]string{}

	// Category 1: ROBOTICUS_* overrides.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ROBOTICUS_") {
			continue
		}
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		envVars[kv[:i]] = kv[i+1:]
	}

	// Category 2: PATH. The service manager's default PATH is too
	// minimal for the MCP/subprocess ecosystem to work. We capture
	// the operator's install-time PATH verbatim.
	if p := os.Getenv("PATH"); p != "" {
		envVars["PATH"] = p
	}

	// Category 3: HOME. Belt-and-suspenders for any code path that
	// expands ~ after the fact.
	if h := os.Getenv("HOME"); h != "" {
		envVars["HOME"] = h
	}

	if len(envVars) > 0 {
		cfg.EnvVars = envVars
	}
	return cfg
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
	daemonReady := false
	defer func() {
		if daemonReady {
			return
		}
		errBusCancel()
		_ = store.Close()
	}()

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
		Policies:  llm.EffectiveModelPolicies(context.Background(), store, cfg.Models.Policy),
		RoleEligibility: func() map[string]llm.RoleEligibility {
			out := make(map[string]llm.RoleEligibility, len(cfg.Models.RoleEligibility))
			for model, eligibility := range cfg.Models.RoleEligibility {
				out[model] = llm.RoleEligibility{
					Orchestrator: eligibility.Orchestrator,
					Subagent:     eligibility.Subagent,
					Reason:       eligibility.Reason,
				}
			}
			return out
		}(),
		BGWorker:      bgWorker,
		ErrBus:        errBus,
		ToolBlocklist: cfg.Models.ToolBlocklist,
		ToolAllowlist: cfg.Models.ToolAllowlist,
	}, store)
	if err != nil {
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
	if cfg.Obsidian.Enabled && strings.TrimSpace(cfg.Obsidian.VaultPath) != "" {
		tools.Register(&agenttools.ObsidianWriteTool{VaultPath: cfg.Obsidian.VaultPath})
	}
	tools.Register(&agenttools.EditFileTool{})
	tools.Register(&agenttools.ListDirectoryTool{})
	tools.Register(&agenttools.SearchFilesTool{})
	tools.Register(&agenttools.GlobFilesTool{})

	// Scheduling.
	tools.Register(&agenttools.CronTool{})

	// Introspection tools.
	tools.Register(&agenttools.RuntimeContextTool{})
	tools.Register(&agenttools.MemoryStatsTool{})
	introspection := agenttools.NewIntrospectionTool(cfg.Agent.Name, "0.1.0", tools.Names)
	tools.Register(introspection)
	tools.Register(agenttools.NewIntrospectionAliasTool("introspection", introspection))

	// Memory tools.
	tools.Register(agenttools.NewMemoryRecallTool(store))
	tools.Register(agenttools.NewMemorySearchTool(store))
	tools.Register(agenttools.NewGraphQueryTool(store))
	tools.Register(agenttools.NewWorkflowSearchTool(store))
	tools.Register(agenttools.NewPolicyIngestTool(store, cfg.Agent.Name))

	// Channel and subagent introspection.
	tools.Register(&agenttools.ChannelHealthTool{})
	tools.Register(&agenttools.SubagentStatusTool{})
	tools.Register(&agenttools.SubagentRosterTool{})
	tools.Register(&agenttools.AvailableSkillsTool{})
	tools.Register(agenttools.NewComposeSkillTool(cfg.Skills.Directory))
	tools.Register(&agenttools.ComposeSubagentTool{})
	tools.Register(&agenttools.OrchestrateSubagentsTool{})
	tools.Register(&agenttools.TaskStatusTool{})
	tools.Register(&agenttools.ListOpenTasksTool{})
	tools.Register(&agenttools.RetryTaskTool{})

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
	policyCfg.WorkspaceOnly = cfg.Security.IsWorkspaceConfined()
	policyCfg.AllowedPaths = cfg.Security.AllowedPaths
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
	retrievalCfg := memory.DefaultRetrievalConfig()
	retrievalCfg.HybridWeight = cfg.Memory.HybridWeightOverride
	retrievalCfg.EpisodicHalfLife = cfg.Memory.DecayHalfLifeDays
	retrievalCfg.Reranker.MinScore = cfg.Memory.RerankerMinScore
	retrievalCfg.Reranker.AuthorityBoost = cfg.Memory.RerankerAuthorityBoost
	retrievalCfg.Reranker.RecencyPenalty = cfg.Memory.RerankerRecencyPenalty
	retrievalCfg.Reranker.CollapseSpread = cfg.Memory.RerankerCollapseSpread
	retrievalCfg.LLMReranker.Enabled = cfg.Memory.LLMRerankerEnabled
	retrievalCfg.LLMReranker.MinCandidates = cfg.Memory.LLMRerankerMinCandidates
	retrievalCfg.LLMReranker.MaxCandidates = cfg.Memory.LLMRerankerMaxCandidates
	retrievalCfg.LLMReranker.KeepTop = cfg.Memory.LLMRerankerKeepTop
	retrievalCfg.LLMReranker.Model = strings.TrimSpace(cfg.Memory.LLMRerankerModel)
	retriever := memory.NewRetriever(retrievalCfg, memory.TierBudget{
		Working:      cfg.Memory.WorkingBudget / 100.0,
		Episodic:     cfg.Memory.EpisodicBudget / 100.0,
		Semantic:     cfg.Memory.SemanticBudget / 100.0,
		Procedural:   cfg.Memory.ProceduralBudget / 100.0,
		Relationship: cfg.Memory.RelationshipBudget / 100.0,
	}, store)
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
	loadedSkills = mergeLoadedSkills(loadedSkills, skillLoader.LoadFromPaths(pipeline.EnabledSkillSourcePathsFromDB(store)))
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
		if cfg.Skills.Directory != "" {
			bootDetail("dir", cfg.Skills.Directory)
		}
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

	// Recover stranded in-flight deliveries from previous unclean shutdown.
	if recovered, err := store.ExecContext(context.Background(),
		`UPDATE delivery_queue SET status = 'pending', next_retry_at = datetime('now', '+30 seconds') WHERE status = 'in_flight'`); err == nil {
		if n, _ := recovered.RowsAffected(); n > 0 {
			log.Warn().Int64("recovered", n).Msg("delivery queue: recovered stranded in-flight messages")
		}
	}

	router := channel.NewRouter(dq)
	var telegramWebhook interface {
		ProcessWebhookBatch(data []byte) ([]channel.InboundMessage, error)
	}
	var whatsAppWebhook interface {
		ProcessWebhookBatch(data []byte) ([]channel.InboundMessage, error)
		VerifyWebhook(mode, token, challenge string) (string, bool)
		ValidateWebhookSignature(body []byte, signature string) bool
	}

	// Register channel adapters from config + keystore.
	// All tokens come from keystore — no env var fallback.
	telegramToken := resolveChannelToken("", "telegram_bot_token", ks)
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
			DenyOnEmpty:    cfg.Security.Filesystem.DenyOnEmptyAllowlist,
		}
		tgAdapter := channel.NewTelegramAdapter(tgCfg)
		telegramWebhook = tgAdapter
		// Clear any stale webhook so getUpdates polling works.
		if err := tgAdapter.DeleteWebhook(context.Background()); err != nil {
			log.Warn().Err(err).Msg("telegram: failed to delete webhook")
		}
		router.Register(tgAdapter)
		log.Info().Msg("telegram adapter registered (polling mode)")
	}
	if cfg.Channels.WhatsApp != nil && cfg.Channels.WhatsApp.Enabled {
		waCfg := channel.WhatsAppConfig{
			Token:          resolveChannelToken("", "whatsapp_api_token", ks),
			PhoneNumberID:  cfg.Channels.WhatsApp.PhoneNumberID,
			VerifyToken:    cfg.Channels.WhatsApp.VerifyToken,
			AppSecret:      cfg.Channels.WhatsApp.AppSecret,
			AllowedNumbers: cfg.Channels.WhatsApp.AllowedNumbers,
			DenyOnEmpty:    cfg.Security.Filesystem.DenyOnEmptyAllowlist,
		}
		whatsAppWebhook = channel.NewWhatsAppAdapter(waCfg)
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
	// Wire embedding client into the memory manager so that newly stored
	// episodic and semantic memories are embedded at ingestion time.
	memMgr.SetEmbeddingClient(embedClient)
	retriever.SetEmbeddingClient(embedClient)
	retriever.SetCompleter(llmSvc)
	// Wire session summary promotion: when a session is archived,
	// promote its top working memory entries to semantic memory.
	mgr := memMgr // capture for closure
	store.OnSessionArchived(func(ctx context.Context, sessionID string) {
		mgr.PromoteSessionSummary(ctx, sessionID)
	})
	bootStep(9, steps, "Embeddings configured")
	if embedProviderName != "" {
		bootDetail("provider", embedProviderName)
	} else {
		bootDetail("provider", "n-gram fallback (local)")
	}

	// ── Phase 10: Pipeline assembly ─────────────────────────────────────
	// Resolve the operator-configured tool-search knobs into the agent-
	// local struct once, so the executor, streamer, and pruner adapters
	// all see the same bindings. Zero-valued fields in core.ToolSearch
	// fall back to the Rust-parity defaults.
	toolSearchCfg := resolveToolSearchConfig(cfg.ToolSearch)

	eventBus := api.NewEventBus(256)
	pipe := pipeline.New(pipeline.PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: &injectionAdapter{det: injection},
		Retriever: &retrieverAdapter{r: retriever},
		Skills:    &skillAdapter{matcher: skillMatcher, tools: tools},
		Executor: &executorAdapter{
			llmSvc:          llmSvc,
			store:           store,
			tools:           tools,
			policy:          policyEngine,
			injection:       injection,
			memMgr:          memMgr,
			embedClient:     embedClient,
			toolSearchCfg:   toolSearchCfg,
			promptConfig:    basePromptCfg,
			budgetCfg:       &cfg.ContextBudget,
			cacheCfg:        &cfg.Cache,
			maxTurnDuration: time.Duration(cfg.Agent.AutonomyMaxTurnDurationSecs) * time.Second,
		},
		Ingestor: &ingestorAdapter{m: memMgr},
		Refiner:  &nicknameAdapter{llm: llmSvc, store: store},
		Streamer: &streamAdapter{
			llmSvc:        llmSvc,
			store:         store,
			tools:         tools,
			embedClient:   embedClient,
			toolSearchCfg: toolSearchCfg,
			promptConfig:  basePromptCfg,
			budgetCfg:     &cfg.ContextBudget,
			cacheCfg:      &cfg.Cache,
		},
		Pruner: &prunerAdapter{
			tools:         tools,
			embedClient:   embedClient,
			toolSearchCfg: toolSearchCfg,
		},
		Capabilities: &capabilitySummaryAdapter{
			store:      store,
			tools:      tools,
			promptBase: basePromptCfg,
		},
		BGWorker:     bgWorker,
		Embeddings:   embedClient,
		ErrBus:       errBus,
		Dashboard:    eventBus,
		Workspace:    cfg.Agent.Workspace,
		AllowedPaths: cfg.Security.AllowedPaths,
		CacheTTL:     time.Duration(cfg.Cache.TTLSeconds) * time.Second,
		CheckpointPolicy: &pipeline.CheckpointPolicy{
			Enabled:       cfg.Context.CheckpointEnabled,
			IntervalTurns: cfg.Context.CheckpointIntervalTurns,
		},
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
	pluginRegistry, err := buildPluginRegistry(cfg)
	if err != nil {
		return nil, err
	}
	if pluginRegistry != nil {
		agenttools.RegisterPluginTools(tools, pluginRegistry)
	}

	appState := &api.AppState{
		Store:           store,
		Pipeline:        pipe,
		StreamFinalizer: pipe, // *Pipeline satisfies both Runner and StreamFinalizer
		LLM:             llmSvc,
		Embeddings:      embedClient,
		Config:          cfg,
		Keystore:        ks,
		EventBus:        eventBus,
		Approvals:       approvalMgr,
		Tools:           tools,
		MCP:             mcpMgr,
		Plugins:         pluginRegistry,
		TelegramWebhook: telegramWebhook,
		WhatsAppWebhook: whatsAppWebhook,
	}

	bootStep(11, steps, "Hippocampus, approvals, events ready")
	log.Info().Msg("[startup 11/12] hippocampus, approvals, events ready")

	// ── Phase 11.5: Working Memory Vet ────────────────────────────────
	// Vet persisted working memory — discard stale/low-value entries,
	// retain goals and active decisions. Like waking up after sleep.
	vetCtx, vetCancel := context.WithTimeout(context.Background(), 5*time.Second)
	vetResult := memMgr.VetWorkingMemory(vetCtx, memory.DefaultVetConfig())
	vetCancel()
	if vetResult.Retained > 0 || vetResult.Discarded > 0 {
		log.Info().Int("retained", vetResult.Retained).Int("discarded", vetResult.Discarded).
			Msg("working memory vetted on startup")
	}

	// v1.0.6 self-audit P1-J: best-effort sweep of stale
	// <roboticus>.old* sidecars left behind by Windows self-updates
	// whose MoveFileExW delete-on-reboot failed (privilege revoked,
	// reboot never happened). Runs once per boot. The 24h minimum
	// age discipline protects in-flight rollback windows. Unix
	// doesn't produce these sidecars so this is a no-op there.
	update.SweepStaleUpdateSidecarsAuto()

	// ── Phase 12: Complete ──────────────────────────────────────────────
	log.Info().Int64("startup_ms", time.Since(startupStart).Milliseconds()).Msg("[startup 12/12] daemon initialization complete")

	daemonReady = true
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
		embedClient:  embedClient,
		memMgr:       memMgr,
		startupStart: startupStart,
		errBusCancel: errBusCancel,
		pidFilePath:  PIDFilePath(cfg),
	}, nil
}

func buildPluginRegistry(cfg *core.Config) (*plugin.Registry, error) {
	reg := plugin.NewRegistry(cfg.Plugins.Allow, cfg.Plugins.Deny, plugin.PermissionPolicy{
		StrictMode:          cfg.Plugins.StrictPermissions,
		AllowedInterpreters: append([]string(nil), cfg.Skills.AllowedInterpreters...),
		MaxOutputBytes:      cfg.Skills.ScriptMaxOutputBytes,
		SandboxEnv:          cfg.Skills.SandboxEnv,
	})

	dir := strings.TrimSpace(cfg.Plugins.Dir)
	if dir == "" {
		return reg, nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, fmt.Errorf("plugin registry: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("plugin registry: %q is not a directory", dir)
	}

	loaded, err := reg.ScanDirectory(dir)
	if err != nil {
		return nil, fmt.Errorf("plugin registry: scan %q: %w", dir, err)
	}
	for _, initErr := range reg.InitAll() {
		log.Warn().Err(initErr).Str("dir", dir).Msg("plugin init failed")
	}
	if loaded > 0 {
		log.Info().Int("count", loaded).Str("dir", dir).Msg("plugin registry loaded")
	}
	return reg, nil
}

// Start implements service.Interface. Called by the OS service manager.
func (d *Daemon) Start(s service.Service) error {
	// Final boot step: HTTP server starting (Rust parity: serve.rs step 12).
	bootStep(12, 12, "HTTP server starting")
	bindAddr := fmt.Sprintf("127.0.0.1:%d", d.cfg.Server.Port)
	bootDetail("bind", bindAddr)
	bootDetail("dashboard", fmt.Sprintf("http://localhost:%d", d.cfg.Server.Port))

	// v1.0.6: surface any system warnings (config-defaults-used,
	// ambient DB creation, etc.) right before "Ready" so the
	// foreground operator sees them at the moment they're most
	// likely to act on them. The warnings are also persisted in
	// the system-warnings collector for the dashboard banner.
	if rawWarnings := core.SystemWarningsSnapshot(); len(rawWarnings) > 0 {
		views := make([]SystemWarningView, len(rawWarnings))
		for i, w := range rawWarnings {
			views[i] = SystemWarningView{Title: w.Title, Detail: w.Detail, Remedy: w.Remedy}
		}
		bootSystemWarningsBanner(views)
	}

	bootReady(time.Since(d.startupStart))

	// v1.0.6: write the PID file so `roboticus daemon stop` can find
	// us via SIGTERM without reaching for launchctl/systemctl. Failure
	// to write is logged but does not block startup — the daemon can
	// still serve requests, the operator just won't be able to use
	// the PID-file path of `daemon stop` (they can still kill -9 or
	// use the OS service manager). Surfacing the failure rather than
	// burying it is what makes operator triage tractable.
	if err := WritePIDFile(d.pidFilePath); err != nil {
		log.Warn().
			Err(err).
			Str("path", d.pidFilePath).
			Msg("could not write pid file; `roboticus daemon stop` will fall back to OS service manager (which may not work for foreground/serve invocations)")
	} else {
		log.Info().Str("path", d.pidFilePath).Int("pid", os.Getpid()).Msg("pid file written")
	}

	log.Info().Str("agent", d.cfg.Agent.Name).Str("platform", service.Platform()).Int("port", d.cfg.Server.Port).Msg("roboticus starting")
	go d.run()
	return nil
}

// Stop implements service.Interface. Called by the OS service manager on shutdown.
func (d *Daemon) Stop(s service.Service) error {
	log.Info().Msg("roboticus stopping")

	// Persist working memory before shutdown — like going to sleep.
	// Must happen before context cancellation.
	if d.memMgr != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		d.memMgr.PersistWorkingMemory(shutdownCtx)
		cancel()
	}

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

	// v1.0.6: remove PID file as the FINAL cleanup step so `roboticus
	// daemon stop` (which polls liveness via the PID file) sees a
	// clean state immediately after we exit. RemovePIDFile is
	// idempotent — missing-file is not an error — so a kill -9 that
	// bypassed Stop entirely won't cause the next graceful shutdown
	// to fail at this step.
	if d.pidFilePath != "" {
		if err := RemovePIDFile(d.pidFilePath); err != nil {
			log.Warn().Err(err).Str("path", d.pidFilePath).Msg("failed to remove pid file on shutdown; next start may report stale-pid warning")
		}
	}
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

// controlStub is a minimal implementation of service.Interface used by
// the lifecycle verbs (Install, Uninstall, Control, Status) that need to
// construct a *service.Service handle without actually running the
// daemon. The kardianos library only invokes Start/Stop on the
// Interface when the binary is itself running AS the service (i.e.,
// inside `svc.Run()`), so a stub satisfying the type is sufficient for
// all external control operations.
//
// The pre-v1.0.6 path called daemon.New() (full 12-step boot) just to
// hand the resulting *Daemon to service.New(). That had two ugly
// consequences:
//  1. Every `roboticus daemon stop` printed a 12-step startup
//     sequence to the user before issuing a stop, which is the
//     opposite of operationally clear.
//  2. Under sudo, the boot opened ~/.roboticus/state.db (and other
//     files) as root, leaving them root-owned and locking subsequent
//     unprivileged invocations out.
//
// The stub eliminates both: zero subsystems initialized, zero files
// touched, zero permission side effects.
type controlStub struct{}

// Start / Stop on the stub are unreachable in practice — they would
// only fire if someone called svc.Run() on a stub-backed service,
// which the lifecycle verbs never do. Implemented as no-ops to satisfy
// the interface.
func (controlStub) Start(s service.Service) error { return nil }
func (controlStub) Stop(s service.Service) error  { return nil }

// NewServiceOnly returns a *service.Service constructed from a
// minimal stub Interface — no daemon boot, no DB open, no LLM init,
// no goroutines spawned. Used by Install / Uninstall / Control /
// Status to issue OS-service commands without paying the full
// daemon-boot cost (and without creating root-owned files in
// ~/.roboticus when run under sudo).
//
// Returns the service handle plus the *service.Config used to build
// it so callers (specifically Control on macOS) can re-derive the
// service name for direct launchctl invocations when the kardianos
// stop path returns its uninformative legacy error.
func NewServiceOnly(_ *core.Config) (service.Service, *service.Config, error) {
	cfg := ServiceConfig()
	svc, err := service.New(controlStub{}, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("service create: %w", err)
	}
	return svc, cfg, nil
}

// Install registers roboticus as an OS service with the invocation
// context embedded (config path + ROBOTICUS_* env). The service manager
// will start the binary as `roboticus serve --config <configPath>` with
// the captured env so the installed runtime honors the operator's intent
// instead of silently falling back to default config lookup.
//
// configPath should be the absolute path the operator used at install
// time — the install command layer obtains it via
// cmdutil.EffectiveConfigPath(). Passing an empty string is accepted (no
// --config arg embedded) but not recommended.
func Install(cfg *core.Config, configPath string) error {
	installCfg := ServiceInstallConfig(cfg, configPath)
	svc, err := service.New(controlStub{}, installCfg)
	if err != nil {
		return fmt.Errorf("service create: %w", err)
	}
	return svc.Install()
}

// Uninstall removes roboticus from the OS service manager.
func Uninstall(cfg *core.Config) error {
	svc, _, err := NewServiceOnly(cfg)
	if err != nil {
		return err
	}
	return svc.Uninstall()
}

// Control dispatches start/stop/restart to the running daemon.
//
// Resolution order:
//
//  1. PID file path (DaemonConfig.PIDFile or default
//     ~/.roboticus/roboticus.pid). If the file exists and points at
//     a live process, the action is satisfied via Unix signals — no
//     sudo needed, no launchctl involved. This is the path that
//     handles `roboticus serve` foreground invocations and
//     user-mode daemons.
//
//  2. Fall back to the OS service manager (launchctl on macOS,
//     systemctl on Linux, SCM on Windows). This is the path for
//     installed system services where the daemon was bootstrapped
//     by the OS, not by `roboticus serve`.
//
// Idempotent semantics:
//   - "stop" on an already-stopped daemon → returns nil with a
//     friendly "not running" log. Operators running `roboticus
//     daemon stop` to verify clean state get exit code 0, not an
//     error.
//   - "start" on an already-running daemon → returns nil with a
//     friendly "already running" log.
func Control(cfg *core.Config, action string) error {
	switch action {
	case "stop":
		return controlStop(cfg)
	case "start":
		return controlStart(cfg)
	case "restart":
		if err := controlStop(cfg); err != nil {
			return fmt.Errorf("restart: stop phase: %w", err)
		}
		return controlStart(cfg)
	default:
		// Unknown verbs go to the OS service manager unchanged so
		// kardianos-supported actions (e.g. "pause" on Windows) keep
		// working without a Roboticus-side switch entry.
		svc, _, err := NewServiceOnly(cfg)
		if err != nil {
			return err
		}
		return service.Control(svc, action)
	}
}

// Status returns the current service status. Resolves the same way as
// Control: PID file first (covers foreground `roboticus serve` and
// user-mode daemons), OS service manager as fallback (covers system-
// installed services).
func Status(cfg *core.Config) (string, error) {
	pidPath := PIDFilePath(cfg)
	if pid, found, err := ReadPIDFile(pidPath); err == nil && found {
		if ProcessIsAlive(pid) {
			return "running", nil
		}
		// PID file exists but process is dead — stale file. Report
		// stopped so callers see clean state; the next `daemon stop`
		// or `serve` will clean up the stale file.
		return "stopped", nil
	}

	svc, _, err := NewServiceOnly(cfg)
	if err != nil {
		return "", err
	}
	status, err := svc.Status()
	if err != nil {
		// kardianos returns errors for "service not installed" — treat
		// that as "stopped" rather than propagating the error, since
		// from the operator's perspective an uninstalled service is
		// indistinguishable from a stopped one for `daemon status`.
		if errors.Is(err, service.ErrNotInstalled) {
			return "stopped", nil
		}
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

func mergeLoadedSkills(base, extra []*skills.Skill) []*skills.Skill {
	if len(extra) == 0 {
		return base
	}
	merged := append([]*skills.Skill{}, base...)
	seen := make(map[string]struct{}, len(merged))
	for _, skill := range merged {
		if skill == nil {
			continue
		}
		key := strings.TrimSpace(skill.SourcePath)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(skill.Name()))
		}
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, skill := range extra {
		if skill == nil {
			continue
		}
		key := strings.TrimSpace(skill.SourcePath)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(skill.Name()))
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, skill)
	}
	return merged
}
