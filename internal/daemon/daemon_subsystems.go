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

		// Register MCP-discovered tools in the agent's ToolRegistry so they
		// appear alongside builtins during inference.
		if d.appState.Tools != nil && connected > 0 {
			mcpTools := agenttools.RegisterMCPTools(d.appState.Tools, d.appState.MCP)
			log.Info().Int("mcp_tools", mcpTools).Msg("MCP tools registered in agent tool registry")
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
			report := pipeline.RunMemoryConsolidation(ctx, d.store, false, pipeline.ConsolidationOpts{
				EmbedClient: d.embedClient,
				LLMService:  d.llm,
			})
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
