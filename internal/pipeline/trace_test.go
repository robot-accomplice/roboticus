package pipeline

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTraceRecorder_BasicFlow(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("validation")
	time.Sleep(time.Millisecond)
	tr.EndSpan("ok")

	tr.BeginSpan("inference")
	tr.Annotate("model", "gpt-4")
	time.Sleep(time.Millisecond)
	tr.EndSpan("ok")

	trace := tr.Finish("turn-1", "api")
	if trace.TurnID != "turn-1" {
		t.Errorf("TurnID = %s, want turn-1", trace.TurnID)
	}
	if trace.Channel != "api" {
		t.Errorf("Channel = %s, want api", trace.Channel)
	}
	if len(trace.Stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(trace.Stages))
	}
	if trace.Stages[0].Name != "validation" {
		t.Errorf("stage 0 name = %s", trace.Stages[0].Name)
	}
	if trace.Stages[1].Metadata["model"] != "gpt-4" {
		t.Errorf("stage 1 missing model annotation")
	}
	if trace.TotalMs < 1 {
		t.Errorf("total_ms should be > 0, got %d", trace.TotalMs)
	}
}

func TestTraceRecorder_AutoClose(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("first")
	tr.BeginSpan("second") // auto-closes "first"
	tr.EndSpan("ok")

	trace := tr.Finish("t1", "api")
	if len(trace.Stages) != 2 {
		t.Fatalf("got %d stages, want 2 (auto-close should create first)", len(trace.Stages))
	}
	if trace.Stages[0].Name != "first" {
		t.Errorf("first stage name = %s", trace.Stages[0].Name)
	}
}

func TestTraceRecorder_StagesJSON(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("test")
	tr.EndSpan("ok")
	trace := tr.Finish("t1", "api")

	j := trace.StagesJSON()
	var stages []TraceSpan
	if err := json.Unmarshal([]byte(j), &stages); err != nil {
		t.Fatalf("StagesJSON not valid JSON: %v", err)
	}
	if len(stages) != 1 {
		t.Errorf("got %d stages from JSON, want 1", len(stages))
	}
}
