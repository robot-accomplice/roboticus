package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// TestParity_APIRouteSet verifies that all operator-critical API routes
// are registered. This test fails when expected routes are missing,
// catching accidental route registration regressions.
func TestParity_APIRouteSet(t *testing.T) {
	store := testutil.TempStore(t)
	cfg := core.DefaultConfig()
	svc, _ := llm.NewService(llm.ServiceConfig{
		Primary: "test/test",
		Providers: []llm.Provider{
			{Name: "test", URL: "http://localhost", Format: "openai"},
		},
	}, nil)

	state := &AppState{
		Store:  store,
		Config: &cfg,
		LLM:    svc,
	}

	srv := NewServer(context.Background(), DefaultServerConfig(), state)
	chiRouter, ok := srv.Handler.(chi.Routes)
	if !ok {
		t.Fatal("server handler is not chi.Routes — cannot walk route tree")
	}

	var registered []string
	err := chi.Walk(chiRouter, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		registered = append(registered, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	// Critical routes that must exist for Rust parity.
	expected := []string{
		// Core agent
		"GET /api/health",
		"GET /api/agent/status",
		"POST /api/agent/message",
		"POST /api/agent/message/stream",
		"POST /api/ws-ticket",

		// Sessions
		"GET /api/sessions",
		"POST /api/sessions",
		"GET /api/sessions/{id}",
		"DELETE /api/sessions/{id}",
		"GET /api/sessions/{id}/messages",
		"GET /api/sessions/{id}/turns",
		"GET /api/sessions/{id}/feedback",
		"GET /api/sessions/{id}/insights",
		"POST /api/sessions/{id}/analyze",

		// Turns
		"GET /api/turns/{id}",
		"GET /api/turns/{id}/context",
		"GET /api/turns/{id}/tools",
		"GET /api/turns/{id}/tips",
		"GET /api/turns/{id}/model-selection",
		"POST /api/turns/{id}/analyze",

		// Memory
		"GET /api/memory/working",
		"GET /api/memory/episodic",
		"GET /api/memory/semantic",
		"GET /api/memory/semantic/categories",
		"GET /api/memory/search",
		"POST /api/memory/consolidate",
		"POST /api/memory/reindex",

		// Skills
		"GET /api/skills",
		"GET /api/skills/{id}",
		"GET /api/skills/catalog",
		"POST /api/skills/catalog/install",
		"POST /api/skills/catalog/activate",
		"POST /api/skills/reload",

		// Config
		"GET /api/config",
		"PUT /api/config",
		"GET /api/config/raw",
		"PUT /api/config/raw",
		"GET /api/config/capabilities",

		// Models
		"GET /api/models/available",
		"GET /api/models/selections",
		"GET /api/models/routing-diagnostics",
		"GET /api/models/routing-dataset",
		"POST /api/models/reset",
		"POST /api/models/exercise",
		"GET /api/models/exercise/scorecard",

		// Routing profile
		"GET /api/routing/profile",
		"PUT /api/routing/profile",

		// Cron
		"GET /api/cron/jobs",
		"POST /api/cron/jobs",
		"GET /api/cron/runs",

		// Wallet
		"GET /api/wallet/balance",
		"GET /api/wallet/address",

		// Channels
		"GET /api/channels/status",
		"GET /api/channels/dead-letter",

		// Roster
		"GET /api/roster",

		// Workspace
		"GET /api/workspace/state",
		"GET /api/workspace/tasks",

		// Breaker
		"GET /api/breaker/status",

		// Keystore
		"GET /api/keystore/status",
		"POST /api/keystore/unlock",

		// Provider keys
		"PUT /api/providers/{provider}/key",
		"DELETE /api/providers/{provider}/key",

		// Themes
		"GET /api/themes",
		"GET /api/themes/catalog",
		"POST /api/themes/catalog/install",
		"GET /api/themes/active",
		"PUT /api/themes/active",

		// MCP
		"GET /api/mcp/servers",
		"GET /api/mcp/servers/{name}",
		"POST /api/mcp/servers/{name}/test",
		"GET /api/mcp/connections",
		"GET /api/mcp/tools",
		"POST /api/mcp/connect",

		// Runtime
		"GET /api/runtime/surfaces",
		"GET /api/runtime/discovery",
		"GET /api/runtime/devices",
		"GET /api/runtime/mcp",
		"POST /api/runtime/devices/pair",

		// Traces
		"GET /api/traces",
		"GET /api/traces/search",

		// Observability
		"GET /api/observability/traces",
		"GET /api/observability/delegation/outcomes",
		"GET /api/observability/delegation/stats",

		// Stats
		"GET /api/stats/costs",
		"GET /api/stats/cache",
		"GET /api/stats/efficiency",
		"GET /api/stats/timeseries",
		"GET /api/stats/transactions",
		"GET /api/stats/memory-analytics",

		// Plugins
		"GET /api/plugins",

		// Agents
		"GET /api/agents",

		// Subagents
		"GET /api/subagents",
		"POST /api/subagents",

		// Recommendations
		"GET /api/recommendations",

		// Admin task events
		"GET /api/admin/task-events",
	}

	// Build a set of registered routes for lookup.
	registeredSet := make(map[string]bool)
	for _, r := range registered {
		registeredSet[r] = true
	}

	var missing []string
	for _, exp := range expected {
		if !registeredSet[exp] {
			missing = append(missing, exp)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("missing %d critical API routes:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}
