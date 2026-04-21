package llm

import (
	"context"
	"testing"
)

type traceCapture struct {
	values map[string]any
}

func (t *traceCapture) Annotate(key string, value any) {
	if t.values == nil {
		t.values = map[string]any{}
	}
	t.values[key] = value
}

func TestServiceComplete_AnnotatesRoutingFromActualRequest(t *testing.T) {
	client, _ := NewClientWithHTTP(&Provider{
		Name: "cloud-model", URL: "http://cloud", Format: FormatOpenAI,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"cloud","model":"cloud-model","choices":[{"message":{"content":"cloud response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":30}}`,
	})

	svc, err := NewService(ServiceConfig{
		Primary: "cloud-model/cloud-model",
		Providers: []Provider{
			{Name: "cloud-model", URL: "http://cloud", Format: FormatOpenAI},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.providers["cloud-model"] = client

	tr := &traceCapture{}
	ctx := WithRoutingTracer(context.Background(), tr)
	req := &Request{
		AgentRole: "orchestrator",
		Messages: []Message{
			{Role: "system", Content: "system context"},
			{Role: "assistant", Content: "prior assistant turn"},
			{Role: "user", Content: "please analyze this request"},
		},
		Tools: []ToolDef{
			{Type: "function", Function: ToolFuncDef{Name: "echo", Description: "Echo"}},
		},
	}

	if _, err := svc.Complete(ctx, req); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if got := tr.values["inference.routing.trace_source"]; got != "actual_request" {
		t.Fatalf("routing.trace_source = %#v want actual_request", got)
	}
	if got := tr.values["inference.routing.request_message_count"]; got != 3 {
		t.Fatalf("routing.request_message_count = %#v want 3", got)
	}
	if got := tr.values["inference.routing.request_tool_count"]; got != 1 {
		t.Fatalf("routing.request_tool_count = %#v want 1", got)
	}
	if got := tr.values["inference.routing.agent_role"]; got != "orchestrator" {
		t.Fatalf("routing.agent_role = %#v want orchestrator", got)
	}
	if got := tr.values["inference.routing.winner"]; got != "cloud-model/cloud-model" {
		t.Fatalf("routing.winner = %#v want cloud-model/cloud-model", got)
	}
}

func TestAnnotateRoutingDecision_NoTracerNoop(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		Primary: "p1/m1",
		Providers: []Provider{
			{Name: "p1", URL: "http://p1", Format: FormatOpenAI},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	target := svc.router.Select(req)

	annotateRoutingDecision(context.Background(), svc, req, target)
}

func TestAnnotateRoutingDecision_IncludesPolicyAnnotations(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		Primary: "ollama/phi4-mini:latest",
		Providers: []Provider{
			{Name: "ollama", URL: "http://ollama", Format: FormatOllama, IsLocal: true},
		},
		Policies: map[string]ModelPolicy{
			"phi4-mini:latest": {
				State:             ModelStateNiche,
				PrimaryReasonCode: "latency_nonviable",
				ReasonCodes:       []string{"latency_nonviable", "user_preference"},
				HumanReason:       "Keep this model only for latency-tolerant local work.",
				EvidenceRefs:      []string{"baseline:run-1"},
				Source:            "persisted_policy",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	req := &Request{AgentRole: "orchestrator", Messages: []Message{{Role: "user", Content: "hello"}}}
	target := svc.router.Select(req)

	tr := &traceCapture{}
	annotateRoutingDecision(WithRoutingTracer(context.Background(), tr), svc, req, target)

	if got := tr.values["inference.routing.policy_state"]; got != ModelStateNiche {
		t.Fatalf("policy_state = %#v, want %q", got, ModelStateNiche)
	}
	if got := tr.values["inference.routing.primary_reason_code"]; got != "latency_nonviable" {
		t.Fatalf("primary_reason_code = %#v", got)
	}
	if got := tr.values["inference.routing.policy_source"]; got != "persisted_policy" {
		t.Fatalf("policy_source = %#v", got)
	}
	if got := tr.values["inference.routing.live_routable"]; got != true {
		t.Fatalf("live_routable = %#v, want true", got)
	}
	if got := tr.values["inference.routing.benchmark_eligible"]; got != true {
		t.Fatalf("benchmark_eligible = %#v, want true", got)
	}
	if got := tr.values["inference.routing.eligibility_reason"]; got != "generalist_default" {
		t.Fatalf("eligibility_reason = %#v", got)
	}
}

func TestAnnotateRoutingDecision_ExplainsEvidenceAndExclusions(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		Primary: "moonshot/kimi-k2-turbo-preview",
		Fallbacks: []string{
			"ollama/phi4-mini:latest",
			"ollama/phi4-reasoning:14b",
		},
		Providers: []Provider{
			{Name: "moonshot", URL: "http://moonshot", Format: FormatOpenAIResponses},
			{Name: "ollama", URL: "http://ollama", Format: FormatOllama, IsLocal: true},
		},
		Policies: map[string]ModelPolicy{
			"phi4-mini:latest": {
				State:             ModelStateNiche,
				PrimaryReasonCode: "under_scrutiny",
				ReasonCodes:       []string{"under_scrutiny", "latency_heavy"},
				Source:            "configured_policy",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.intentQuality.RecordWithIntent("kimi-k2-turbo-preview", IntentToolUse.String(), 0.82)
	req := &Request{
		AgentRole:      "orchestrator",
		TurnWeight:     "standard",
		TaskIntent:     "task",
		TaskComplexity: "simple",
		IntentClass:    IntentToolUse.String(),
		Tools:          []ToolDef{{Type: "function", Function: ToolFuncDef{Name: "obsidian_write"}}},
		Messages:       []Message{{Role: "user", Content: "Create the note in the vault."}},
	}
	target := svc.router.Select(req)

	tr := &traceCapture{}
	annotateRoutingDecision(WithRoutingTracer(context.Background(), tr), svc, req, target)

	if got := tr.values["inference.routing.request_eligible_candidates"]; got == nil {
		t.Fatal("expected request_eligible_candidates annotation")
	}
	if got := tr.values["inference.routing.excluded_candidates"]; got == nil {
		t.Fatal("expected excluded_candidates annotation")
	}
	if got := tr.values["inference.routing.ignored_for_missing_capability_evidence"]; got == nil {
		t.Fatal("expected ignored_for_missing_capability_evidence annotation")
	}
	if got := tr.values["inference.routing.capability_evidence_recommendation"]; got == nil {
		t.Fatal("expected capability evidence recommendation")
	}
}

func TestAnnotateRoutingDecision_UsesCanonicalIntentEvidenceKeys(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		Primary: "moonshot/kimi-k2-turbo-preview",
		Fallbacks: []string{
			"openrouter/openai/gpt-4o-mini",
		},
		Providers: []Provider{
			{Name: "moonshot", URL: "http://moonshot", Format: FormatOpenAIResponses},
			{Name: "openrouter", URL: "http://openrouter", Format: FormatOpenAIResponses},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.intentQuality.RecordWithIntent("openrouter/openai/gpt-4o-mini", IntentToolUse.String(), 0.81)
	req := &Request{
		AgentRole:      "orchestrator",
		TurnWeight:     "standard",
		TaskIntent:     "task",
		TaskComplexity: "simple",
		IntentClass:    IntentToolUse.String(),
		Tools:          []ToolDef{{Type: "function", Function: ToolFuncDef{Name: "obsidian_write"}}},
		Messages:       []Message{{Role: "user", Content: "Create the note in the vault."}},
	}
	target := svc.router.Select(req)

	tr := &traceCapture{}
	annotateRoutingDecision(WithRoutingTracer(context.Background(), tr), svc, req, target)

	if got := tr.values["inference.routing.ignored_for_missing_capability_evidence"]; got != nil {
		if list, ok := got.([]string); ok {
			for _, item := range list {
				if item == "openai/gpt-4o-mini" || item == "openrouter/openai/gpt-4o-mini" {
					t.Fatalf("canonical evidence mismatch: %v should not be listed as missing capability evidence", got)
				}
			}
		}
	}
}

func TestAnnotateRoutingDecision_UsesProviderAwareAliasKeysForLocalModels(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		Primary: "openrouter/openai/gpt-4o-mini",
		Fallbacks: []string{
			"ollama/gemma4",
			"ollama/phi4-mini:latest",
		},
		Providers: []Provider{
			{Name: "openrouter", URL: "http://openrouter", Format: FormatOpenAIResponses},
			{Name: "ollama", URL: "http://ollama", Format: FormatOllama, IsLocal: true},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.intentQuality.RecordWithIntent("ollama/gemma4", IntentToolUse.String(), 0.71)
	svc.intentQuality.RecordWithIntent("ollama/phi4-mini:latest", IntentToolUse.String(), 0.83)

	req := &Request{
		AgentRole:      "orchestrator",
		TurnWeight:     "standard",
		TaskIntent:     "task",
		TaskComplexity: "simple",
		IntentClass:    IntentToolUse.String(),
		Tools:          []ToolDef{{Type: "function", Function: ToolFuncDef{Name: "obsidian_write"}}},
		Messages:       []Message{{Role: "user", Content: "Create the note in the vault."}},
	}
	target := svc.router.Select(req)

	tr := &traceCapture{}
	annotateRoutingDecision(WithRoutingTracer(context.Background(), tr), svc, req, target)

	if got := tr.values["inference.routing.ignored_for_missing_capability_evidence"]; got != nil {
		if list, ok := got.([]string); ok {
			for _, item := range list {
				if item == "gemma4" || item == "phi4-mini:latest" || item == "ollama/gemma4" || item == "ollama/phi4-mini:latest" {
					t.Fatalf("provider-aware alias mismatch: %v should not be listed as missing capability evidence", got)
				}
			}
		}
	}
}
