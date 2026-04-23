package llm

import "strings"

const (
	ModelStateEnabled       = "enabled"
	ModelStateNiche         = "niche"
	ModelStateDisabled      = "disabled"
	ModelStateBenchmarkOnly = "benchmark_only"
)

type ModelPolicy struct {
	State             string   `json:"state"`
	PrimaryReasonCode string   `json:"primary_reason_code,omitempty"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
	HumanReason       string   `json:"human_reason,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	Source            string   `json:"source,omitempty"`
}

type ModelPolicyDecision struct {
	Model             string   `json:"model"`
	State             string   `json:"state"`
	LiveRoutable      bool     `json:"live_routable"`
	BenchmarkEligible bool     `json:"benchmark_eligible"`
	PrimaryReasonCode string   `json:"primary_reason_code,omitempty"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
	HumanReason       string   `json:"human_reason,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	Source            string   `json:"source"`
}

func normalizeModelState(state string) string {
	switch strings.TrimSpace(strings.ToLower(state)) {
	case ModelStateNiche:
		return ModelStateNiche
	case ModelStateDisabled:
		return ModelStateDisabled
	case ModelStateBenchmarkOnly:
		return ModelStateBenchmarkOnly
	default:
		return ModelStateEnabled
	}
}

func liveRoutingAllowedForState(state string) bool {
	switch normalizeModelState(state) {
	case ModelStateDisabled, ModelStateBenchmarkOnly:
		return false
	default:
		return true
	}
}

func benchmarkAllowedForState(state string) bool {
	switch normalizeModelState(state) {
	case ModelStateDisabled:
		return false
	default:
		return true
	}
}

func BenchmarkAllowedForState(state string) bool {
	return benchmarkAllowedForState(state)
}

func lookupModelPolicy(policies map[string]ModelPolicy, model string) (ModelPolicy, bool) {
	if len(policies) == 0 {
		return ModelPolicy{}, false
	}
	normalized := normalizeEligibilityModelSpec(model)
	keys := []string{
		strings.TrimSpace(model),
		strings.ToLower(strings.TrimSpace(model)),
		normalized,
		strings.ToLower(normalized),
	}
	for _, key := range keys {
		if policy, ok := policies[key]; ok {
			policy.State = normalizeModelState(policy.State)
			return policy, true
		}
	}
	return ModelPolicy{}, false
}

func effectiveModelPolicy(model string, policies map[string]ModelPolicy) ModelPolicy {
	if policy, ok := lookupModelPolicy(policies, model); ok {
		return policy
	}
	return ModelPolicy{State: ModelStateEnabled}
}

func MergeModelPolicies(base, overrides map[string]ModelPolicy) map[string]ModelPolicy {
	out := make(map[string]ModelPolicy, len(base)+len(overrides))
	for model, policy := range base {
		cloned := policy
		cloned.State = normalizeModelState(cloned.State)
		if strings.TrimSpace(cloned.Source) == "" {
			cloned.Source = "configured_policy"
		}
		out[model] = cloned
	}
	for model, policy := range overrides {
		cloned := policy
		cloned.State = normalizeModelState(cloned.State)
		if strings.TrimSpace(cloned.Source) == "" {
			cloned.Source = "persisted_policy"
		}
		out[model] = cloned
	}
	return out
}

func EffectiveModelPolicy(specs []string, policies map[string]ModelPolicy) []ModelPolicyDecision {
	decisions := make([]ModelPolicyDecision, 0, len(specs))
	for _, spec := range specs {
		model := normalizeEligibilityModelSpec(spec)
		policy, ok := lookupModelPolicy(policies, spec)
		source := "default"
		if ok {
			source = strings.TrimSpace(policy.Source)
			if source == "" {
				source = "configured_policy"
			}
		} else {
			policy = ModelPolicy{State: ModelStateEnabled}
		}
		decisions = append(decisions, ModelPolicyDecision{
			Model:             model,
			State:             normalizeModelState(policy.State),
			LiveRoutable:      liveRoutingAllowedForState(policy.State),
			BenchmarkEligible: benchmarkAllowedForState(policy.State),
			PrimaryReasonCode: policy.PrimaryReasonCode,
			ReasonCodes:       append([]string(nil), policy.ReasonCodes...),
			HumanReason:       policy.HumanReason,
			EvidenceRefs:      append([]string(nil), policy.EvidenceRefs...),
			Source:            source,
		})
	}
	return decisions
}
