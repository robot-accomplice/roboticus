package pipeline

import (
	"context"
	"sort"
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

type ToolProfile string

const (
	ToolProfileDefault          ToolProfile = "default"
	ToolProfileFocusedAuthoring ToolProfile = "focused_authoring"
)

type TurnEnvelopePolicy struct {
	Weight                 TurnWeight
	ContextBudget          int
	AllowRetrieval         bool
	LightweightToolSurface bool
	MaxTools               int
	AllowRetryExpansion    bool
	RequireArtifactWrite   bool
	AllowAuthorityMutation bool
	ToolProfile            ToolProfile
	Reason                 string
}

func DeriveTurnEnvelopePolicy(content string, synthesis TaskSynthesis, sessionTurns int) TurnEnvelopePolicy {
	words := len(strings.Fields(strings.TrimSpace(content)))
	requiresArtifactWrite := looksLikeBoundedAuthoringTask(strings.ToLower(content))
	allowAuthorityMutation := requiresExplicitAuthorityMutation(content)

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
		synthesis.PlannedAction == "execute_directly" &&
		(synthesis.Complexity == "simple" || requiresArtifactWrite):
		return TurnEnvelopePolicy{
			Weight:                 TurnWeightStandard,
			ContextBudget:          2048,
			AllowRetrieval:         synthesis.RetrievalNeeded,
			MaxTools:               6,
			AllowRetryExpansion:    true,
			RequireArtifactWrite:   requiresArtifactWrite,
			AllowAuthorityMutation: allowAuthorityMutation,
			ToolProfile:            toolProfileForTurn(requiresArtifactWrite, allowAuthorityMutation),
			Reason:                 "direct artifact authoring should stay on a focused execution envelope",
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
	defs, filtered := filterToolDefsForPolicy(defs, p)
	if filtered > 0 {
		stats.CandidatesPruned += filtered
	}
	stats.CandidatesSelected = len(defs)
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

func filterToolDefsForPolicy(defs []llm.ToolDef, policy TurnEnvelopePolicy) ([]llm.ToolDef, int) {
	if len(defs) == 0 {
		return defs, 0
	}

	filtered := make([]llm.ToolDef, 0, len(defs))
	removed := 0
	for _, def := range defs {
		name := strings.TrimSpace(def.Function.Name)
		if name == "" {
			filtered = append(filtered, def)
			continue
		}
		if policy.RequireArtifactWrite && !policy.AllowAuthorityMutation && agenttools.MutatesAuthorityLayer(name) {
			removed++
			continue
		}
		if !toolAllowedForPolicy(name, policy) {
			removed++
			continue
		}
		filtered = append(filtered, def)
	}

	if !policy.RequireArtifactWrite {
		return filtered, removed
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return toolPriorityForPolicy(filtered[i].Function.Name, policy) < toolPriorityForPolicy(filtered[j].Function.Name, policy)
	})
	return filtered, removed
}

func toolPriorityForPolicy(name string, policy TurnEnvelopePolicy) int {
	if policy.ToolProfile == ToolProfileFocusedAuthoring {
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactWrite:
			return 0
		case agenttools.OperationRuntimeContextRead:
			return 1
		case agenttools.OperationMemoryRead:
			return 2
		case agenttools.OperationWorkspaceInspect:
			return 3
		case agenttools.OperationExecution:
			return 4
		case agenttools.OperationTaskInspection:
			return 5
		case agenttools.OperationCapabilityInventory:
			return 6
		case agenttools.OperationDelegation:
			return 7
		case agenttools.OperationAuthorityWrite:
			return 8
		default:
			return 9
		}
	}
	if policy.RequireArtifactWrite {
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactWrite:
			return 0
		case agenttools.OperationRuntimeContextRead, agenttools.OperationWorkspaceInspect,
			agenttools.OperationCapabilityInventory, agenttools.OperationTaskInspection,
			agenttools.OperationInspection:
			return 1
		case agenttools.OperationExecution:
			return 2
		case agenttools.OperationMemoryRead:
			return 3
		case agenttools.OperationDelegation:
			return 4
		case agenttools.OperationAuthorityWrite:
			return 5
		default:
			return 6
		}
	}
	return 0
}

func toolProfileForTurn(requireArtifactWrite, allowAuthorityMutation bool) ToolProfile {
	if requireArtifactWrite && !allowAuthorityMutation {
		return ToolProfileFocusedAuthoring
	}
	return ToolProfileDefault
}

func toolAllowedForPolicy(name string, policy TurnEnvelopePolicy) bool {
	switch policy.ToolProfile {
	case ToolProfileFocusedAuthoring:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactWrite, agenttools.OperationRuntimeContextRead:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	default:
		return true
	}
}

func requiresExplicitAuthorityMutation(content string) bool {
	lower := strings.ToLower(content)
	actionMarkers := []string{
		"ingest", "record", "store", "save", "capture", "persist", "promote",
		"register", "add", "create", "update", "revise",
	}
	authorityMarkers := []string{
		"policy", "spec", "specification", "runbook", "procedure", "rule",
		"playbook", "guideline", "canonical memory", "semantic memory",
	}
	return containsAnyMarker(lower, actionMarkers) && containsAnyMarker(lower, authorityMarkers)
}
