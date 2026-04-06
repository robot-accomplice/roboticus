package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"
	"github.com/rs/zerolog/log"

	"roboticus/internal/agent"
	"roboticus/internal/agent/memory"
	"roboticus/internal/agent/policy"
	"roboticus/internal/agent/skills"
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
func buildAgentContext(ctx context.Context, sess *session.Session, tools *agent.ToolRegistry, retriever *memory.Retriever, promptCfg agent.PromptConfig) *agent.ContextBuilder {
	ctxBuilder := agent.NewContextBuilder(agent.DefaultContextConfig())

	cfg := promptCfg
	cfg.AgentName = sess.AgentName
	ctxBuilder.SetSystemPrompt(agent.BuildSystemPrompt(cfg))

	if tools != nil {
		ctxBuilder.SetTools(tools.ToolDefs())
	}

	if retriever != nil {
		msgs := sess.Messages()
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				mem := retriever.Retrieve(ctx, sess.ID, msgs[i].Content, 2048)
				if mem != "" {
					ctxBuilder.SetMemory(mem)
				}
				break
			}
		}
	}

	return ctxBuilder
}

// executorAdapter wraps the full agent loop deps → pipeline.ToolExecutor.
type executorAdapter struct {
	llmSvc       *llm.Service
	tools        *agent.ToolRegistry
	policy       *policy.Engine
	injection    *agent.InjectionDetector
	memMgr       *memory.Manager
	retriever    *memory.Retriever
	promptConfig agent.PromptConfig
}

func (a *executorAdapter) RunLoop(ctx context.Context, sess *session.Session) (string, int, error) {
	ctxBuilder := buildAgentContext(ctx, sess, a.tools, a.retriever, a.promptConfig)

	loop := agent.NewLoop(agent.DefaultLoopConfig(), agent.LoopDeps{
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
		log.Debug().Err(err).Msg("nickname refinement LLM call failed")
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
		log.Debug().Err(err).Str("session", session.ID).Msg("failed to update session nickname")
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
	promptConfig agent.PromptConfig
}

func (a *streamAdapter) PrepareStream(ctx context.Context, sess *session.Session) (*llm.Request, error) {
	ctxBuilder := buildAgentContext(ctx, sess, a.tools, a.retriever, a.promptConfig)
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

	cancel context.CancelFunc
	wg     sync.WaitGroup
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
func New(cfg *core.Config) (*Daemon, error) {
	store, err := db.Open(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("daemon: open database: %w", err)
	}

	// Build LLM service config from validated core.Config.
	var providers []llm.Provider
	for name, pc := range cfg.Providers {
		providers = append(providers, llm.Provider{
			Name:             name,
			URL:              pc.URL,
			Format:           llm.APIFormat(pc.Format),
			APIKeyEnv:        pc.APIKeyEnv,
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
		})
	}
	bgWorker := core.NewBackgroundWorker(32)

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: providers,
		Primary:   cfg.Models.Primary,
		Fallbacks: cfg.Models.Fallback,
		BGWorker:  bgWorker,
	}, store)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("daemon: init LLM: %w", err)
	}

	injection := agent.NewInjectionDetector()
	tools := agent.NewToolRegistry()
	policyEngine := policy.NewEngine(policy.Config{
		MaxTransferCents:   int64(cfg.Treasury.PerPaymentCap * 100),
		RateLimitPerMinute: 30,
	})
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

	// Build shared prompt config with personality and workspace context.
	var skillNames []string
	for _, s := range loadedSkills {
		skillNames = append(skillNames, s.Name())
	}
	basePromptCfg := agent.PromptConfig{
		AgentName:   cfg.Agent.Name,
		Firmware:    core.FormatFirmwareRules(fwCfg),
		Personality: core.FormatOsPersonality(osCfg),
		Workspace:   cfg.Agent.Workspace,
		Skills:      skillNames,
		Model:       cfg.Models.Primary,
	}
	log.Info().
		Str("agent", cfg.Agent.Name).
		Bool("has_firmware", basePromptCfg.Firmware != "").
		Bool("has_personality", basePromptCfg.Personality != "").
		Msg("personality loaded")

	dq := channel.NewDeliveryQueue(store)
	router := channel.NewRouter(dq)

	pipe := pipeline.New(pipeline.PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: &injectionAdapter{det: injection},
		Retriever: &retrieverAdapter{r: retriever},
		Skills:    &skillAdapter{matcher: skillMatcher, tools: tools},
		Executor: &executorAdapter{
			llmSvc:       llmSvc,
			tools:        tools,
			policy:       policyEngine,
			injection:    injection,
			memMgr:       memMgr,
			retriever:    retriever,
			promptConfig: basePromptCfg,
		},
		Ingestor: &ingestorAdapter{m: memMgr},
		Refiner:  &nicknameAdapter{llm: llmSvc, store: store},
		Streamer: &streamAdapter{
			llmSvc:       llmSvc,
			tools:        tools,
			retriever:    retriever,
			promptConfig: basePromptCfg,
		},
		Guards:   guards,
		BGWorker: bgWorker,
	})

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
		Store:     store,
		Pipeline:  pipe,
		LLM:       llmSvc,
		Config:    cfg,
		EventBus:  eventBus,
		Approvals: approvalMgr,
		MCP:       mcpMgr,
	}

	return &Daemon{
		cfg:      cfg,
		store:    store,
		llm:      llmSvc,
		pipe:     pipe,
		router:   router,
		appState: appState,
		eventBus: eventBus,
		bgWorker: bgWorker,
	}, nil
}

// Start implements service.Interface. Called by the OS service manager.
func (d *Daemon) Start(s service.Service) error {
	log.Info().Str("platform", service.Platform()).Msg("roboticus starting")
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

	_ = d.store.Close()
	return nil
}

// run starts all subsystems. Called from Start() in a goroutine.
func (d *Daemon) run() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	// API server.
	srvCfg := api.DefaultServerConfig()
	if d.cfg.Server.Port > 0 {
		srvCfg.Port = d.cfg.Server.Port
	}
	if d.cfg.Server.Bind != "" {
		srvCfg.Bind = d.cfg.Server.Bind
	}
	httpSrv := api.NewServer(srvCfg, d.appState)

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
			}))
		worker.Run(ctx)
	}()

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

	log.Info().Msg("all subsystems started")
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
	d, err := New(cfg)
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
	d, err := New(cfg)
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
	d, err := New(cfg)
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
	d, err := New(cfg)
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

	cfg := pipeline.PresetChannel(msg.Platform)
	result, err := pipeline.RunPipeline(ctx, d.pipe, cfg, pipeline.Input{
		Content:  msg.Content,
		Platform: msg.Platform,
		SenderID: msg.SenderID,
		ChatID:   msg.ChatID,
	})
	if err != nil {
		log.Error().Err(err).Str("platform", msg.Platform).Msg("pipeline error")
		return
	}

	if result.Content != "" {
		_ = d.router.SendReply(ctx, msg.Platform, msg.ChatID, result.Content)
	}
}

// Router returns the channel router for adapter registration.
func (d *Daemon) Router() *channel.Router { return d.router }
