package llm

import (
	"context"
	"sync/atomic"
	"testing"

	"roboticus/internal/hostresources"
)

func TestServiceComplete_ObserverCapturesHostResourceSnapshots(t *testing.T) {
	var calls int32
	restore := hostresources.SetSamplerForTests(func(context.Context) hostresources.Snapshot {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			return hostresources.Snapshot{
				CollectedAt:          "2026-04-20T19:00:00Z",
				CPUPercent:           11.5,
				MemoryAvailableBytes: 8_000_000_000,
				OllamaRSSBytes:       2_000_000_000,
				RoboticusRSSBytes:    250_000_000,
			}
		}
		return hostresources.Snapshot{
			CollectedAt:          "2026-04-20T19:00:02Z",
			CPUPercent:           64.25,
			MemoryAvailableBytes: 5_500_000_000,
			OllamaRSSBytes:       3_400_000_000,
			RoboticusRSSBytes:    275_000_000,
		}
	})
	t.Cleanup(restore)

	client, _ := NewClientWithHTTP(&Provider{
		Name: "ollama", URL: "http://ok", Format: FormatOpenAI, IsLocal: true,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"ok","model":"phi4-mini:latest","choices":[{"message":{"content":"ready"},"finish_reason":"stop"}],"usage":{"prompt_tokens":16,"completion_tokens":5}}`,
	})

	svc, _ := NewService(ServiceConfig{
		Primary: "ollama/phi4-mini:latest",
		Providers: []Provider{
			{Name: "ollama", URL: "http://ok", Format: FormatOpenAI, IsLocal: true},
		},
	}, nil)
	svc.providers["ollama"] = client

	obs := newRecordingObserver()
	ctx := WithInferenceObserver(context.Background(), obs)
	resp, err := svc.Complete(ctx, &Request{
		Model:          "ollama/phi4-mini:latest",
		Messages:       []Message{{Role: "user", Content: "say hello"}},
		TurnWeight:     "light",
		TaskIntent:     "conversation",
		TaskComplexity: "simple",
		IntentClass:    IntentConversation.String(),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	var sawStart, sawFinish bool
	for _, ev := range obs.events {
		if ev.eventType == "model_attempt_started" {
			sawStart = true
			snapshot, ok := ev.details["host_resources"].(hostresources.Snapshot)
			if !ok {
				t.Fatalf("model_attempt_started host_resources = %T, want hostresources.Snapshot", ev.details["host_resources"])
			}
			if snapshot.CPUPercent != 11.5 {
				t.Fatalf("start snapshot CPUPercent = %v, want 11.5", snapshot.CPUPercent)
			}
		}
		if ev.eventType == "model_attempt_finished" && ev.status == "ok" {
			sawFinish = true
			snapshot, ok := ev.details["host_resources"].(hostresources.Snapshot)
			if !ok {
				t.Fatalf("model_attempt_finished host_resources = %T, want hostresources.Snapshot", ev.details["host_resources"])
			}
			if snapshot.CPUPercent != 64.25 {
				t.Fatalf("finish snapshot CPUPercent = %v, want 64.25", snapshot.CPUPercent)
			}
		}
	}
	if !sawStart {
		t.Fatal("expected model_attempt_started event")
	}
	if !sawFinish {
		t.Fatal("expected successful model_attempt_finished event")
	}

	summarySnapshot, ok := obs.summary["resource_snapshot"].(hostresources.Snapshot)
	if !ok {
		t.Fatalf("summary resource_snapshot = %T, want hostresources.Snapshot", obs.summary["resource_snapshot"])
	}
	if summarySnapshot.CPUPercent != 64.25 {
		t.Fatalf("summary resource snapshot CPUPercent = %v, want 64.25", summarySnapshot.CPUPercent)
	}
}
