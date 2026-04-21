package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"

	"roboticus/internal/agent/tools"
	"roboticus/internal/api/routes"
	"roboticus/internal/browser"
	"roboticus/internal/channel"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/mcp"
	"roboticus/internal/pipeline"
	"roboticus/internal/plugin"
)

// AppState holds all shared state for the API server.
type AppState struct {
	Store           *db.Store
	Pipeline        pipeline.Runner          // connectors must depend on the interface, not *pipeline.Pipeline
	StreamFinalizer pipeline.StreamFinalizer // post-stream work (Rule 7.2 parity)
	LLM             *llm.Service
	Embeddings      *llm.EmbeddingClient
	Config          *core.Config
	Keystore        *core.Keystore
	EventBus        *EventBus
	Approvals       routes.ApprovalService
	Tools           *tools.Registry
	MCP             *mcp.ConnectionManager
	MCPGateway      *mcp.Gateway // serves the agent's tools to external MCP clients
	Plugins         *plugin.Registry
	Browser         *browser.Browser
	TelegramWebhook routesWebhookBatchParser
	WhatsAppWebhook routesWhatsAppWebhook
}

type routesWebhookBatchParser interface {
	ProcessWebhookBatch(data []byte) ([]channel.InboundMessage, error)
}

type routesWhatsAppWebhook interface {
	routesWebhookBatchParser
	VerifyWebhook(mode, token, challenge string) (string, bool)
	ValidateWebhookSignature(body []byte, signature string) bool
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
		WriteTimeout: 0, // No hard TCP deadline — chi middleware handles fast endpoint timeouts.
		// Long-running endpoints (inference, exercise) are exempt from chi timeout
		// and control their own deadline via client timeout + context cancellation.
	}
}

// NewServer creates the HTTP server with all routes and middleware.
// The context controls the lifetime of background goroutines (rate limit cleanup, ticket cleanup).
func NewServer(ctx context.Context, cfg ServerConfig, state *AppState) *http.Server {
	r := chi.NewRouter()
	mcpToolSurface := newMCPToolSurface(state.Tools, state.Embeddings)

	// Global middleware.
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(NewRequestLogger())
	r.Use(chimw.Recoverer)
	// Timeout middleware for non-streaming endpoints. WebSocket and SSE
	// connections are long-lived and must NOT be killed by this timeout.
	r.Use(func(next http.Handler) http.Handler {
		timeout := chimw.Timeout(60 * time.Second) // Fast endpoint deadline; long-running paths are exempt below.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip timeout for WebSocket upgrades, SSE streaming, and inference
			// endpoints — inference can legitimately take 30-120s with slow
			// models or cold-start local providers. The client controls its
			// own deadline via http.Client.Timeout.
			if r.URL.Path == "/ws" || strings.HasSuffix(r.URL.Path, "/stream") ||
				r.URL.Path == "/api/agent/message" ||
				r.URL.Path == "/api/models/exercise" {
				next.ServeHTTP(w, r)
				return
			}
			timeout(next).ServeHTTP(w, r)
		})
	})
	r.Use(SecurityHeaders)
	r.Use(CORSMiddleware(state.Config.CORS.AllowedOrigins, state.Config.CORS.MaxAgeSeconds))
	r.Use(BodyLimit(1 << 20)) // 1MB
	r.Use(RateLimitMiddleware(ctx,
		state.Config.RateLimit.Enabled,
		state.Config.RateLimit.RequestsPerWindow,
		state.Config.RateLimit.WindowSeconds,
	))

	// Public routes (no auth).
	r.Group(func(r chi.Router) {
		r.Get("/api/health", routes.Health(state.Store, state.LLM, state.Config))
		r.Get("/health", routes.Health(state.Store, state.LLM, state.Config))
		r.Get("/.well-known/agent.json", routes.AgentCard())
		r.Get("/openapi.yaml", OpenAPIHandler())
		r.Get("/api/docs", DocsHandler())
		r.Post("/api/webhooks/telegram", routes.WebhookTelegram(state.Pipeline, state.TelegramWebhook))
		r.Get("/api/webhooks/whatsapp", routes.WebhookWhatsAppVerify(state.WhatsAppWebhook))
		r.Post("/api/webhooks/whatsapp", routes.WebhookWhatsApp(state.Pipeline, state.WhatsAppWebhook, state.WhatsAppWebhook))

		// MCP gateway — external MCP clients authenticate via their own mechanism.
		if state.MCPGateway != nil {
			r.Handle("/mcp", state.MCPGateway)
		}
	})

	// Concurrency semaphore for heavy analysis routes.
	analysisSem := make(chan struct{}, 3)
	analysisLimit := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case analysisSem <- struct{}{}:
				defer func() { <-analysisSem }()
				next.ServeHTTP(w, r)
			default:
				http.Error(w, `{"error":"analysis capacity exceeded, try again later"}`, http.StatusServiceUnavailable)
			}
		})
	}

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(APIKeyAuth(cfg.APIKey))

		// Dashboard.
		r.Get("/", DashboardHandler())
		r.Get("/dashboard", DashboardHandler())

		// Agent inference.
		agentName := "Roboticus"
		if state.Config != nil && state.Config.Agent.Name != "" {
			agentName = state.Config.Agent.Name
		}
		r.Post("/api/agent/message", routes.AgentMessage(state.Pipeline, agentName))
		r.Post("/api/agent/message/stream", routes.AgentMessageStream(state.Pipeline, state.LLM, agentName, state.StreamFinalizer))
		r.Get("/api/agent/status", routes.AgentStatus(state.LLM, state.Config))

		// Sessions.
		r.Get("/api/sessions", routes.ListSessions(state.Store))
		r.Post("/api/sessions", routes.CreateSession(state.Store))
		r.Post("/api/sessions/backfill-nicknames", routes.BackfillNicknames(state.Store))
		r.Get("/api/sessions/{id}", routes.GetSession(state.Store))
		r.Delete("/api/sessions/{id}", routes.DeleteSession(state.Store))
		r.Get("/api/sessions/{id}/messages", routes.ListMessages(state.Store))
		r.Post("/api/sessions/{id}/messages", routes.PostMessage(state.Pipeline, agentName))
		r.Get("/api/sessions/{id}/turns", routes.ListSessionTurns(state.Store))
		r.Get("/api/sessions/{id}/feedback", routes.GetSessionFeedback(state.Store))
		r.Get("/api/sessions/{id}/insights", routes.GetSessionInsights(state.Store))
		r.Post("/api/sessions/{id}/archive", routes.ArchiveSession(state.Store))
		r.With(analysisLimit).Post("/api/sessions/{id}/analyze", routes.AnalyzeSession(state.Store, state.LLM))

		// Turns.
		r.Get("/api/turns/{id}", routes.GetTurn(state.Store))
		r.Get("/api/turns/{id}/feedback", routes.GetTurnFeedback(state.Store))
		r.Post("/api/turns/{id}/feedback", routes.PostTurnFeedback(state.Store))
		r.Put("/api/turns/{id}/feedback", routes.PutTurnFeedback(state.Store))
		r.Get("/api/turns/{id}/context", routes.GetTurnContext(state.Store))
		r.Get("/api/turns/{id}/tools", routes.GetTurnTools(state.Store))
		r.Get("/api/turns/{id}/tips", routes.GetTurnTips(state.Store))
		r.Get("/api/turns/{id}/model-selection", routes.GetTurnModelSelection(state.Store))
		r.With(analysisLimit).Post("/api/turns/{id}/analyze", routes.AnalyzeTurn(state.Store, state.LLM))

		// Memory.
		r.Get("/api/memory/working", routes.GetWorkingMemory(state.Store))
		r.Get("/api/memory/working/{session_id}", routes.GetSessionWorkingMemory(state.Store))
		r.Get("/api/memory/episodic", routes.GetEpisodicMemory(state.Store))
		r.Get("/api/memory/semantic", routes.GetSemanticMemory(state.Store))
		r.Get("/api/memory/semantic/categories", routes.GetSemanticCategories(state.Store))
		r.Get("/api/memory/semantic/{category}", routes.GetSemanticMemoryByCategory(state.Store))
		r.Get("/api/memory/search", routes.SearchMemory(state.Store))
		r.Post("/api/memory/consolidate", routes.TriggerConsolidation(state.Store))
		r.Post("/api/memory/reindex", routes.TriggerReindex(state.Store))
		r.Post("/api/knowledge/ingest", routes.IngestKnowledge(state.Store))
		r.Get("/api/stats/memory-analytics", routes.GetMemoryAnalytics(state.Store))
		r.Get("/api/memory/health", routes.MemoryHealth(state.Store))

		// Routing profile.
		var llmRouter *llm.Router
		if state.LLM != nil {
			llmRouter = state.LLM.Router()
		}
		r.Get("/api/routing/profile", routes.GetRoutingProfile(state.Store, llmRouter))
		r.Put("/api/routing/profile", routes.PutRoutingProfile(state.Store, llmRouter))

		// Circuit breaker observability and reset.
		r.Get("/api/routing/breakers", routes.GetBreakers(state.LLM))
		r.Post("/api/routing/breakers/reset", routes.ResetBreakers(state.LLM))
		r.Post("/api/routing/breakers/{provider}/reset", routes.ResetBreakerProvider(state.LLM))

		// Cron.
		r.Get("/api/cron/jobs", routes.ListCronJobs(state.Store))
		r.Post("/api/cron/jobs", routes.CreateCronJob(state.Store))
		r.Get("/api/cron/runs", routes.ListCronRuns(state.Store))
		r.Get("/api/cron/jobs/{id}", routes.GetCronJob(state.Store))
		r.Put("/api/cron/jobs/{id}", routes.UpdateCronJob(state.Store))
		r.Delete("/api/cron/jobs/{id}", routes.DeleteCronJob(state.Store))
		r.Post("/api/cron/jobs/{id}/run", routes.RunCronJobNow(state.Pipeline, state.Store, agentName))

		// Skills.
		r.Get("/api/skills", routes.ListSkills(state.Store))
		r.Post("/api/skills/reload", routes.ReloadSkills(func() error {
			// Reload skills from the configured directory by re-scanning.
			dir := state.Config.Skills.Directory
			if dir == "" {
				return nil
			}
			// Count files to confirm the directory is accessible.
			entries, err := os.ReadDir(dir)
			if err != nil {
				return fmt.Errorf("skills directory %q: %w", dir, err)
			}
			_ = entries
			return nil
		}))
		r.Delete("/api/skills/{id}", routes.DeleteSkill(state.Store))
		r.Put("/api/skills/{id}/toggle", routes.ToggleSkill(state.Store))
		r.Get("/api/skills/catalog", routes.GetSkillsCatalog(state.Store, state.Plugins, state.Config))
		r.Post("/api/skills/catalog/install", routes.InstallSkillFromCatalog(state.Config, state.Store))
		r.Post("/api/skills/catalog/activate", routes.ActivateSkillFromCatalog(state.Store))
		r.Get("/api/skills/audit", routes.AuditSkills(state.Store))
		r.Get("/api/skills/{id}", routes.GetSkill(state.Store))
		r.Put("/api/skills/{id}", routes.UpdateSkill(state.Store))

		// Plugins.
		r.Get("/api/plugins", routes.ListPlugins(state.Plugins))
		r.Get("/api/plugins/tools", routes.ListPluginTools(state.Plugins))
		r.Post("/api/plugins/{name}/enable", routes.EnablePlugin(state.Plugins, state.Tools, state.Embeddings))
		r.Post("/api/plugins/{name}/disable", routes.DisablePlugin(state.Plugins, state.Tools, state.Embeddings))
		r.Post("/api/plugins/catalog/install", routes.InstallPlugin(state.Config, state.Plugins, state.Tools, state.Embeddings))
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
		r.Get("/api/models/routing-diagnostics", routes.GetRoutingDiagnostics(state.Store, state.Config, state.LLM))
		r.Get("/api/models/policies", routes.ListModelPolicies(state.Store, state.Config))
		r.Put("/api/models/policies", routes.UpsertModelPolicy(state.Store, state.Config, state.LLM))
		r.Delete("/api/models/policies", routes.DeleteModelPolicy(state.Store, state.Config, state.LLM))
		r.Get("/api/models/routing-dataset", routes.GetRoutingDataset(state.Store))
		r.Post("/api/models/reset", routes.ResetModelScores(state.LLM))
		r.Post("/api/models/exercise", routes.ExerciseModel(state.Pipeline, state.Store, state.Config, agentName))
		r.Post("/api/models/exercise/runs", routes.StartExerciseRun(state.Store, state.Config))
		r.Get("/api/models/exercise/runs", routes.ListExerciseRuns(state.Store))
		r.Post("/api/models/exercise/runs/{runID}/results", routes.AppendExerciseRunResult(state.Store))
		r.Post("/api/models/exercise/runs/{runID}/complete", routes.CompleteExerciseRun(state.Store, state.Config))
		r.Get("/api/models/exercise/status", routes.GetExerciseStatus(state.Store))
		r.Get("/api/models/exercise/scorecard", routes.GetExerciseScorecard(state.Store))
		r.Post("/api/models/routing-eval", routes.RunRoutingEval(state.LLM))

		// Recommendations.
		r.Get("/api/recommendations", routes.GetRecommendations(state.Store))
		r.With(analysisLimit).Post("/api/recommendations/generate", routes.GenerateRecommendations(state.Store))

		// Channels.
		r.Get("/api/channels/status", routes.GetChannelsStatus(state.Config, state.Keystore))
		r.Get("/api/channels/dead-letter", routes.GetDeadLetters(state.Store))
		r.Post("/api/channels/{name}/test", routes.TestChannel(state.Config))
		r.Post("/api/channels/dead-letter/{id}/replay", routes.ReplayDeadLetter(state.Store))

		// Config.
		r.Get("/api/config", routes.GetConfig(state.Config, state.Keystore))
		r.Put("/api/config", routes.UpdateConfig(state.Config, state.Store))
		r.Get("/api/config/capabilities", routes.GetCapabilities())
		r.Get("/api/config/status", routes.GetConfigStatus())
		r.Get("/api/config/schema", routes.GetConfigSchema(state.Config))
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
		r.Post("/api/services/quote", routes.CreateServiceQuote(state.Store))
		r.Get("/api/services/requests", routes.ListServiceRequests(state.Store))
		r.Get("/api/services/requests/{id}", routes.GetServiceRequest(state.Store))
		r.Post("/api/services/requests/{id}/payment/verify", routes.VerifyServicePayment(state.Store))
		r.Post("/api/services/requests/{id}/fulfill", routes.FulfillServiceRequest(state.Store))
		r.Post("/api/services/requests/{id}/fail", routes.FailServiceRequest(state.Store))
		r.Get("/api/services/opportunities", routes.ListRevenueOpportunities(state.Store))
		r.Post("/api/services/opportunities/intake", routes.IntakeRevenueOpportunity(state.Store))
		r.Post("/api/services/opportunities/adapters/micro-bounty/intake", routes.IntakeMicroBounty(state.Store))
		r.Post("/api/services/opportunities/adapters/oracle-feed/intake", routes.IntakeOracleFeed(state.Store))
		r.Get("/api/services/opportunities/{id}", routes.GetRevenueOpportunity(state.Store))
		r.Post("/api/services/opportunities/{id}/score", routes.TransitionOpportunity(state.Store, "scored"))
		r.Post("/api/services/opportunities/{id}/qualify", routes.TransitionOpportunity(state.Store, "qualified"))
		r.Post("/api/services/opportunities/{id}/plan", routes.TransitionOpportunity(state.Store, "planned"))
		r.Post("/api/services/opportunities/{id}/fulfill", routes.TransitionOpportunity(state.Store, "fulfilled"))
		r.Post("/api/services/opportunities/{id}/settle", routes.TransitionOpportunity(state.Store, "settled"))
		r.Post("/api/services/opportunities/{id}/feedback", routes.RecordOpportunityFeedback(state.Store))

		// Roster (agents page).
		r.Get("/api/roster", routes.GetRoster(state.Store, state.Config))
		r.Put("/api/roster/{agent}/model", routes.UpdateRosterModel(state.Store))

		// Workspace.
		r.Get("/api/workspace/state", routes.GetWorkspaceState(state.Store, state.Config))
		r.Get("/api/workspace/tasks", routes.ListWorkspaceTasks(state.Store))
		r.Get("/api/admin/task-events", routes.GetTaskEvents(state.Store))

		// v1.0.6: system warnings (config-defaults-used, ambient DB
		// creation, etc.). Polled by the dashboard for the
		// top-of-page warning banner. See
		// internal/core/system_warnings.go for the collector
		// surface and internal/api/routes/system_warnings.go for
		// the wire shape.
		r.Get("/api/admin/system-warnings", routes.GetSystemWarnings())

		// Runtime discovery.
		r.Get("/api/runtime/surfaces", routes.GetRuntimeSurfaces())
		r.Get("/api/runtime/discovery", routes.GetRuntimeDiscovery(state.Store))
		r.Post("/api/runtime/discovery", routes.RegisterDiscoveredAgent(state.Store))
		r.Post("/api/runtime/discovery/{id}/verify", routes.VerifyDiscoveredAgent(state.Store))
		r.Get("/api/runtime/devices", routes.GetRuntimeDevices(state.Store))
		r.Post("/api/runtime/devices/pair", routes.PairRuntimeDevice(state.Store))
		r.Post("/api/runtime/devices/{id}/verify", routes.VerifyPairedDevice(state.Store))
		r.Delete("/api/runtime/devices/{id}", routes.UnpairDevice(state.Store))
		r.Get("/api/runtime/mcp", routes.GetMCPRuntime(state.Config, state.MCP))
		r.Post("/api/runtime/mcp/clients/{name}/discover", routes.DiscoverMCPTools(state.MCP, mcpToolSurface))
		r.Post("/api/runtime/mcp/clients/{name}/disconnect", routes.DisconnectMCPClient(state.MCP, mcpToolSurface))

		// Provider key management.
		r.Put("/api/providers/{provider}/key", routes.SetProviderKey(state.Keystore))
		r.Delete("/api/providers/{provider}/key", routes.DeleteProviderKey(state.Keystore))

		// Traces.
		r.Get("/api/traces", routes.ListTraces(state.Store))
		r.Get("/api/traces/search", routes.SearchTraces(state.Store))
		r.Get("/api/traces/{turn_id}", routes.GetTrace(state.Store))
		r.Get("/api/traces/{turn_id}/react", routes.GetReactTrace(state.Store))
		r.Get("/api/traces/{turn_id}/export", routes.ExportTrace(state.Store))
		r.Post("/api/traces/{turn_id}/replay", routes.ReplayTrace(state.Store))
		r.Get("/api/traces/{turn_id}/flow", routes.GetTraceFlow(state.Store))
		r.Get("/api/traces/{turn_id}/diagnostics", routes.GetTurnDiagnostics(state.Store))

		// Themes.
		r.Get("/api/themes", routes.GetThemesList())
		r.Get("/api/themes/catalog", routes.GetThemeCatalog(state.Store))
		r.Post("/api/themes/catalog/install", routes.InstallCatalogTheme(state.Store))
		r.Post("/api/themes/catalog/uninstall", routes.UninstallCatalogTheme(state.Store))
		r.Get("/api/themes/{id}/textures/{filename}", routes.ServeThemeTexture())
		r.Get("/api/themes/active", routes.GetActiveTheme(state.Store))
		r.Put("/api/themes/active", routes.SetActiveTheme(state.Store))

		// MCP.
		r.Get("/api/mcp/servers", routes.ListMCPServers(state.Config, state.MCP))
		r.Get("/api/mcp/servers/{name}", routes.GetMCPServer(state.Config, state.MCP))
		r.Post("/api/mcp/servers/{name}/test", routes.TestMCPServer(state.Config))
		r.Post("/api/mcp/servers/{name}/validate-sse", routes.ValidateSSEMCPServer(state.Config))
		r.Get("/api/mcp/connections", routes.ListMCPConnections(state.MCP))
		r.Get("/api/mcp/tools", routes.ListMCPTools(state.MCP))
		r.Post("/api/mcp/connect", routes.ConnectMCPServer(state.MCP, mcpToolSurface))
		r.Post("/api/mcp/disconnect/{name}", routes.DisconnectMCPServer(state.MCP, mcpToolSurface))

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
		r.Get("/api/subagents/retirement-candidates", routes.SubagentRetirementCandidates(state.Store))
		r.Post("/api/subagents/retire-unused", routes.RetireUnusedSubagents(state.Store))
		r.Put("/api/subagents/{name}", routes.UpdateSubagent(state.Store))
		r.Post("/api/subagents/{name}/toggle", routes.ToggleSubagent(state.Store))
		r.Delete("/api/subagents/{name}", routes.DeleteSubagent(state.Store))

		// Agents.
		r.Get("/api/agents", routes.ListAgents(state.Store))
		r.Post("/api/agents/{id}/start", routes.StartAgent(state.Store))
		r.Post("/api/agents/{id}/stop", routes.StopAgent(state.Store))
		r.Post("/api/a2a/hello", routes.A2AHello())

		// Audit.
		r.Get("/api/audit/policy/{turn_id}", routes.GetPolicyAudit(state.Store))
		r.Get("/api/audit/tools/{turn_id}", routes.GetToolAudit(state.Store))

		// Observability.
		r.Get("/api/observability/traces", routes.ListObservabilityTraces(state.Store))
		r.Get("/api/observability/traces/{id}/waterfall", routes.TraceWaterfall(state.Store))
		r.Get("/api/observability/delegation/outcomes", routes.DelegationOutcomes(state.Store))
		r.Get("/api/observability/delegation/stats", routes.DelegationStats(state.Store))

		// Keystore.
		r.Get("/api/keystore/status", routes.KeystoreStatus(state.Keystore))
		r.Post("/api/keystore/unlock", routes.KeystoreUnlock())

		// Interview.
		interviewWorkspace := ""
		if state.Config != nil {
			interviewWorkspace = state.Config.Agent.Workspace
		}
		interviewMgr := routes.NewInterviewManager(state.LLM, interviewWorkspace)
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
		wsTickets := NewTicketStore(ctx, 60*time.Second)
		r.Get("/ws", HandleWebSocket(WSHandlerDeps{
			Bus:       state.EventBus,
			APIKey:    cfg.APIKey,
			Tickets:   wsTickets,
			Snapshots: BuildTopicSnapshots(state),
		}))
		r.Post("/api/ws-ticket", routes.IssueWSTicket(wsTickets))
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
