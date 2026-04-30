package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"roboticus/internal/api/routes"
)

// BuildTopicSnapshots creates snapshot functions for each dashboard topic.
// Interface-driving topics should call shared producers directly. Legacy
// management/debug topics may still project existing HTTP handlers until they
// are moved behind producers.
func BuildTopicSnapshots(state *AppState) map[string]TopicSnapshotFunc {
	// Map topic names to their corresponding HTTP handler factories.
	handlers := map[string]http.HandlerFunc{
		TopicAgentStatus: routes.AgentStatus(state.LLM, state.Config),
		TopicModels:      routes.GetAvailableModels(state.LLM),
		TopicSessions:    routes.ListSessions(state.Store),
		TopicConfig:      routes.GetCapabilities(),
		TopicSkills:      routes.ListSkills(state.Store),
		TopicCron:        routes.ListCronJobs(state.Store),
		TopicPlugins:     routes.ListPlugins(state.Plugins),
		TopicChannels:    routes.GetChannelsStatus(state.Config, state.Keystore),
		TopicWallet:      routes.GetWalletBalance(state.Store),
		TopicRoster:      routes.GetRoster(state.Store, state.Config),
		TopicSubagents:   routes.ListSubagents(state.Store),
		TopicBreakers:    routes.GetBreakers(state.LLM),
		TopicApprovals:   routes.ListApprovals(state.Approvals),
		TopicTraces:      routes.ListTraces(state.Store),
	}

	// Stats uses a composite of multiple endpoints.
	costHandler := routes.GetCosts(state.Store)
	cacheHandler := routes.GetCacheStats(state.Store)

	snapshots := make(map[string]TopicSnapshotFunc, len(handlers)+5)

	// Simple 1:1 topic → handler mappings.
	for topic, handler := range handlers {
		h := handler // capture for closure
		snapshots[topic] = func() any {
			return invokeHandler(h)
		}
	}

	snapshots[TopicWorkspace] = func() any {
		payload, err := routes.BuildWorkspaceStatePayload(
			context.Background(),
			state.Store,
			state.Config,
		)
		if err != nil {
			payload["compatibility_status"] = "degraded"
			payload["error"] = err.Error()
		}
		return payload
	}

	// Composite stats snapshot.
	snapshots[TopicStats] = func() any {
		costs := invokeHandler(costHandler)
		cache := invokeHandler(cacheHandler)
		return map[string]any{"costs": costs, "cache": cache}
	}

	// MCP snapshot — may not have a connection manager.
	if state.MCP != nil {
		mcpHandler := routes.ListMCPServers(state.Config, state.MCP)
		snapshots[TopicMCP] = func() any {
			return invokeHandler(mcpHandler)
		}
	}

	// Routing composite.
	snapshots[TopicRouting] = func() any {
		return map[string]any{
			"breakers": invokeHandler(routes.GetBreakers(state.LLM)),
		}
	}

	return snapshots
}

// invokeHandler calls an HTTP handler via httptest and returns the parsed JSON response.
// This ensures snapshot data is identical to what the HTTP endpoint returns.
func invokeHandler(h http.HandlerFunc) any {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		return map[string]any{"error": w.Body.String()}
	}

	var result any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		return map[string]any{"error": "failed to parse response"}
	}
	return result
}
