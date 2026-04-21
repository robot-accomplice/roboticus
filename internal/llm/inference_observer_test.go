package llm

import (
	"context"
	"strings"
	"testing"
	"time"
)

type recordingObserver struct {
	summary  map[string]any
	counters map[string]int
	events   []observerEvent
}

type observerEvent struct {
	eventType string
	status    string
	details   map[string]any
}

func newRecordingObserver() *recordingObserver {
	return &recordingObserver{
		summary:  make(map[string]any),
		counters: make(map[string]int),
	}
}

func (r *recordingObserver) RecordEvent(eventType, status, _, _ string, details map[string]any) string {
	r.events = append(r.events, observerEvent{eventType: eventType, status: status, details: details})
	return eventType
}

func (r *recordingObserver) RecordTimedEvent(eventType, status, _, _ string, _ time.Time, _ string, details map[string]any) string {
	r.events = append(r.events, observerEvent{eventType: eventType, status: status, details: details})
	return eventType
}

func (r *recordingObserver) SetSummaryField(key string, value any) {
	r.summary[key] = value
}

func (r *recordingObserver) IncrementSummaryCounter(key string, delta int) {
	r.counters[key] += delta
}

func TestServiceComplete_ObserverCapturesFallbackDiagnostics(t *testing.T) {
	failClient, _ := NewClientWithHTTP(&Provider{
		Name: "primary", URL: "http://fail", Format: FormatOpenAI,
	}, &mockHTTP{err: context.DeadlineExceeded})
	successClient, _ := NewClientWithHTTP(&Provider{
		Name: "fallback", URL: "http://ok", Format: FormatOpenAI,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"fb","model":"fallback-model","choices":[{"message":{"content":"fallback response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":42,"completion_tokens":9}}`,
	})

	svc, _ := NewService(ServiceConfig{
		Primary:   "primary",
		Fallbacks: []string{"fallback"},
	}, nil)
	svc.providers["primary"] = failClient
	svc.providers["fallback"] = successClient

	obs := newRecordingObserver()
	ctx := WithInferenceObserver(context.Background(), obs)
	resp, err := svc.Complete(ctx, &Request{
		Model:          "gpt-4",
		Messages:       []Message{{Role: "user", Content: "what happened during /breaker?"}},
		Tools:          []ToolDef{{Type: "function", Function: ToolFuncDef{Name: "list_directory"}}},
		TurnWeight:     "heavy",
		TaskIntent:     "code",
		TaskComplexity: "complex",
		IntentClass:    IntentCoding.String(),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := obs.counters["inference_attempts"]; got != 2 {
		t.Fatalf("inference_attempts = %d, want 2", got)
	}
	if got := obs.counters["fallback_count"]; got != 1 {
		t.Fatalf("fallback_count = %d, want 1", got)
	}
	if got := obs.summary["final_provider"]; got != "fallback" {
		t.Fatalf("final_provider = %v, want fallback", got)
	}
	if got := obs.summary["final_model"]; got != "fallback-model" {
		t.Fatalf("final_model = %v, want fallback-model", got)
	}
	if got := obs.summary["resource_pressure"]; got != "high" {
		t.Fatalf("resource_pressure = %v, want high", got)
	}
	if got := obs.summary["primary_diagnosis"]; got != "local_model_resource_instability" {
		t.Fatalf("primary_diagnosis = %v, want local_model_resource_instability", got)
	}

	requiredEvents := map[string]bool{
		"routing_chain_built":    false,
		"model_attempt_started":  false,
		"model_attempt_finished": false,
		"fallback_triggered":     false,
	}
	for _, ev := range obs.events {
		if _, ok := requiredEvents[ev.eventType]; ok {
			requiredEvents[ev.eventType] = true
		}
		if ev.eventType == "routing_chain_built" {
			if got := ev.details["task_intent"]; got != "code" {
				t.Fatalf("routing task_intent = %v, want code", got)
			}
			if got := ev.details["task_complexity"]; got != "complex" {
				t.Fatalf("routing task_complexity = %v, want complex", got)
			}
			if got := ev.details["intent_class"]; got != IntentCoding.String() {
				t.Fatalf("routing intent_class = %v, want %q", got, IntentCoding.String())
			}
			if got := ev.details["complexity_source"]; got != "pipeline_task_complexity" {
				t.Fatalf("complexity_source = %v, want pipeline_task_complexity", got)
			}
		}
	}
	for eventType, seen := range requiredEvents {
		if !seen {
			t.Fatalf("expected event %q to be recorded", eventType)
		}
	}
}

func TestServiceComplete_RoutingChainEventCarriesCapabilityEvidence(t *testing.T) {
	moonshotClient, _ := NewClientWithHTTP(&Provider{
		Name: "moonshot", URL: "http://moonshot", Format: FormatOpenAI,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"ok","model":"kimi-k2","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":32,"completion_tokens":7}}`,
	})
	ollamaClient, _ := NewClientWithHTTP(&Provider{
		Name: "ollama", URL: "http://ollama", Format: FormatOpenAI, IsLocal: true,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"ok","model":"phi4-mini","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":32,"completion_tokens":7}}`,
	})
	openrouterClient, _ := NewClientWithHTTP(&Provider{
		Name: "openrouter", URL: "http://openrouter", Format: FormatOpenAI,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"ok","model":"new-hotness","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":32,"completion_tokens":7}}`,
	})

	svc, err := NewService(ServiceConfig{
		Providers: []Provider{
			{Name: "moonshot", URL: "http://moonshot", Format: FormatOpenAI},
			{Name: "ollama", URL: "http://ollama", Format: FormatOpenAI, IsLocal: true},
			{Name: "openrouter", URL: "http://openrouter", Format: FormatOpenAI},
		},
		Primary:       "moonshot/kimi-k2",
		Fallbacks:     []string{"ollama/phi4-mini", "openrouter/new-hotness"},
		ToolBlocklist: []string{"phi4-mini"},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.providers["moonshot"] = moonshotClient
	svc.providers["ollama"] = ollamaClient
	svc.providers["openrouter"] = openrouterClient

	obs := newRecordingObserver()
	ctx := WithInferenceObserver(context.Background(), obs)
	resp, err := svc.Complete(ctx, &Request{
		Messages:       []Message{{Role: "user", Content: "Use a tool to inspect the vault and report back."}},
		Tools:          []ToolDef{{Type: "function", Function: ToolFuncDef{Name: "obsidian_write"}}},
		TurnWeight:     "standard",
		TaskIntent:     "task",
		TaskComplexity: "moderate",
		IntentClass:    IntentToolUse.String(),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	var routingEvent *observerEvent
	for i := range obs.events {
		if obs.events[i].eventType == "routing_chain_built" {
			routingEvent = &obs.events[i]
			break
		}
	}
	if routingEvent == nil {
		t.Fatal("expected routing_chain_built event")
	}
	if got := routingEvent.details["request_eligible_candidates"]; got == nil {
		t.Fatal("expected request_eligible_candidates in routing_chain_built details")
	}
	var firstExcluded map[string]any
	switch raw := routingEvent.details["excluded_candidates"].(type) {
	case []map[string]any:
		if len(raw) == 0 {
			t.Fatalf("excluded_candidates = %v, want at least one item", routingEvent.details["excluded_candidates"])
		}
		firstExcluded = raw[0]
	case []any:
		if len(raw) == 0 {
			t.Fatalf("excluded_candidates = %v, want at least one item", routingEvent.details["excluded_candidates"])
		}
		var ok bool
		firstExcluded, ok = raw[0].(map[string]any)
		if !ok {
			t.Fatalf("first excluded candidate = %T, want map[string]any", raw[0])
		}
	default:
		t.Fatalf("excluded_candidates = %T, want slice", routingEvent.details["excluded_candidates"])
	}
	if firstExcluded["model"] != "ollama/phi4-mini" {
		t.Fatalf("excluded model = %v, want ollama/phi4-mini", firstExcluded["model"])
	}
	if got := routingEvent.details["ignored_for_missing_capability_evidence"]; got == nil {
		t.Fatal("expected ignored_for_missing_capability_evidence in routing_chain_built details")
	}
	suggestion, _ := routingEvent.details["capability_evidence_recommendation"].(string)
	if !strings.Contains(suggestion, "have no runtime evidence yet for "+IntentToolUse.String()) {
		t.Fatalf("capability_evidence_recommendation = %q", suggestion)
	}
}
