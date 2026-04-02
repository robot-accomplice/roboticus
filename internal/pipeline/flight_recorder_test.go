package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReactTrace_RecordStep(t *testing.T) {
	tests := []struct {
		name      string
		steps     []ReactStep
		wantCount int
		wantKinds []StepKind
	}{
		{
			name:      "empty trace",
			steps:     nil,
			wantCount: 0,
			wantKinds: nil,
		},
		{
			name: "single tool call",
			steps: []ReactStep{
				{Kind: StepToolCall, Name: "web_search", DurationMs: 150, Success: true, Source: ToolSource{Kind: "builtin"}},
			},
			wantCount: 1,
			wantKinds: []StepKind{StepToolCall},
		},
		{
			name: "mixed step types",
			steps: []ReactStep{
				{Kind: StepLLMCall, Name: "llm", DurationMs: 500, Success: true},
				{Kind: StepToolCall, Name: "file_read", DurationMs: 10, Success: true, Source: ToolSource{Kind: "builtin"}},
				{Kind: StepGuardCheck, Name: "empty_response", DurationMs: 1, Success: true},
				{Kind: StepRetry, Name: "repetition", DurationMs: 0, Success: false},
			},
			wantCount: 4,
			wantKinds: []StepKind{StepLLMCall, StepToolCall, StepGuardCheck, StepRetry},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := NewReactTrace()
			for _, s := range tt.steps {
				rt.RecordStep(s)
			}
			if len(rt.Steps) != tt.wantCount {
				t.Errorf("step count = %d, want %d", len(rt.Steps), tt.wantCount)
			}
			for i, wk := range tt.wantKinds {
				if rt.Steps[i].Kind != wk {
					t.Errorf("step[%d].Kind = %d, want %d", i, rt.Steps[i].Kind, wk)
				}
			}
		})
	}
}

func TestReactTrace_Truncation(t *testing.T) {
	rt := NewReactTrace()
	longInput := strings.Repeat("x", 1000)
	longOutput := strings.Repeat("y", 2000)
	rt.RecordStep(ReactStep{
		Kind:   StepToolCall,
		Name:   "test",
		Input:  longInput,
		Output: longOutput,
	})
	if len(rt.Steps[0].Input) > maxStepFieldLen+3 {
		t.Errorf("input not truncated: len=%d", len(rt.Steps[0].Input))
	}
	if len(rt.Steps[0].Output) > maxStepFieldLen+3 {
		t.Errorf("output not truncated: len=%d", len(rt.Steps[0].Output))
	}
}

func TestReactTrace_JSON(t *testing.T) {
	rt := NewReactTrace()
	rt.RecordStep(ReactStep{Kind: StepToolCall, Name: "echo", Success: true})
	rt.Finish()

	data, err := json.Marshal(rt)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ReactTrace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Steps) != 1 {
		t.Errorf("roundtrip: got %d steps, want 1", len(decoded.Steps))
	}
}

func TestReactTrace_Finish(t *testing.T) {
	rt := NewReactTrace()
	time.Sleep(5 * time.Millisecond)
	rt.Finish()
	if rt.TotalMs <= 0 {
		t.Error("TotalMs should be > 0 after Finish")
	}
}
