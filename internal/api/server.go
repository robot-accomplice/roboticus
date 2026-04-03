package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"

	"goboticus/internal/api/routes"
	"goboticus/internal/browser"
	"goboticus/internal/core"
	"goboticus/internal/db"
	"goboticus/internal/llm"
	"goboticus/internal/mcp"
	"goboticus/internal/pipeline"
	"goboticus/internal/plugin"
)

// AppState holds all shared state for the API server.
type AppState struct {
	Store      *db.Store
	Pipeline   pipeline.Runner // connectors must depend on the interface, not *pipeline.Pipeline
	LLM        *llm.Service
	Config     *core.Config
	EventBus   *EventBus
	Approvals  routes.ApprovalService
	MCP        *mcp.ConnectionManager
	MCPGateway *mcp.Gateway // serves the agent's tools to external MCP clients
	Plugins    *plugin.Registry
	Browser    *browser.Browser
}

// ServerConfig controls the HTTP server.
type ServerConfig struct {
	Port         int
	Bind         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	APIKey       string // empty = loopback-only
}

// DefaultServerConfig returns sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Port:         core.DefaultServerPort,
		Bind:         core.DefaultServerBind,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}
}

// NewServer creates the HTTP server with all routes and middleware.
func NewServer(cfg ServerConfig, state *AppState) *http.Server {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(NewRequestLogger())
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(cfg.WriteTimeout))
	r.Use(SecurityHeaders)
	r.Use(CORSMiddleware(state.Config.CORS.AllowedOrigins, state.Config.CORS.MaxAgeSeconds))
	r.Use(BodyLimit(1 << 20)) // 1MB
	r.Use(RateLimitMiddleware(
		state.Config.RateLimit.Enabled,
		state.Config.RateLimit.RequestsPerWindow,
		state.Config.RateLimit.WindowSeconds,
	))

	// Public routes (no auth).
	r.Group(func(r chi.Router) {
		r.Get("/api/health", routes.Health(state.Store, state.LLM))
		r.Get("/health", routes.Health(state.Store, state.LLM))
		r.Get("/.well-known/agent.json", routes.AgentCard())
		r.Get("/openapi.yaml", OpenAPIHandler())
		r.Get("/api/docs", DocsHandler())
		r.Post("/api/webhooks/telegram", routes.WebhookTelegram(state.Pipeline))
		r.Get("/api/webhooks/whatsapp", routes.WebhookWhatsAppVerify(state.Config.Channels.WhatsAppTokenEnv))
		r.Post("/api/webhooks/whatsapp", routes.WebhookWhatsApp(state.Pipeline))

		// MCP gateway — external MCP clients authenticate via their own mechanism.
		if state.MCPGateway != nil {
			r.Handle("/mcp", state.MCPGateway)
		}
	})

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(APIKeyAuth(cfg.APIKey))

		// Dashboard.
		r.Get("/", DashboardHandler())
		r.Get("/dashboard", DashboardHandler())

		// Agent inference.
		r.Post("/api/agent/message", routes.AgentMessage(state.Pipeline))
		r.Post("/api/agent/message/stream", routes.AgentMessageStream(state.Pipeline, state.LLM))
		r.Get("/api/agent/status", routes.AgentStatus(state.LLM))

		// Sessions.
		r.Get("/api/sessions", routes.ListSessions(state.Store))
		r.Post("/api/sessions", routes.CreateSession(state.Store))
		r.Get("/api/sessions/{id}", routes.GetSession(state.Store))
		r.Delete("/api/sessions/{id}", routes.DeleteSession(state.Store))
		r.Get("/api/sessions/{id}/messages", routes.ListMessages(state.Store))
		r.Post("/api/sessions/{id}/messages", routes.PostMessage(state.Pipeline))
		r.Get("/api/sessions/{id}/turns", routes.ListSessionTurns(state.Store))
		r.Get("/api/sessions/{id}/feedback", routes.GetSessionFeedback(state.Store))
		r.Get("/api/sessions/{id}/insights", routes.GetSessionInsights(state.Store))
		r.Post("/api/sessions/{id}/archive", routes.ArchiveSession(state.Store))
		r.Post("/api/sessions/{id}/analyze", routes.AnalyzeSession(state.Store))

		// Turns.
		r.Get("/api/turns/{id}", routes.GetTurn(state.Store))
		r.Get("/api/turns/{id}/feedback", routes.GetTurnFeedback(state.Store))
		r.Post("/api/turns/{id}/feedback", routes.PostTurnFeedback(state.Store))
		r.Get("/api/turns/{id}/context", routes.GetTurnContext(state.Store))
		r.Get("/api/turns/{id}/tools", routes.GetTurnTools(state.Store))
		r.Get("/api/turns/{id}/tips", routes.GetTurnTips(state.Store))
		r.Get("/api/turns/{id}/model-selection", routes.GetTurnModelSelection(state.Store))
		r.Post("/api/turns/{id}/analyze", routes.AnalyzeTurn(state.Store))

		// Memory.
		r.Get("/api/memory/working", routes.GetWorkingMemory(state.Store))
		r.Get("/api/memory/working/{session_id}", routes.GetSessionWorkingMemory(state.Store))
		r.Get("/api/memory/episodic", routes.GetEpisodicMemory(state.Store))
		r.Get("/api/memory/semantic", routes.GetSemanticMemory(state.Store))
		r.Get("/api/memory/semantic/categories", routes.GetSemanticCategories(state.Store))
		r.Get("/api/memory/search", routes.SearchMemory(state.Store))
		r.Get("/api/stats/memory-analytics", routes.GetMemoryAnalytics(state.Store))
		r.Get("/api/memory/health", routes.MemoryHealth(state.Store))

		// Cron.
		r.Get("/api/cron/jobs", routes.ListCronJobs(state.Store))
		r.Post("/api/cron/jobs", routes.CreateCronJob(state.Store))
		r.Get("/api/cron/runs", routes.ListCronRuns(state.Store))
		r.Get("/api/cron/jobs/{id}", routes.GetCronJob(state.Store))
		r.Put("/api/cron/jobs/{id}", routes.UpdateCronJob(state.Store))
		r.Delete("/api/cron/jobs/{id}", routes.DeleteCronJob(state.Store))
		r.Post("/api/cron/jobs/{id}/run", routes.RunCronJobNow(state.Pipeline, state.Store))

		// Skills.
		r.Get("/api/skills", routes.ListSkills(state.Store))
		r.Post("/api/skills/reload", routes.ReloadSkills())
		r.Delete("/api/skills/{id}", routes.DeleteSkill(state.Store))
		r.Put("/api/skills/{id}/toggle", routes.ToggleSkill(state.Store))
		r.Get("/api/skills/catalog", routes.GetSkillsCatalog())
		r.Post("/api/skills/catalog/install", routes.InstallSkillFromCatalog())
		r.Post("/api/skills/catalog/activate", routes.ActivateSkillFromCatalog())
		r.Get("/api/skills/audit", routes.AuditSkills(state.Store))
		r.Get("/api/skills/{id}", routes.GetSkill(state.Store))
		r.Put("/api/skills/{id}", routes.UpdateSkill(state.Store))

		// Plugins.
		r.Get("/api/plugins", routes.ListPlugins(state.Plugins))
		r.Get("/api/plugins/tools", routes.ListPluginTools(state.Plugins))
		r.Post("/api/plugins/{name}/enable", routes.EnablePlugin(state.Plugins))
		r.Post("/api/plugins/{name}/disable", routes.DisablePlugin(state.Plugins))
		r.Post("/api/plugins/catalog/install", routes.InstallPlugin())
		r.Post("/api/plugins/{name}/execute/{tool}", routes.ExecutePluginTool(state.Plugins))

		// Stats.
		r.Get("/api/stats/costs", routes.GetCosts(state.Store))
		r.Get("/api/stats/cache", routes.GetCacheStats(state.Store))
		r.Get("/api/stats/transactions", routes.GetTransactions(state.Store))
		r.Get("/api/stats/capacity", routes.GetCapacity(state.LLM))
		r.Get("/api/stats/efficiency", routes.GetEfficiency(state.Store))
		r.Get("/api/stats/timeseries", routes.GetTimeseries(state.Store))
		r.Get("/api/stats/escalation", routes.GetEscalationStats(state.LLM))
		r.Get("/api/stats/throttle", routes.GetThrottleStats(state.Store))
		r.Get("/api/delegations", routes.ListDelegations(state.Store))

		// Models.
		r.Get("/api/models/available", routes.GetAvailableModels(state.LLM))
		r.Get("/api/models/selections", routes.GetModelSelections(state.Store))
		r.Get("/api/models/routing-diagnostics", routes.GetRoutingDiagnostics(state.Config))

		// Recommendations.
		r.Get("/api/recommendations", routes.GetRecommendations(state.Store))
		r.Post("/api/recommendations/generate", routes.GenerateRecommendations(state.Store))

		// Channels.
		r.Get("/api/channels/status", routes.GetChannelsStatus(state.LLM))
		r.Get("/api/channels/dead-letter", routes.GetDeadLetters(state.Store))
		r.Post("/api/channels/{name}/test", routes.TestChannel())
		r.Post("/api/channels/dead-letter/{id}/replay", routes.ReplayDeadLetter(state.Store))

		// Config.
		r.Get("/api/config", routes.GetConfig(state.Config))
		r.Put("/api/config", routes.UpdateConfig(state.Store))
		r.Get("/api/config/capabilities", routes.GetCapabilities())
		r.Get("/api/config/raw", routes.GetConfigRaw())
		r.Put("/api/config/raw", routes.UpdateConfigRaw())

		// Wallet.
		r.Get("/api/wallet/balance", routes.GetWalletBalance(state.Store))
		r.Get("/api/wallet/address", routes.GetWalletAddress(state.Store))
		r.Get("/api/services/swaps", routes.GetSwaps(state.Store))
		r.Post("/api/services/swaps/{id}/start", routes.TransitionServiceRequest(state.Store, "started"))
		r.Post("/api/services/swaps/{id}/submit", routes.TransitionServiceRequest(state.Store, "submitted"))
		r.Post("/api/services/swaps/{id}/reconcile", routes.TransitionServiceRequest(state.Store, "reconciled"))
		r.Post("/api/services/swaps/{id}/confirm", routes.TransitionServiceRequest(state.Store, "confirmed"))
		r.Post("/api/services/swaps/{id}/fail", routes.TransitionServiceRequest(state.Store, "failed"))
		r.Get("/api/services/tax-payouts", routes.GetTaxPayouts(state.Store))
		r.Post("/api/services/tax-payouts/{id}/start", routes.TransitionServiceRequest(state.Store, "started"))
		r.Post("/api/services/tax-payouts/{id}/submit", routes.TransitionServiceRequest(state.Store, "submitted"))
		r.Post("/api/services/tax-payouts/{id}/reconcile", routes.TransitionServiceRequest(state.Store, "reconciled"))
		r.Post("/api/services/tax-payouts/{id}/confirm", routes.TransitionServiceRequest(state.Store, "confirmed"))
		r.Post("/api/services/tax-payouts/{id}/fail", routes.TransitionServiceRequest(state.Store, "failed"))

		// Revenue / Services.
		r.Get("/api/services/catalog", routes.ListServiceCatalog(state.Store))
		r.Get("/api/services/requests", routes.ListServiceRequests(state.Store))
		r.Get("/api/services/requests/{id}", routes.GetServiceRequest(state.Store))
		r.Get("/api/services/opportunities", routes.ListRevenueOpportunities(state.Store))
		r.Post("/api/services/opportunities/intake", routes.IntakeRevenueOpportunity(state.Store))
		r.Get("/api/services/opportunities/{id}", routes.GetRevenueOpportunity(state.Store))
		r.Post("/api/services/opportunities/{id}/score", routes.TransitionOpportunity(state.Store, "scored"))
		r.Post("/api/services/opportunities/{id}/qualify", routes.TransitionOpportunity(state.Store, "qualified"))
		r.Post("/api/services/opportunities/{id}/plan", routes.TransitionOpportunity(state.Store, "planned"))
		r.Post("/api/services/opportunities/{id}/fulfill", routes.TransitionOpportunity(state.Store, "fulfilled"))
		r.Post("/api/services/opportunities/{id}/settle", routes.TransitionOpportunity(state.Store, "settled"))

		// Roster (agents page).
		r.Get("/api/roster", routes.GetRoster(state.Store))
		r.Put("/api/roster/{agent}/model", routes.UpdateRosterModel(state.Store))

		// Workspace.
		r.Get("/api/workspace/state", routes.GetWorkspaceState(state.Store))

		// Runtime discovery.
		r.Get("/api/runtime/surfaces", routes.GetRuntimeSurfaces())
		r.Get("/api/runtime/discovery", routes.GetRuntimeDiscovery(state.Store))
		r.Post("/api/runtime/discovery", routes.RegisterDiscoveredAgent(state.Store))
		r.Get("/api/runtime/devices", routes.GetRuntimeDevices())

		// Provider key management.
		r.Put("/api/providers/{provider}/key", routes.SetProviderKey(state.Store))
		r.Delete("/api/providers/{provider}/key", routes.DeleteProviderKey(state.Store))

		// Traces.
		r.Get("/api/traces", routes.ListTraces(state.Store))
		r.Get("/api/traces/{turn_id}", routes.GetTrace(state.Store))

		// Themes.
		r.Get("/api/themes/catalog", routes.GetThemeCatalog())
		r.Get("/api/themes/active", routes.GetActiveTheme(state.Store))
		r.Put("/api/themes/active", routes.SetActiveTheme(state.Store))

		// MCP.
		r.Get("/api/mcp/connections", routes.ListMCPConnections(state.MCP))
		r.Get("/api/mcp/tools", routes.ListMCPTools(state.MCP))
		r.Post("/api/mcp/connect", routes.ConnectMCPServer(state.MCP))
		r.Post("/api/mcp/disconnect/{name}", routes.DisconnectMCPServer(state.MCP))

		// Browser.
		if state.Browser != nil {
			r.Get("/api/browser/status", routes.BrowserStatus(state.Browser))
			r.Post("/api/browser/start", routes.BrowserStart(state.Browser))
			r.Post("/api/browser/stop", routes.BrowserStop(state.Browser))
			r.Post("/api/browser/action", routes.BrowserAction(state.Browser))
		}

		// Logs.
		r.Get("/api/logs", routes.GetLogs())

		// Circuit breaker admin.
		r.Get("/api/breaker/status", routes.BreakerStatus(state.LLM))
		r.Post("/api/breaker/reset/{provider}", routes.BreakerReset(state.LLM))
		r.Post("/api/breaker/open/{provider}", routes.BreakerForceOpen(state.LLM))

		// Subagents.
		r.Get("/api/subagents", routes.ListSubagents(state.Store))
		r.Post("/api/subagents", routes.CreateSubagent(state.Store))
		r.Put("/api/subagents/{name}", routes.UpdateSubagent(state.Store))
		r.Post("/api/subagents/{name}/toggle", routes.ToggleSubagent(state.Store))
		r.Delete("/api/subagents/{name}", routes.DeleteSubagent(state.Store))

		// Interview.
		interviewMgr := routes.NewInterviewManager()
		r.Post("/api/interview/start", routes.InterviewStart(interviewMgr))
		r.Post("/api/interview/turn", routes.InterviewTurn(interviewMgr))
		r.Post("/api/interview/finish", routes.InterviewFinish(interviewMgr))

		// Approvals.
		if state.Approvals != nil {
			r.Get("/api/approvals", routes.ListApprovals(state.Approvals))
			r.Get("/api/approvals/{id}", routes.GetApproval(state.Approvals))
			r.Post("/api/approvals/{id}/approve", routes.ApproveRequest(state.Approvals))
			r.Post("/api/approvals/{id}/deny", routes.DenyRequest(state.Approvals))
		}

		// WebSocket.
		r.Get("/ws", HandleWebSocket(state.EventBus, cfg.APIKey))
		r.Post("/api/ws-ticket", routes.IssueWSTicket())
	})

	return &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Bind, cfg.Port),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
}

// ListenAndServe starts the server with graceful shutdown support.
func ListenAndServe(ctx context.Context, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", srv.Addr).Msg("server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Info().Msg("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
