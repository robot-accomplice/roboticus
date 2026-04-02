package llm

import "testing"

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
