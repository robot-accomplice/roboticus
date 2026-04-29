package routes

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// buildAgentRegistryView is the shared roster composition seam for operator
// agent inventory surfaces. Callers may project the returned rows differently,
// but they must not independently reconstruct orchestrator/subagent truth.
func buildAgentRegistryView(ctx context.Context, store *db.Store, cfg *core.Config) ([]map[string]any, error) {
	rq := db.NewRouteQueries(store)

	primaryName := "roboticus"
	primaryID := "default"
	primaryModel := "auto"
	if cfg != nil {
		if cfg.Agent.Name != "" {
			primaryName = cfg.Agent.Name
		}
		if cfg.Agent.ID != "" {
			primaryID = cfg.Agent.ID
		}
		if cfg.Models.Primary != "" {
			primaryModel = cfg.Models.Primary
		}
	}

	primaryState, primaryActivity := primaryAgentRuntimeState(ctx, rq)

	agents := []map[string]any{
		{
			"name":         strings.ToLower(primaryName),
			"display_name": primaryName,
			"id":           primaryID,
			"model":        primaryModel,
			"enabled":      true,
			"state":        primaryState,
			"status":       primaryState,
			"activity":     primaryActivity,
			"color":        "#6366f1",
			"role":         "orchestrator",
		},
	}

	subagents, _, _, _, err := buildSubagentRosterCards(ctx, rq, nil, primaryName)
	if err != nil {
		return agents, err
	}
	agents = append(agents, subagents...)
	return agents, nil
}

func primaryAgentRuntimeState(ctx context.Context, rq *db.RouteQueries) (state string, activity string) {
	if active, err := rq.HasRecentActivity(ctx, 30); err == nil && active {
		return "running", "inference"
	} else if err != nil {
		log.Warn().Err(err).Msg("failed to query recent agent activity")
	}
	return "sleeping", "idle"
}
