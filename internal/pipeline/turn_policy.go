package pipeline

import (
	"context"
	"strings"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/llm"
)

type TurnWeight string

const (
	TurnWeightLight    TurnWeight = "light"
	TurnWeightStandard TurnWeight = "standard"
	TurnWeightHeavy    TurnWeight = "heavy"
)

type TurnEnvelopePolicy struct {
	Weight                 TurnWeight
	ContextBudget          int
	AllowRetrieval         bool
	LightweightToolSurface bool
	MaxTools               int
	AllowRetryExpansion    bool
	Reason                 string
}

func DeriveTurnEnvelopePolicy(content string, synthesis TaskSynthesis, sessionTurns int) TurnEnvelopePolicy {
	words := len(strings.Fields(strings.TrimSpace(content)))

	switch {
	case synthesis.Complexity == "complex" || synthesis.Intent == "code":
		return TurnEnvelopePolicy{
			Weight:              TurnWeightHeavy,
			ContextBudget:       defaultTokenBudget,
			AllowRetrieval:      true,
			AllowRetryExpansion: false,
			Reason:              "complex or action-oriented turn requires the full adaptive envelope",
		}
	case synthesis.Intent == "task" &&
		synthesis.Complexity == "simple" &&
		synthesis.PlannedAction == "execute_directly":
		return TurnEnvelopePolicy{
			Weight:              TurnWeightStandard,
			ContextBudget:       2048,
			AllowRetrieval:      synthesis.RetrievalNeeded,
			MaxTools:            6,
			AllowRetryExpansion: true,
			Reason:              "simple direct task should stay on a focused execution envelope",
		}
	case synthesis.Intent == "task":
		return TurnEnvelopePolicy{
			Weight:              TurnWeightHeavy,
			ContextBudget:       defaultTokenBudget,
			AllowRetrieval:      true,
			AllowRetryExpansion: false,
			Reason:              "multi-step task requires the full adaptive envelope",
		}
	case synthesis.Complexity == "simple" &&
		(synthesis.Intent == "conversational" || synthesis.Intent == "general") &&
		words <= 24:
		return TurnEnvelopePolicy{
			Weight:                 TurnWeightLight,
			ContextBudget:          1536,
			AllowRetrieval:         false,
			LightweightToolSurface: true,
			AllowRetryExpansion:    true,
			Reason:                 "simple conversational turn should start with a minimal envelope",
		}
	default:
		budget := defaultTokenBudget
		if sessionTurns <= 3 {
			budget = 4096
		}
		return TurnEnvelopePolicy{
			Weight:              TurnWeightStandard,
			ContextBudget:       budget,
			AllowRetrieval:      synthesis.RetrievalNeeded,
			AllowRetryExpansion: true,
			Reason:              "default adaptive envelope for normal turns",
		}
	}
}

func (p TurnEnvelopePolicy) Expanded() TurnEnvelopePolicy {
	if p.Weight != TurnWeightLight {
		return p
	}
	return TurnEnvelopePolicy{
		Weight:              TurnWeightStandard,
		ContextBudget:       4096,
		AllowRetrieval:      true,
		AllowRetryExpansion: false,
		Reason:              "light envelope widened after the first pass proved insufficient",
	}
}

func shouldKeepSocialTurnAmbientContextMinimal(policy TurnEnvelopePolicy, synthesis TaskSynthesis) bool {
	return policy.Weight == TurnWeightLight && synthesis.Intent == "conversational"
}

func (p TurnEnvelopePolicy) applyToolPolicy(ctx context.Context, session *Session, pruner ToolPruner) (agenttools.ToolSearchStats, error) {
	if session == nil {
		return agenttools.ToolSearchStats{}, nil
	}
	if p.LightweightToolSurface {
		session.SetSelectedToolDefs([]llm.ToolDef{})
		return agenttools.ToolSearchStats{
			CandidatesSelected: 0,
			EmbeddingStatus:    "policy_lightweight",
		}, nil
	}
	if pruner == nil {
		return agenttools.ToolSearchStats{}, nil
	}
	defs, stats, err := pruner.PruneTools(ctx, session)
	if err != nil {
		return stats, err
	}
	if p.MaxTools > 0 && len(defs) > p.MaxTools {
		original := len(defs)
		defs = defs[:p.MaxTools]
		stats.CandidatesSelected = len(defs)
		if original > len(defs) {
			stats.CandidatesPruned += original - len(defs)
		}
	}
	session.SetSelectedToolDefs(defs)
	return stats, nil
}
