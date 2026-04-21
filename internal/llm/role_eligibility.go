package llm

import "strings"

type RoleEligibilityDecision struct {
	Model                string `json:"model"`
	OrchestratorEligible bool   `json:"orchestrator_eligible"`
	SubagentEligible     bool   `json:"subagent_eligible"`
	Reason               string `json:"reason,omitempty"`
	Source               string `json:"source"`
}

// EffectiveRoleEligibility returns the routing role decision for each model spec
// in evaluation order, combining explicit overrides with default heuristics.
func EffectiveRoleEligibility(specs []string, overrides map[string]RoleEligibility) []RoleEligibilityDecision {
	decisions := make([]RoleEligibilityDecision, 0, len(specs))
	for _, spec := range specs {
		model := normalizeEligibilityModelSpec(spec)
		orchestratorEligible, subagentEligible, reason := inferRouteTargetEligibility(model, overrides)
		source := "heuristic"
		if _, ok := lookupRoleEligibilityOverride(overrides, model); ok {
			source = "configured_override"
		}
		decisions = append(decisions, RoleEligibilityDecision{
			Model:                model,
			OrchestratorEligible: orchestratorEligible,
			SubagentEligible:     subagentEligible,
			Reason:               reason,
			Source:               source,
		})
	}
	return decisions
}

func normalizeEligibilityModelSpec(spec string) string {
	_, model := splitModelSpec(spec)
	if model != "" {
		return model
	}
	return strings.TrimSpace(spec)
}
