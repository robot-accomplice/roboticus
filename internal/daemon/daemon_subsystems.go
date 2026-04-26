package daemon

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/api"
	"roboticus/internal/channel"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/mcp"
	"roboticus/internal/pipeline"
	"roboticus/internal/schedule"
)

// run starts all subsystems. Called from Start() in a goroutine.
func (d *Daemon) run() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	// ── Startup Phase: MCP server connections (Rust parity) ──────────────
	// Connect to all configured MCP servers with a 30-second timeout.
	// Non-fatal: individual server failures are logged, not fatal.
	if len(d.cfg.MCP.Servers) > 0 {
		mcpCtx, mcpCancel := context.WithTimeout(ctx, 30*time.Second)
		mcpServers := mcp.ConfigsFromCoreEntries(d.cfg.MCP.Servers)
		connected := d.appState.MCP.ConnectAll(mcpCtx, mcpServers)
		mcpCancel()
		log.Info().Int("connected", connected).Int("configured", len(d.cfg.MCP.Servers)).Msg("MCP server connections established")

		// Register MCP-discovered tools in the agent's ToolRegistry so they
		// appear alongside builtins during inference.
		if d.appState.Tools != nil && connected > 0 {
			mcpTools := agenttools.RegisterMCPTools(d.appState.Tools, d.appState.MCP)
			log.Info().Int("mcp_tools", mcpTools).Msg("MCP tools registered in agent tool registry")
		}
	}

	// Tool embeddings for semantic tool search (Rust parity:
	// roboticus-agent/src/tool_search.rs). Runs AFTER all tools are
	// registered (builtins + MCP bridges) so the embedding pass
	// covers the full set. One batch call; failures are non-fatal
	// (ranker falls back to always_include-only selection).
	if d.appState.Tools != nil && d.embedClient != nil {
		if err := d.appState.Tools.EmbedDescriptors(ctx, d.embedClient); err != nil {
			log.Warn().Err(err).Msg("tool descriptor embedding failed; tool pruning will degrade to always_include-only")
		}
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
				outcome, err := pipeline.RunPipeline(ctx, d.pipe, pipeline.PresetCron(), input)
				if err != nil {
					log.Error().Err(err).Str("job", job.Name).Msg("cron job pipeline failed")
					return err
				}

				// Deliver the pipeline outcome to the configured channel.
				if job.DeliveryMode != "" && job.DeliveryMode != "none" && job.DeliveryChannel != "" && outcome.Content != "" {
					dq := d.router.DeliveryQueue()
					dq.Enqueue(job.DeliveryChannel, "", outcome.Content)
					log.Info().
						Str("job", job.Name).
						Str("channel", job.DeliveryChannel).
						Str("mode", job.DeliveryMode).
						Msg("cron job outcome enqueued for delivery")
				}

				return nil
			}), d.errBus)
		worker.Run(ctx)
	}()

	// Note: Telegram polling is handled by runChannelListener via router.PollAll().
	// No dedicated poller needed — PollAll calls Recv on all registered adapters.

	// Signal poll loop.
	if d.cfg.Channels.Signal != nil && d.cfg.Channels.Signal.Enabled {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.runSignalPoller(ctx)
		}()
	}

	// Email poll loop.
	if d.cfg.Channels.Email != nil && d.cfg.Channels.Email.Enabled {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.runEmailPoller(ctx)
		}()
	}

	// Start wallet balance poller.
	startWalletPoller(ctx, d.cfg, d.store, d.appState.Keystore)

	// Treasury refresh loop — derives treasury_state from cached wallet balances
	// on its own slower cadence, not the application-health heartbeat.
	if d.treasuryRefreshEnabled() {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.runTreasuryRefresh(ctx)
		}()
	}

	// Memory consolidation heartbeat — runs the dreaming cycle periodically.
	// Matches Rust's heartbeat-triggered consolidation.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.runConsolidationHeartbeat(ctx)
	}()

	// Maintenance heartbeat — runs cache and lease cleanup on a shared
	// heartbeat runtime instead of a bespoke maintenance loop.
	if d.maintenanceHeartbeatEnabled() {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.runMaintenanceHeartbeat(ctx)
		}()
	}

	log.Info().Msg("all subsystems started")
}

func (d *Daemon) consolidationHeartbeatInterval() time.Duration {
	if secs := d.cfg.Heartbeat.MemoryIntervalSeconds; secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if secs := d.cfg.Heartbeat.IntervalSeconds; secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return time.Hour
}

func (d *Daemon) treasuryRefreshEnabled() bool {
	return d.cfg.Heartbeat.TreasuryIntervalSeconds > 0
}

func (d *Daemon) treasuryRefreshInterval() time.Duration {
	if secs := d.cfg.Heartbeat.TreasuryIntervalSeconds; secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func (d *Daemon) newTreasuryRefresh() (*schedule.HeartbeatDaemon, schedule.DomainIntervals, func() *schedule.TickContext) {
	task := &schedule.TreasuryLoopTask{Store: d.store}
	interval := d.treasuryRefreshInterval()
	daemon := schedule.NewHeartbeatDaemon(interval, []schedule.HeartbeatTask{task})
	intervals := schedule.DefaultDomainIntervals()
	intervals.Financial = interval
	return daemon, intervals, func() *schedule.TickContext {
		return &schedule.TickContext{
			SurvivalTier: core.SurvivalTierStable,
			Timestamp:    time.Now(),
		}
	}
}

func (d *Daemon) newConsolidationHeartbeat() (*schedule.HeartbeatDaemon, schedule.DomainIntervals, func() *schedule.TickContext) {
	task := &schedule.MemoryLoopTask{
		Consolidate: func(ctx context.Context, force bool) string {
			report := pipeline.RunMemoryConsolidation(ctx, d.store, force, pipeline.ConsolidationOpts{
				EmbedClient: d.embedClient,
				LLMService:  d.llm,
			})
			log.Info().
				Int("indexed", report.Indexed).
				Int("deduped", report.Deduped).
				Int("promoted", report.Promoted).
				Int("pruned", report.Pruned).
				Msg("memory consolidation completed")
			return fmt.Sprintf("indexed=%d deduped=%d promoted=%d pruned=%d", report.Indexed, report.Deduped, report.Promoted, report.Pruned)
		},
	}
	interval := d.consolidationHeartbeatInterval()
	daemon := schedule.NewHeartbeatDaemon(interval, []schedule.HeartbeatTask{task})
	intervals := schedule.DefaultDomainIntervals()
	intervals.Memory = interval
	return daemon, intervals, func() *schedule.TickContext {
		return &schedule.TickContext{
			SurvivalTier: core.SurvivalTierStable,
			Timestamp:    time.Now(),
		}
	}
}

func (d *Daemon) maintenanceHeartbeatEnabled() bool {
	return d.cfg.Heartbeat.MaintenanceIntervalSeconds > 0 || d.cfg.Heartbeat.IntervalSeconds > 0
}

func (d *Daemon) maintenanceHeartbeatInterval() time.Duration {
	if secs := d.cfg.Heartbeat.MaintenanceIntervalSeconds; secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if secs := d.cfg.Heartbeat.IntervalSeconds; secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func (d *Daemon) newMaintenanceHeartbeat() (*schedule.HeartbeatDaemon, schedule.DomainIntervals, func() *schedule.TickContext) {
	task := &schedule.MaintenanceLoopTask{Store: d.store}
	interval := d.maintenanceHeartbeatInterval()
	daemon := schedule.NewHeartbeatDaemon(interval, []schedule.HeartbeatTask{task})
	intervals := schedule.DefaultDomainIntervals()
	intervals.Memory = interval
	return daemon, intervals, func() *schedule.TickContext {
		return &schedule.TickContext{
			SurvivalTier: core.SurvivalTierStable,
			Timestamp:    time.Now(),
		}
	}
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

	daemon, intervals, tickCtxFn := d.newConsolidationHeartbeat()
	log.Info().Dur("interval", intervals.Memory).Msg("memory consolidation heartbeat started")
	daemon.RunDistributed(ctx, intervals, tickCtxFn)
}

func (d *Daemon) runTreasuryRefresh(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	daemon, intervals, tickCtxFn := d.newTreasuryRefresh()
	log.Info().Dur("interval", intervals.Financial).Msg("treasury refresh loop started")
	daemon.RunDistributed(ctx, intervals, tickCtxFn)
}

func (d *Daemon) runMaintenanceHeartbeat(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	daemon, intervals, tickCtxFn := d.newMaintenanceHeartbeat()
	log.Info().Dur("interval", intervals.Memory).Msg("maintenance heartbeat started")
	daemon.RunDistributed(ctx, intervals, tickCtxFn)
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
