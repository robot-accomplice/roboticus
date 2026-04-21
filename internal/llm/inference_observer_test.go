package llm

import (
	"context"
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
