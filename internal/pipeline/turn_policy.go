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
	ToolProfileFocusedWebRead           ToolProfile = "focused_web_read"
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
	requiresWebRead := looksLikePublicWebReadTurn(content)

	switch {
	case requiresWebRead:
		return TurnEnvelopePolicy{
			Weight:              TurnWeightStandard,
			ContextBudget:       2048,
			AllowRetrieval:      false,
			MaxTools:            6,
			AllowRetryExpansion: true,
			ToolProfile:         ToolProfileFocusedWebRead,
			Reason:              "public web/browser request should use a focused web-read tool envelope",
		}
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
		(synthesis.Complexity == "simple" || requiresArtifactWrite || requiresInspection || requiresAnalysisAuthoring):
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
	case synthesis.Complexity == "complex" || synthesis.Intent == "code":
		return TurnEnvelopePolicy{
			Weight:              TurnWeightHeavy,
			ContextBudget:       defaultTokenBudget,
			AllowRetrieval:      true,
			AllowRetryExpansion: false,
			Reason:              "complex or action-oriented turn requires the full adaptive envelope",
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

// AlwaysIncluder is the optional interface a ToolPruner may implement to
// declare which tool names are operator-pinned for a given session. The
// pin set survives both `filterToolDefsForPolicy` (operation-class admit
// list) and `MaxTools` truncation: a pinned name that the upstream pruner
// returned must always reach the turn surface, regardless of policy
// admission or truncation order.
//
// Authority-mutation pins are still subject to `policy.AllowAuthorityMutation`
// — pinning a name that mutates the authority layer when the turn does not
// allow it is treated as an operator misconfiguration and the pin is
// honored in the loop's `OperationAuthorityWrite` filter rather than at
// admission time.
type AlwaysIncluder interface {
	AlwaysIncluded(session *Session) []string
}

func (p TurnEnvelopePolicy) applyToolPolicy(ctx context.Context, session *Session, pruner ToolPruner) (agenttools.ToolSearchStats, error) {
	if session == nil {
		return agenttools.ToolSearchStats{}, nil
	}
	pinSet := pinSetFromPruner(pruner, session)
	if p.LightweightToolSurface && len(pinSet) == 0 {
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

	var filtered int
	if p.ToolProfile == ToolProfileFocusedWebRead {
		defs, filtered = filterToolDefsForFocusedWebRead(defs)
	} else {
		defs, filtered = filterToolDefsForPolicy(defs, p, pinSet)
	}
	if filtered > 0 {
		stats.CandidatesPruned += filtered
	}
	stats.CandidatesSelected = len(defs)
	if p.MaxTools > 0 && len(defs) > p.MaxTools {
		defs = truncateRespectingPins(defs, p.MaxTools, pinSet)
		if dropped := stats.CandidatesSelected - len(defs); dropped > 0 {
			stats.CandidatesPruned += dropped
		}
		stats.CandidatesSelected = len(defs)
	}
	session.SetSelectedToolDefs(defs)
	return stats, nil
}

func filterToolDefsForFocusedWebRead(defs []llm.ToolDef) ([]llm.ToolDef, int) {
	if len(defs) == 0 {
		return defs, 0
	}
	filtered := make([]llm.ToolDef, 0, len(defs))
	removed := 0
	for _, def := range defs {
		if agenttools.OperationClassForName(def.Function.Name) == agenttools.OperationWebRead {
			filtered = append(filtered, def)
			continue
		}
		removed++
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return toolPriorityForPolicy(filtered[i].Function.Name, TurnEnvelopePolicy{ToolProfile: ToolProfileFocusedWebRead}) <
			toolPriorityForPolicy(filtered[j].Function.Name, TurnEnvelopePolicy{ToolProfile: ToolProfileFocusedWebRead})
	})
	return filtered, removed
}

func pinSetFromPruner(pruner ToolPruner, session *Session) map[string]struct{} {
	pinned := make(map[string]struct{})
	if includer, ok := pruner.(AlwaysIncluder); ok {
		for _, raw := range includer.AlwaysIncluded(session) {
			name := strings.ToLower(strings.TrimSpace(raw))
			if name != "" {
				pinned[name] = struct{}{}
			}
		}
	}
	return pinned
}

// truncateRespectingPins keeps the first MaxTools entries while ensuring
// every pinned name that survived policy filtering remains in the result.
// The pruner already ordered pins ahead of unpinned tools, but a downstream
// re-sort (toolPriorityForPolicy) can interleave them. Pinned names that
// fall outside the head of the slice replace the lowest-priority unpinned
// tail entries.
func truncateRespectingPins(defs []llm.ToolDef, maxTools int, pinSet map[string]struct{}) []llm.ToolDef {
	if maxTools <= 0 || len(defs) <= maxTools {
		return defs
	}
	if len(pinSet) == 0 {
		return defs[:maxTools]
	}

	head := append([]llm.ToolDef(nil), defs[:maxTools]...)
	headHasPin := func(name string) bool {
		for _, def := range head {
			if strings.EqualFold(strings.TrimSpace(def.Function.Name), name) {
				return true
			}
		}
		return false
	}

	for _, def := range defs[maxTools:] {
		name := strings.ToLower(strings.TrimSpace(def.Function.Name))
		if name == "" {
			continue
		}
		if _, pinned := pinSet[name]; !pinned {
			continue
		}
		if headHasPin(name) {
			continue
		}
		// Replace the last non-pinned slot in the head with the pinned tool.
		replaced := false
		for i := len(head) - 1; i >= 0; i-- {
			candidate := strings.ToLower(strings.TrimSpace(head[i].Function.Name))
			if _, isPin := pinSet[candidate]; isPin {
				continue
			}
			head[i] = def
			replaced = true
			break
		}
		if !replaced {
			// All head slots are already pinned; widen the surface so the
			// pin is preserved rather than silently dropped. This is
			// preferable to violating pin semantics.
			head = append(head, def)
		}
	}
	return head
}

func filterToolDefsForPolicy(defs []llm.ToolDef, policy TurnEnvelopePolicy, pinSet map[string]struct{}) ([]llm.ToolDef, int) {
	if len(defs) == 0 {
		return defs, 0
	}

	filtered := make([]llm.ToolDef, 0, len(defs))
	removed := 0
	for _, def := range defs {
		name := strings.TrimSpace(def.Function.Name)
		lower := strings.ToLower(name)
		_, pinned := pinSet[lower]
		if name == "" {
			filtered = append(filtered, def)
			continue
		}
		// Authority-layer mutation is the one admission rule that overrides
		// pinning: if the operator pinned an authority-mutating tool on a
		// turn that explicitly does not allow it, the pin loses. Everything
		// else respects the pin.
		if policy.RequireArtifactWrite && !policy.AllowAuthorityMutation && agenttools.MutatesAuthorityLayer(name) {
			removed++
			continue
		}
		if !toolAllowedForPolicy(name, policy) && !pinned {
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
	if policy.ToolProfile == ToolProfileFocusedWebRead {
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationWebRead:
			return 0
		case agenttools.OperationMemoryRead:
			return 1
		default:
			return 2
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
			agenttools.OperationExecution,
			agenttools.OperationDataRead:
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
			agenttools.OperationExecution,
			agenttools.OperationDataRead, agenttools.OperationWebRead,
			agenttools.OperationCapabilityInventory, agenttools.OperationInspection:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	case ToolProfileFocusedAuthoring:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationArtifactWrite, agenttools.OperationArtifactRead,
			agenttools.OperationRuntimeContextRead,
			agenttools.OperationDataRead, agenttools.OperationDataWrite:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	case ToolProfileFocusedInspection:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationWorkspaceInspect, agenttools.OperationArtifactRead,
			agenttools.OperationRuntimeContextRead,
			agenttools.OperationDataRead, agenttools.OperationWebRead,
			agenttools.OperationInspection, agenttools.OperationCapabilityInventory,
			agenttools.OperationTaskInspection:
			return true
		case agenttools.OperationMemoryRead:
			return policy.AllowRetrieval
		default:
			return false
		}
	case ToolProfileFocusedScheduling:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationScheduling, agenttools.OperationRuntimeContextRead,
			agenttools.OperationTaskInspection,
			agenttools.OperationCapabilityInventory,
			agenttools.OperationDelegation:
			return true
		default:
			return false
		}
	case ToolProfileFocusedWebRead:
		switch agenttools.OperationClassForName(name) {
		case agenttools.OperationWebRead:
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

func looksLikePublicWebReadTurn(content string) bool {
	normalized := strings.ToLower(content)
	if strings.TrimSpace(normalized) == "" {
		return false
	}
	if strings.Contains(normalized, "http://") ||
		strings.Contains(normalized, "https://") ||
		strings.Contains(normalized, "www.") {
		return true
	}
	webSubjects := []string{"website", "web site", "webpage", "web page", "page", "site", "url", "metacritic", "browser", "playwright"}
	webActions := []string{"fetch", "pull", "open", "browse", "surf", "navigate", "visit", "read", "search", "look up", "lookup", "find", "latest", "current", "use"}
	hasSubject := false
	for _, subject := range webSubjects {
		if strings.Contains(normalized, subject) {
			hasSubject = true
			break
		}
	}
	if !hasSubject {
		return false
	}
	for _, action := range webActions {
		if strings.Contains(normalized, action) {
			return true
		}
	}
	return false
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
