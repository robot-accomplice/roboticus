package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/kardianos/service"
	"github.com/rs/zerolog/log"

	"goboticus/internal/agent"
	"goboticus/internal/api"
	"goboticus/internal/channel"
	"goboticus/internal/core"
	"goboticus/internal/db"
	"goboticus/internal/llm"
	"goboticus/internal/pipeline"
	"goboticus/internal/schedule"
)

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
	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: providers,
		Primary:   cfg.Models.Primary,
		Fallbacks: cfg.Models.Fallback,
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
		Injection: injection,
		Tools:     tools,
		Policy:    policyEngine,
		Memory:    memMgr,
		Retriever: retriever,
		Guards:    guards,
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

	appState := &api.AppState{
		Store:     store,
		Pipeline:  pipe,
		LLM:       llmSvc,
		Config:    cfg,
		EventBus:  eventBus,
		Approvals: approvalMgr,
	}

	return &Daemon{
		cfg:      cfg,
		store:    store,
		llm:      llmSvc,
		pipe:     pipe,
		router:   router,
		appState: appState,
		eventBus: eventBus,
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
