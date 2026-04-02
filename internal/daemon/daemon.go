package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"
	"github.com/rs/zerolog/log"

	"goboticus/internal/agent"
	"goboticus/internal/api"
	"goboticus/internal/api/routes"
	"goboticus/internal/channel"
	"goboticus/internal/core"
	"goboticus/internal/db"
	"goboticus/internal/llm"
	"goboticus/internal/mcp"
	"goboticus/internal/pipeline"
	"goboticus/internal/schedule"
)

// ---------------------------------------------------------------------------
// Adapter types: bridge concrete agent types to pipeline interfaces.
// These are private wiring glue — not reusable outside the composition root.
// ---------------------------------------------------------------------------

// pipelineToAgentSession converts a pipeline.Session to an agent.Session,
// copying identity fields and replaying the message history.
func pipelineToAgentSession(ps *pipeline.Session) *agent.Session {
	as := agent.NewSession(ps.ID, ps.AgentID, ps.AgentName)
	as.Authority = ps.Authority
	as.Workspace = ps.Workspace
	as.AllowedPaths = ps.AllowedPaths
	as.Channel = ps.Channel

	for _, m := range ps.Messages() {
		switch m.Role {
		case "user":
			as.AddUserMessage(m.Content)
		case "assistant":
			as.AddAssistantMessage(m.Content, m.ToolCalls)
		case "system":
			as.AddSystemMessage(m.Content)
		case "tool":
			as.AddToolResult(m.ToolCallID, m.Name, m.Content, false)
		}
	}
	return as
}

// syncAgentToPipeline copies new messages from agent session back to pipeline session.
func syncAgentToPipeline(as *agent.Session, ps *pipeline.Session) {
	existingCount := ps.MessageCount()
	agentMsgs := as.Messages()
	for i := existingCount; i < len(agentMsgs); i++ {
		m := agentMsgs[i]
		switch m.Role {
		case "user":
			ps.AddUserMessage(m.Content)
		case "assistant":
			ps.AddAssistantMessage(m.Content, m.ToolCalls)
		case "system":
			ps.AddSystemMessage(m.Content)
		case "tool":
			ps.AddToolResult(m.ToolCallID, m.Name, m.Content, false)
		}
	}
}

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

// retrieverAdapter wraps *agent.MemoryRetriever → pipeline.MemoryRetriever.
type retrieverAdapter struct {
	r *agent.MemoryRetriever
}

func (a *retrieverAdapter) Retrieve(ctx context.Context, sessionID, query string, budget int) string {
	return a.r.Retrieve(ctx, sessionID, query, budget)
}

// ingestorAdapter wraps *agent.MemoryManager → pipeline.Ingestor.
type ingestorAdapter struct {
	m *agent.MemoryManager
}

func (a *ingestorAdapter) IngestTurn(ctx context.Context, session *pipeline.Session) {
	as := pipelineToAgentSession(session)
	a.m.IngestTurn(ctx, as)
}

// executorAdapter wraps the full agent loop deps → pipeline.ToolExecutor.
type executorAdapter struct {
	llmSvc    *llm.Service
	tools     *agent.ToolRegistry
	policy    *agent.PolicyEngine
	injection *agent.InjectionDetector
	memMgr    *agent.MemoryManager
	retriever *agent.MemoryRetriever
}

func (a *executorAdapter) RunLoop(ctx context.Context, session *pipeline.Session) (string, int, error) {
	as := pipelineToAgentSession(session)

	// Build context: system prompt + memory retrieval.
	ctxBuilder := agent.NewContextBuilder(agent.DefaultContextConfig())

	prompt := agent.BuildSystemPrompt(agent.PromptConfig{
		AgentName: as.AgentName,
	})
	ctxBuilder.SetSystemPrompt(prompt)

	// Set tool definitions.
	if a.tools != nil {
		ctxBuilder.SetTools(a.tools.ToolDefs())
	}

	// Run retrieval if available.
	if a.retriever != nil {
		lastUserMsg := ""
		msgs := as.Messages()
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				lastUserMsg = msgs[i].Content
				break
			}
		}
		if lastUserMsg != "" {
			mem := a.retriever.Retrieve(ctx, as.ID, lastUserMsg, 2048)
			if mem != "" {
				ctxBuilder.SetMemory(mem)
			}
		}
	}

	// Create loop and run.
	loop := agent.NewLoop(agent.DefaultLoopConfig(), agent.LoopDeps{
		LLM:       a.llmSvc,
		Tools:     a.tools,
		Policy:    a.policy,
		Injection: a.injection,
		Memory:    a.memMgr,
		Context:   ctxBuilder,
	})

	content, err := loop.Run(ctx, as)
	turns := loop.TurnCount()

	// Sync new messages back to the pipeline session.
	syncAgentToPipeline(as, session)

	return content, turns, err
}

// nicknameAdapter wraps *llm.Service + *db.Store → pipeline.NicknameRefiner.
type nicknameAdapter struct {
	llm   *llm.Service
	store *db.Store
}

func (a *nicknameAdapter) Refine(ctx context.Context, session *pipeline.Session) {
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

// streamAdapter wraps agent context builder deps → pipeline.StreamPreparer.
type streamAdapter struct {
	llmSvc    *llm.Service
	tools     *agent.ToolRegistry
	retriever *agent.MemoryRetriever
}

func (a *streamAdapter) PrepareStream(ctx context.Context, session *pipeline.Session) (*llm.Request, error) {
	as := pipelineToAgentSession(session)

	ctxBuilder := agent.NewContextBuilder(agent.DefaultContextConfig())

	prompt := agent.BuildSystemPrompt(agent.PromptConfig{
		AgentName: as.AgentName,
	})
	ctxBuilder.SetSystemPrompt(prompt)

	if a.tools != nil {
		ctxBuilder.SetTools(a.tools.ToolDefs())
	}

	// Run retrieval if available.
	if a.retriever != nil {
		lastUserMsg := ""
		msgs := as.Messages()
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				lastUserMsg = msgs[i].Content
				break
			}
		}
		if lastUserMsg != "" {
			mem := a.retriever.Retrieve(ctx, as.ID, lastUserMsg, 2048)
			if mem != "" {
				ctxBuilder.SetMemory(mem)
			}
		}
	}

	req := ctxBuilder.BuildRequest(as)
	req.Stream = true
	return req, nil
}

// Daemon manages the lifecycle of all goboticus subsystems.
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
		Name:        "goboticus",
		DisplayName: "Goboticus Agent Runtime",
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
	policyEngine := agent.NewPolicyEngine(agent.PolicyConfig{
		MaxTransferCents:   int64(cfg.Treasury.PerPaymentCap * 100),
		RateLimitPerMinute: 30,
	})
	memMgr := agent.NewMemoryManager(agent.MemoryConfig{
		TotalTokenBudget: 2048,
		Budgets: agent.MemoryTierBudget{
			Working:      cfg.Memory.WorkingBudget / 100.0,
			Episodic:     cfg.Memory.EpisodicBudget / 100.0,
			Semantic:     cfg.Memory.SemanticBudget / 100.0,
			Procedural:   cfg.Memory.ProceduralBudget / 100.0,
			Relationship: cfg.Memory.RelationshipBudget / 100.0,
		},
	}, store)
	retriever := agent.NewMemoryRetriever(agent.DefaultRetrievalConfig(), agent.MemoryTierBudget{
		Working:      cfg.Memory.WorkingBudget / 100.0,
		Episodic:     cfg.Memory.EpisodicBudget / 100.0,
		Semantic:     cfg.Memory.SemanticBudget / 100.0,
		Procedural:   cfg.Memory.ProceduralBudget / 100.0,
		Relationship: cfg.Memory.RelationshipBudget / 100.0,
	}, store)
	guards := pipeline.DefaultGuardChain()

	dq := channel.NewDeliveryQueue(store)
	router := channel.NewRouter(dq)

	pipe := pipeline.New(pipeline.PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: &injectionAdapter{det: injection},
		Retriever: &retrieverAdapter{r: retriever},
		Skills:    nil, // skill matching wired later
		Executor: &executorAdapter{
			llmSvc:    llmSvc,
			tools:     tools,
			policy:    policyEngine,
			injection: injection,
			memMgr:    memMgr,
			retriever: retriever,
		},
		Ingestor: &ingestorAdapter{m: memMgr},
		Refiner:  &nicknameAdapter{llm: llmSvc, store: store},
		Streamer: &streamAdapter{
			llmSvc:    llmSvc,
			tools:     tools,
			retriever: retriever,
		},
		Guards:   guards,
		BGWorker: bgWorker,
	})

	// Sync hippocampus schema registry.
	hippo := db.NewHippocampusRegistry(store)
	if err := hippo.SyncBuiltinTables(context.Background()); err != nil {
		log.Warn().Err(err).Msg("hippocampus sync failed")
	}

	approvalMgr := agent.NewApprovalManager(agent.ApprovalsConfig{
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
	log.Info().Str("platform", service.Platform()).Msg("goboticus starting")
	go d.run()
	return nil
}

// Stop implements service.Interface. Called by the OS service manager on shutdown.
func (d *Daemon) Stop(s service.Service) error {
	log.Info().Msg("goboticus stopping")
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
				log.Info().Str("job", job.Name).Msg("cron job executing")
				return nil
			}))
		worker.Run(ctx)
	}()

	log.Info().Msg("all subsystems started")
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

// Install registers goboticus as an OS service.
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

// Uninstall removes goboticus from the OS service manager.
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
				go d.handleInbound(ctx, msg)
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
