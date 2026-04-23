package llm

import (
	"testing"
)

func TestEstimateComplexity_SimpleGreeting(t *testing.T) {
	req := &Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	c := estimateComplexity(req)
	if c >= 0.2 {
		t.Errorf("simple greeting should be low complexity, got %f", c)
	}
}

func TestEstimateComplexity_LongWithTools(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Please analyze this codebase and refactor the authentication module to use OAuth2 instead of session tokens. Consider the security implications and performance trade-offs."},
		},
		Tools: []ToolDef{
			{Function: ToolFuncDef{Name: "read_file"}},
			{Function: ToolFuncDef{Name: "write_file"}},
			{Function: ToolFuncDef{Name: "search"}},
			{Function: ToolFuncDef{Name: "run_tests"}},
			{Function: ToolFuncDef{Name: "git_diff"}},
			{Function: ToolFuncDef{Name: "lint"}},
		},
	}
	c := estimateComplexity(req)
	if c < 0.3 {
		t.Errorf("complex request with tools should be high complexity, got %f", c)
	}
}

func TestTierForComplexity(t *testing.T) {
	tests := []struct {
		complexity Complexity
		want       ModelTier
	}{
		{0.0, TierSmall},
		{0.1, TierSmall},
		{0.2, TierMedium},
		{0.4, TierLarge},
		{0.7, TierFrontier},
		{1.0, TierFrontier},
	}
	for _, tt := range tests {
		got := tierForComplexity(tt.complexity)
		if got != tt.want {
			t.Errorf("tierForComplexity(%f) = %d, want %d", tt.complexity, got, tt.want)
		}
	}
}

func TestRouter_SelectsCorrectTier(t *testing.T) {
	targets := []RouteTarget{
		{Model: "gpt-4o-mini", Provider: "openai", Tier: TierSmall, Cost: 0.001},
		{Model: "gpt-4o", Provider: "openai", Tier: TierLarge, Cost: 0.01},
		{Model: "claude-opus", Provider: "anthropic", Tier: TierFrontier, Cost: 0.05},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true})

	// Simple request should route to small model.
	simple := &Request{Messages: []Message{{Role: "user", Content: "hi"}}}
	selected := router.Select(simple)
	if selected.Model != "gpt-4o-mini" {
		t.Errorf("simple request should route to small model, got %s", selected.Model)
	}
}

func TestRouter_FallsUpward(t *testing.T) {
	targets := []RouteTarget{
		{Model: "big-model", Provider: "provider-a", Tier: TierFrontier, Cost: 0.05},
	}
	router := NewRouter(targets, RouterConfig{})

	// Even a simple request should get the frontier model if it's all we have.
	req := &Request{Messages: []Message{{Role: "user", Content: "hi"}}}
	selected := router.Select(req)
	if selected.Model != "big-model" {
		t.Errorf("should fall upward to available model, got %s", selected.Model)
	}
}

func TestFilterProfilesForCapabilityEvidence_ToolBearingPrefersObservedToolUse(t *testing.T) {
	profiles := []ModelProfile{
		{Model: "under-evidenced", CapabilityEvidence: "unproven_for_intent"},
		{Model: "observed", CapabilityEvidence: "observed_for_intent"},
		{Model: "unexercised", CapabilityEvidence: "unexercised"},
	}
	req := &Request{Tools: []ToolDef{{Function: ToolFuncDef{Name: "list_tools"}}}}
	got := filterProfilesForCapabilityEvidence(profiles, req)
	if len(got) != 1 || got[0].Model != "observed" {
		t.Fatalf("filtered profiles = %#v, want only observed candidate", got)
	}
}

func TestFilterProfilesForCapabilityEvidence_NoObservedToolUseKeepsCandidates(t *testing.T) {
	profiles := []ModelProfile{
		{Model: "under-evidenced", CapabilityEvidence: "unproven_for_intent"},
		{Model: "unexercised", CapabilityEvidence: "unexercised"},
	}
	req := &Request{Tools: []ToolDef{{Function: ToolFuncDef{Name: "list_tools"}}}}
	got := filterProfilesForCapabilityEvidence(profiles, req)
	if len(got) != len(profiles) {
		t.Fatalf("len(filtered) = %d, want %d", len(got), len(profiles))
	}
}
