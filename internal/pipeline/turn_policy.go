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
	ToolProfileDefault                  ToolProfile = "default"
	ToolProfileFocusedAuthoring         ToolProfile = "focused_authoring"
	ToolProfileFocusedAnalysisAuthoring ToolProfile = "focused_analysis_authoring"
	ToolProfileFocusedSourceCode        ToolProfile = "focused_source_code"
	ToolProfileFocusedInspection        ToolProfile = "focused_inspection"
	ToolProfileFocusedScheduling        ToolProfile = "focused_scheduling"
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
	lower := strings.ToLower(content)
	requiresArtifactWrite := len(ParseExpectedArtifactSpecs(content)) > 0 || looksLikeBoundedAuthoringTask(lower) || looksLikeFilesystemAuthoringTurn(lower)
	allowAuthorityMutation := requiresExplicitAuthorityMutation(content)
	requiresScheduling := looksLikeSchedulingTask(lower)
	requiresInspection := looksLikeFocusedInspectionTurn(content)
	requiresAnalysisAuthoring := looksLikeInspectionBackedArtifactAuthoring(content)

	switch {
	case synthesis.Intent == "code" &&
		synthesis.PlannedAction == "execute_directly" &&
		looksLikeSourceBackedCodeTask(content):
		return TurnEnvelopePolicy{
			Weight:              TurnWeightStandard,
			ContextBudget:       3072,
			AllowRetrieval:      synthesis.RetrievalNeeded,
			MaxTools:            8,
			AllowRetryExpansion: true,
			ToolProfile:         ToolProfileFocusedSourceCode,
			Reason:              "source-backed code surgery should stay on a repo-grounded focused execution envelope",
		}
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
		requiresScheduling:
		return TurnEnvelopePolicy{
			Weight:                 TurnWeightStandard,
			ContextBudget:          2048,
			AllowRetrieval:         false,
			MaxTools:               4,
			AllowRetryExpansion:    true,
			AllowAuthorityMutation: false,
			ToolProfile:            ToolProfileFocusedScheduling,
			Reason:                 "direct scheduling should stay on a focused scheduling envelope",
		}
	case synthesis.Intent == "task" &&
		synthesis.PlannedAction == "execute_directly" &&
		(synthesis.Complexity == "simple" || requiresArtifactWrite):
		profile := toolProfileForTurn(requiresArtifactWrite, allowAuthorityMutation)
		maxTools := 6
		if requiresAnalysisAuthoring {
			profile = ToolProfileFocusedAnalysisAuthoring
			maxTools = 8
		} else if !requiresArtifactWrite && requiresInspection {
			profile = ToolProfileFocusedInspection
		}
		return TurnEnvelopePolicy{
			Weight:                 TurnWeightStandard,
			ContextBudget:          2048,
			AllowRetrieval:         synthesis.RetrievalNeeded,
			MaxTools:               maxTools,
			AllowRetryExpansion:    true,
			RequireArtifactWrite:   requiresArtifactWrite,
			AllowAuthorityMutation: allowAuthorityMutation,
			ToolProfile:            profile,
			Reason:                 directExecutionPolicyReason(requiresArtifactWrite, requiresInspection, requiresAnalysisAuthoring),
		}
	case synthesis.Intent == "task":
		return TurnEnvelopePolicy{
			Weight:              TurnWeightHeavy,
			ContextBudget:       defaultTokenBudget,
			AllowRetrieval:      true,
			AllowRetryExpansion: false,
			Reason:              "multi-step task requires the full adaptive envelope",
		}
	case synthesis.Intent == "question" &&
		looksLikeDerivableDirectFactQuestion(content):
		return TurnEnvelopePolicy{
			Weight:                 TurnWeightLight,
			ContextBudget:          1536,
			AllowRetrieval:         false,
			LightweightToolSurface: true,
			AllowRetryExpansion:    true,
			Reason:                 "derivable direct-fact questions should stay on a minimal no-retrieval envelope",
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

	if !policy.RequireArtifactWrite && policy.ToolProfile == ToolProfileDefault {
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
		case agenttools.OperationArtifactRead:
			return 1
		case agenttools.OperationRuntimeContextRead:
			return 2
		case agenttools.OperationMemoryRead:
			return 3
		case agenttools.OperationWorkspaceInspect:
			return 4
		case agenttools.OperationExecution:
			return 5
		case agenttools.OperationTaskInspection:
			return 6
		case agenttools.OperationCapabilityInventory:
			return 7
		case agenttools.OperationDelegation:
			return 8
		case agenttools.OperationAuthorityWrite:
			return 9
		default:
			return 10
		}
	}
	if policy.ToolProfile == ToolProfileFocusedAnalysisAuthoring {
		switch strings.TrimSpace(strings.ToLower(name)) {
		case "inventory_projects":
			return 0
		case "list_directory":
			return 1
		case "bash":
			return 2
		case "search_files", "glob_files":
			return 3
		}
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactRead:
			return 4
		case agenttools.OperationArtifactWrite:
			return 5
		case agenttools.OperationRuntimeContextRead:
			return 6
		case agenttools.OperationMemoryRead:
			return 7
		default:
			return 8
		}
	}
	if policy.ToolProfile == ToolProfileFocusedSourceCode {
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactRead:
			return 0
		case agenttools.OperationWorkspaceInspect:
			return 1
		case agenttools.OperationArtifactWrite:
			return 2
		case agenttools.OperationRuntimeContextRead:
			return 3
		case agenttools.OperationExecution:
			return 4
		case agenttools.OperationMemoryRead:
			return 5
		default:
			return 6
		}
	}
	if policy.ToolProfile == ToolProfileFocusedScheduling {
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationScheduling:
			return 0
		case agenttools.OperationRuntimeContextRead:
			return 1
		case agenttools.OperationTaskInspection:
			return 2
		case agenttools.OperationCapabilityInventory:
			return 3
		case agenttools.OperationMemoryRead:
			return 4
		default:
			return 5
		}
	}
	if policy.ToolProfile == ToolProfileFocusedInspection {
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationWorkspaceInspect:
			return 0
		case agenttools.OperationArtifactRead:
			return 1
		case agenttools.OperationRuntimeContextRead:
			return 2
		case agenttools.OperationMemoryRead:
			return 3
		default:
			return 4
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
	case ToolProfileFocusedSourceCode:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactRead, agenttools.OperationWorkspaceInspect,
			agenttools.OperationArtifactWrite, agenttools.OperationRuntimeContextRead,
			agenttools.OperationExecution:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	case ToolProfileFocusedAnalysisAuthoring:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationWorkspaceInspect, agenttools.OperationArtifactRead,
			agenttools.OperationArtifactWrite, agenttools.OperationRuntimeContextRead,
			agenttools.OperationExecution:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	case ToolProfileFocusedAuthoring:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactWrite, agenttools.OperationArtifactRead, agenttools.OperationRuntimeContextRead:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	case ToolProfileFocusedInspection:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationWorkspaceInspect, agenttools.OperationArtifactRead, agenttools.OperationRuntimeContextRead:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	case ToolProfileFocusedScheduling:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationScheduling, agenttools.OperationRuntimeContextRead:
			return true
		case agenttools.OperationTaskInspection:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func looksLikeSchedulingTask(lower string) bool {
	markers := []string{
		"schedule ", "scheduled", "cron", "every ", "reminder",
		"quiet ticker", "ticker", "runs every",
	}
	return containsAnyMarker(lower, markers)
}

func directExecutionPolicyReason(requireArtifactWrite, requiresInspection, requiresAnalysisAuthoring bool) string {
	switch {
	case requiresAnalysisAuthoring:
		return "inspection-backed report authoring should stay on a focused analysis+authoring envelope"
	case requireArtifactWrite:
		return "direct artifact authoring should stay on a focused execution envelope"
	case requiresInspection:
		return "direct filesystem inspection should stay on a focused inspection envelope"
	default:
		return "simple direct task should stay on a focused execution envelope"
	}
}

func requiresExplicitAuthorityMutation(content string) bool {
	lower := strings.ToLower(content)
	persistenceMarkers := []string{
		"ingest", "record", "store", "save", "capture", "persist", "promote", "register",
	}
	authorityTargetMarkers := []string{
		"canonical memory",
		"semantic memory",
		"policy store",
		"instruction registry",
		"skill registry",
		"authority layer",
		"knowledge base",
	}
	return containsAnyMarker(lower, persistenceMarkers) && containsAnyMarker(lower, authorityTargetMarkers)
}
