package llm

import (
	"context"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

func ModelPoliciesFromCore(cfg map[string]core.ModelPolicyConfig) map[string]ModelPolicy {
	out := make(map[string]ModelPolicy, len(cfg))
	for model, policy := range cfg {
		out[normalizeEligibilityModelSpec(model)] = ModelPolicy{
			State:             policy.State,
			PrimaryReasonCode: policy.PrimaryReasonCode,
			ReasonCodes:       append([]string(nil), policy.ReasonCodes...),
			HumanReason:       policy.HumanReason,
			EvidenceRefs:      append([]string(nil), policy.EvidenceRefs...),
			Source:            policy.Source,
		}
	}
	return out
}

func ModelPoliciesFromRows(rows []db.ModelPolicyRow) map[string]ModelPolicy {
	out := make(map[string]ModelPolicy, len(rows))
	for _, row := range rows {
		out[normalizeEligibilityModelSpec(row.Model)] = ModelPolicy{
			State:             row.State,
			PrimaryReasonCode: row.PrimaryReasonCode,
			ReasonCodes:       append([]string(nil), row.ReasonCodes...),
			HumanReason:       row.HumanReason,
			EvidenceRefs:      append([]string(nil), row.EvidenceRefs...),
			Source:            row.Source,
		}
	}
	return out
}

func EffectiveModelPolicies(ctx context.Context, store *db.Store, cfg map[string]core.ModelPolicyConfig) map[string]ModelPolicy {
	base := ModelPoliciesFromCore(cfg)
	if store == nil {
		return base
	}
	persisted := ModelPoliciesFromRows(db.ListModelPolicies(ctx, store))
	return MergeModelPolicies(base, persisted)
}
