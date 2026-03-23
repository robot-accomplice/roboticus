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
	"goboticus/internal/core"
	"goboticus/internal/db"
	"goboticus/internal/llm"
	"goboticus/internal/pipeline"
)

// AppState holds all shared state for the API server.
type AppState struct {
	Store    *db.Store
	Pipeline *pipeline.Pipeline
	LLM      *llm.Service
	Config   *core.Config
	EventBus *EventBus
}

// ServerConfig controls the HTTP server.
type ServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	APIKey       string // empty = loopback-only
}

// DefaultServerConfig returns sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Port:         core.DefaultServerPort,
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
	r.Use(BodyLimit(1 << 20)) // 1MB

	// Public routes (no auth).
	r.Group(func(r chi.Router) {
		r.Get("/api/health", routes.Health(state.Store, state.LLM))
		r.Get("/health", routes.Health(state.Store, state.LLM))
		r.Get("/.well-known/agent.json", routes.AgentCard())
		r.Post("/api/webhooks/telegram", routes.WebhookTelegram(state.Pipeline))
		r.Get("/api/webhooks/whatsapp", routes.WebhookWhatsAppVerify(state.Config.Channels.WhatsAppTokenEnv))
		r.Post("/api/webhooks/whatsapp", routes.WebhookWhatsApp(state.Pipeline))
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

		// Plugins.
		r.Post("/api/plugins/catalog/install", routes.InstallPlugin())

		// Stats.
		r.Get("/api/stats/costs", routes.GetCosts(state.Store))
		r.Get("/api/stats/cache", routes.GetCacheStats(state.Store))
		r.Get("/api/stats/transactions", routes.GetTransactions(state.Store))
		r.Get("/api/stats/capacity", routes.GetCapacity(state.Store))
		r.Get("/api/stats/efficiency", routes.GetEfficiency(state.Store))
		r.Get("/api/stats/timeseries", routes.GetTimeseries(state.Store))

		// Models.
		r.Get("/api/models/available", routes.GetAvailableModels(state.LLM))
		r.Get("/api/models/selections", routes.GetModelSelections(state.Store))
		r.Get("/api/models/routing-diagnostics", routes.GetRoutingDiagnostics(state.Store))

		// Recommendations.
		r.Get("/api/recommendations", routes.GetRecommendations(state.Store))
		r.Post("/api/recommendations/generate", routes.GenerateRecommendations(state.Store))

		// Channels.
		r.Get("/api/channels/status", routes.GetChannelsStatus(state.LLM))
		r.Get("/api/channels/dead-letter", routes.GetDeadLetters(state.Store))
		r.Post("/api/channels/{name}/test", routes.TestChannel())

		// Config.
		r.Get("/api/config", routes.GetConfig(state.Config))
		r.Put("/api/config", routes.UpdateConfig(state.Store))
		r.Get("/api/config/capabilities", routes.GetCapabilities())

		// Wallet.
		r.Get("/api/wallet/balance", routes.GetWalletBalance())
		r.Get("/api/wallet/address", routes.GetWalletAddress())
		r.Get("/api/services/swaps", routes.GetSwaps())
		r.Get("/api/services/tax-payouts", routes.GetTaxPayouts())

		// Roster (agents page).
		r.Get("/api/roster", routes.GetRoster(state.Store))
		r.Put("/api/roster/{agent}/model", routes.UpdateRosterModel(state.Store))

		// Workspace.
		r.Get("/api/workspace/state", routes.GetWorkspaceState(state.Store))

		// Provider key management.
		r.Put("/api/providers/{provider}/key", routes.SetProviderKey(state.Store))
		r.Delete("/api/providers/{provider}/key", routes.DeleteProviderKey(state.Store))

		// Logs.
		r.Get("/api/logs", routes.GetLogs())

		// Circuit breaker admin.
		r.Get("/api/breaker/status", routes.BreakerStatus(state.LLM))
		r.Post("/api/breaker/reset/{provider}", routes.BreakerReset(state.LLM))

		// Subagents.
		r.Get("/api/subagents", routes.ListSubagents(state.Store))
		r.Post("/api/subagents", routes.CreateSubagent(state.Store))
		r.Put("/api/subagents/{name}", routes.UpdateSubagent(state.Store))
		r.Post("/api/subagents/{name}/toggle", routes.ToggleSubagent(state.Store))
		r.Delete("/api/subagents/{name}", routes.DeleteSubagent(state.Store))

		// WebSocket.
		r.Get("/ws", HandleWebSocket(state.EventBus, cfg.APIKey))
		r.Post("/api/ws-ticket", routes.IssueWSTicket())
	})

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
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
