package llm

import "testing"

func TestEffectiveModelPolicy_DefaultsToEnabled(t *testing.T) {
	decisions := EffectiveModelPolicy([]string{"ollama/gemma4"}, nil)
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].State != ModelStateEnabled {
		t.Fatalf("state = %q, want enabled", decisions[0].State)
	}
	if !decisions[0].LiveRoutable {
		t.Fatal("default enabled model should be live routable")
	}
	if decisions[0].Source != "default" {
		t.Fatalf("source = %q, want default", decisions[0].Source)
	}
}

func TestEffectiveModelPolicy_UsesConfiguredDecision(t *testing.T) {
	decisions := EffectiveModelPolicy([]string{"ollama/qwen2.5:32b"}, map[string]ModelPolicy{
		"qwen2.5:32b": {
			State:             ModelStateDisabled,
			PrimaryReasonCode: "latency_nonviable",
			ReasonCodes:       []string{"latency_nonviable", "provider_instability"},
			HumanReason:       "Disable on this hardware.",
			EvidenceRefs:      []string{"baseline:run-2"},
			Source:            "configured_policy",
		},
	})
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].State != ModelStateDisabled {
		t.Fatalf("state = %q, want disabled", decisions[0].State)
	}
	if decisions[0].LiveRoutable {
		t.Fatal("disabled model should not be live routable")
	}
	if decisions[0].PrimaryReasonCode != "latency_nonviable" {
		t.Fatalf("primary reason = %q", decisions[0].PrimaryReasonCode)
	}
	if decisions[0].Source != "configured_policy" {
		t.Fatalf("source = %q, want configured_policy", decisions[0].Source)
	}
}

func TestEffectiveModelPolicy_UsesProviderQualifiedConfiguredDecision(t *testing.T) {
	decisions := EffectiveModelPolicy([]string{"ollama/qwen2.5:32b"}, map[string]ModelPolicy{
		"ollama/qwen2.5:32b": {
			State:             ModelStateDisabled,
			PrimaryReasonCode: "latency_nonviable",
			HumanReason:       "Disabled on this hardware.",
			Source:            "persisted_policy",
		},
	})
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].State != ModelStateDisabled {
		t.Fatalf("state = %q, want disabled", decisions[0].State)
	}
	if decisions[0].BenchmarkEligible {
		t.Fatal("disabled model should not be benchmark eligible")
	}
	if decisions[0].Source != "persisted_policy" {
		t.Fatalf("source = %q, want persisted_policy", decisions[0].Source)
	}
}

func TestRouterSelect_OrchestratorExcludesSubagentOnlyModels(t *testing.T) {
	router := NewRouter([]RouteTarget{
		{
			Model:                "qwen2.5-coder:14b",
			Provider:             "ollama",
			Tier:                 TierMedium,
			OrchestratorEligible: false,
			SubagentEligible:     true,
			EligibilityReason:    "coding_specialist_subagent_only",
		},
		{
			Model:                "kimi-k2-turbo-preview",
			Provider:             "moonshot",
			Tier:                 TierMedium,
			OrchestratorEligible: true,
			SubagentEligible:     true,
			EligibilityReason:    "generalist_default",
		},
	}, RouterConfig{})

	got := router.Select(&Request{
		AgentRole:      "orchestrator",
		TurnWeight:     "standard",
		TaskIntent:     "code",
		TaskComplexity: "moderate",
		Messages:       []Message{{Role: "user", Content: "Review this patch and summarize the risks."}},
	})

	if got.Model != "kimi-k2-turbo-preview" {
		t.Fatalf("selected model = %q, want generalist orchestrator model", got.Model)
	}
}

func TestRouterSelect_SubagentCanUseSubagentOnlyModels(t *testing.T) {
	router := NewRouter([]RouteTarget{
		{
			Model:                "qwen2.5-coder:14b",
			Provider:             "ollama",
			Tier:                 TierMedium,
			OrchestratorEligible: false,
			SubagentEligible:     true,
			EligibilityReason:    "coding_specialist_subagent_only",
		},
		{
			Model:                "kimi-k2-turbo-preview",
			Provider:             "moonshot",
			Tier:                 TierMedium,
			OrchestratorEligible: true,
			SubagentEligible:     true,
			EligibilityReason:    "generalist_default",
		},
	}, RouterConfig{})

	got := router.Select(&Request{
		AgentRole:      "subagent",
		TurnWeight:     "standard",
		TaskIntent:     "code",
		TaskComplexity: "moderate",
		Messages:       []Message{{Role: "user", Content: "Run the bounded code review task and report findings."}},
	})

	if got.Model != "qwen2.5-coder:14b" {
		t.Fatalf("selected model = %q, want subagent-specialist model", got.Model)
	}
}

func TestInferRouteTargetEligibility_CoderIsSubagentOnly(t *testing.T) {
	orchestratorEligible, subagentEligible, reason := inferRouteTargetEligibility("qwen2.5-coder:14b", nil)
	if orchestratorEligible {
		t.Fatal("coder model should not be orchestrator eligible by default")
	}
	if !subagentEligible {
		t.Fatal("coder model should remain subagent eligible")
	}
	if reason != "coding_specialist_subagent_only" {
		t.Fatalf("reason = %q, want coding_specialist_subagent_only", reason)
	}
}

func TestRouterSelect_DisabledModelExcludedFromLiveRouting(t *testing.T) {
	router := NewRouter([]RouteTarget{
		{
			Model:                "phi4-mini:latest",
			Provider:             "ollama",
			Tier:                 TierMedium,
			State:                ModelStateDisabled,
			PrimaryReasonCode:    "quality_nonviable",
			OrchestratorEligible: true,
			SubagentEligible:     true,
		},
		{
			Model:                "kimi-k2-turbo-preview",
			Provider:             "moonshot",
			Tier:                 TierMedium,
			State:                ModelStateEnabled,
			OrchestratorEligible: true,
			SubagentEligible:     true,
		},
	}, RouterConfig{})

	got := router.Select(&Request{
		AgentRole:      "orchestrator",
		TurnWeight:     "standard",
		TaskIntent:     "analysis",
		TaskComplexity: "moderate",
		Messages:       []Message{{Role: "user", Content: "Summarize the tradeoffs in this design."}},
	})

	if got.Model != "kimi-k2-turbo-preview" {
		t.Fatalf("selected model = %q, want enabled model", got.Model)
	}
}

func TestRouterSelect_BenchmarkOnlyModelExcludedFromLiveRouting(t *testing.T) {
	router := NewRouter([]RouteTarget{
		{
			Model:                "gemma3:12b",
			Provider:             "ollama",
			Tier:                 TierMedium,
			State:                ModelStateBenchmarkOnly,
			PrimaryReasonCode:    "benchmark_only_by_policy",
			OrchestratorEligible: true,
			SubagentEligible:     true,
		},
		{
			Model:                "kimi-k2-turbo-preview",
			Provider:             "moonshot",
			Tier:                 TierMedium,
			State:                ModelStateEnabled,
			OrchestratorEligible: true,
			SubagentEligible:     true,
		},
	}, RouterConfig{})

	got := router.Select(&Request{
		AgentRole:      "orchestrator",
		TurnWeight:     "standard",
		TaskIntent:     "analysis",
		TaskComplexity: "moderate",
		Messages:       []Message{{Role: "user", Content: "Summarize the tradeoffs in this design."}},
	})

	if got.Model != "kimi-k2-turbo-preview" {
		t.Fatalf("selected model = %q, want enabled model", got.Model)
	}
}
