package llm

import (
	"testing"

	"roboticus/internal/db"
)

func TestNewService_NoProviders(t *testing.T) {
	svc, err := NewService(ServiceConfig{}, nil)
	if err != nil {
		t.Fatalf("should succeed: %v", err)
	}
	if svc == nil {
		t.Fatal("nil")
	}
}

func TestService_Status_Empty(t *testing.T) {
	svc, _ := NewService(ServiceConfig{}, nil)
	if len(svc.Status()) != 0 {
		t.Error("empty service should have 0 statuses")
	}
}

func TestService_Escalation_Init(t *testing.T) {
	svc, _ := NewService(ServiceConfig{}, nil)
	if svc.Escalation == nil {
		t.Fatal("nil")
	}
	stats := svc.Escalation.Stats()
	if stats["cache_hits"] != 0 {
		t.Error("should start at 0")
	}
}

func TestService_Confidence_Init(t *testing.T) {
	svc, _ := NewService(ServiceConfig{ConfidenceFloor: 0.8}, nil)
	if svc.Confidence == nil {
		t.Fatal("nil")
	}
}

func TestResolveProviderChain_IncludesPrimaryAndFallbacks(t *testing.T) {
	svc, _ := NewService(ServiceConfig{
		Primary:   "openai",
		Fallbacks: []string{"anthropic", "ollama"},
		Providers: []Provider{
			{Name: "openai", URL: "http://test", Format: FormatOpenAI},
			{Name: "anthropic", URL: "http://test", Format: FormatAnthropic},
			{Name: "ollama", URL: "http://test", Format: FormatOpenAI, IsLocal: true},
		},
	}, nil)
	chain := svc.resolveProviderChain("openai")
	hasPrimary, hasFallback := false, false
	for _, p := range chain {
		if p == "openai" {
			hasPrimary = true
		}
		if p == "anthropic" {
			hasFallback = true
		}
	}
	if !hasPrimary {
		t.Error("chain should include primary")
	}
	if !hasFallback {
		t.Error("chain should include fallbacks")
	}
}

func TestSplitModelSpec(t *testing.T) {
	tests := []struct {
		input    string
		provider string
		model    string
	}{
		{"ollama/qwen3.5:35b-a3b", "ollama", "qwen3.5:35b-a3b"},
		{"openrouter/openai/gpt-4o-mini", "openrouter", "openai/gpt-4o-mini"},
		{"anthropic", "anthropic", ""},
		{"gpt-4", "gpt-4", ""},
	}
	for _, tc := range tests {
		p, m := splitModelSpec(tc.input)
		if p != tc.provider || m != tc.model {
			t.Errorf("splitModelSpec(%q) = (%q, %q), want (%q, %q)", tc.input, p, m, tc.provider, tc.model)
		}
	}
}

func TestContains_Util(t *testing.T) {
	if !contains([]string{"a", "b", "c"}, "b") {
		t.Error("should find b")
	}
	if contains([]string{"a"}, "z") {
		t.Error("should not find z")
	}
}

func TestOrderedRoutingSpecs_PreservesMultipleModelsPerProvider(t *testing.T) {
	got := orderedRoutingSpecs(
		"moonshot/kimi-k2-turbo-preview",
		[]string{
			"ollama/qwen2.5:32b",
			"ollama/gemma4",
			"openrouter/openai/gpt-4o-mini",
			"ollama/gemma3:12b",
			"ollama/gemma4",
		},
	)

	if len(got["ollama"]) != 3 {
		t.Fatalf("ollama targets = %v, want 3 distinct models", got["ollama"])
	}
	want := []string{"qwen2.5:32b", "gemma4", "gemma3:12b"}
	for i, model := range want {
		if got["ollama"][i] != model {
			t.Fatalf("ollama target[%d] = %q, want %q", i, got["ollama"][i], model)
		}
	}
	if len(got["moonshot"]) != 1 || got["moonshot"][0] != "kimi-k2-turbo-preview" {
		t.Fatalf("moonshot targets = %v, want [kimi-k2-turbo-preview]", got["moonshot"])
	}
}

func TestNewService_BuildsRoutingTargetsPerOrderedModel(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		Primary: "moonshot/kimi-k2-turbo-preview",
		Fallbacks: []string{
			"ollama/qwen2.5:32b",
			"ollama/gemma4",
			"openrouter/openai/gpt-4o-mini",
			"ollama/gemma3:12b",
		},
		Providers: []Provider{
			{Name: "moonshot", URL: "http://test", Format: FormatOpenAIResponses},
			{Name: "ollama", URL: "http://test", Format: FormatOllama, IsLocal: true},
			{Name: "openrouter", URL: "http://test", Format: FormatOpenAI},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	targets := svc.router.Targets()
	if len(targets) != 5 {
		t.Fatalf("routing targets = %d, want 5", len(targets))
	}

	want := []RouteTarget{
		{Model: "kimi-k2-turbo-preview", Provider: "moonshot"},
		{Model: "qwen2.5:32b", Provider: "ollama"},
		{Model: "gemma4", Provider: "ollama"},
		{Model: "openai/gpt-4o-mini", Provider: "openrouter"},
		{Model: "gemma3:12b", Provider: "ollama"},
	}
	for _, expected := range want {
		found := false
		for _, target := range targets {
			if target.Model == expected.Model && target.Provider == expected.Provider {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing routing target %s/%s in %+v", expected.Provider, expected.Model, targets)
		}
	}
}

func TestNewService_AppliesConfiguredModelPolicyToTargets(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		Primary: "ollama/phi4-mini:latest",
		Providers: []Provider{
			{Name: "ollama", URL: "http://test", Format: FormatOllama, IsLocal: true},
		},
		Policies: map[string]ModelPolicy{
			"phi4-mini:latest": {
				State:             ModelStateNiche,
				PrimaryReasonCode: "user_preference",
				ReasonCodes:       []string{"user_preference", "latency_nonviable"},
				HumanReason:       "Keep this model only for latency-tolerant local work.",
				EvidenceRefs:      []string{"baseline:run-1", "incident:turn-42"},
				Source:            "configured_policy",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	targets := svc.router.Targets()
	if len(targets) != 1 {
		t.Fatalf("routing targets = %d, want 1", len(targets))
	}
	target := targets[0]
	if target.State != ModelStateNiche {
		t.Fatalf("target.State = %q, want %q", target.State, ModelStateNiche)
	}
	if target.PrimaryReasonCode != "user_preference" {
		t.Fatalf("target.PrimaryReasonCode = %q", target.PrimaryReasonCode)
	}
	if target.PolicySource != "configured_policy" {
		t.Fatalf("target.PolicySource = %q", target.PolicySource)
	}
	if len(target.ReasonCodes) != 2 {
		t.Fatalf("target.ReasonCodes = %v", target.ReasonCodes)
	}
	if len(target.EvidenceRefs) != 2 {
		t.Fatalf("target.EvidenceRefs = %v", target.EvidenceRefs)
	}
}

func TestModelPoliciesFromRows_NormalizesProviderQualifiedKeys(t *testing.T) {
	policies := ModelPoliciesFromRows([]db.ModelPolicyRow{{
		Model:             "ollama/qwen2.5:32b",
		State:             ModelStateDisabled,
		PrimaryReasonCode: "latency_nonviable",
		Source:            "persisted_policy",
	}})

	policy, ok := policies["qwen2.5:32b"]
	if !ok {
		t.Fatalf("normalized policy missing: %v", policies)
	}
	if policy.State != ModelStateDisabled {
		t.Fatalf("state = %q, want disabled", policy.State)
	}
}
