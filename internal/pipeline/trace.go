package pipeline

import (
	"encoding/json"
	"time"
)

// PipelineTrace records per-stage timing for a single pipeline run.
type PipelineTrace struct {
	ID      string      `json:"id"`
	TurnID  string      `json:"turn_id"`
	Channel string      `json:"channel"`
	TotalMs int64       `json:"total_ms"`
	Stages  []TraceSpan `json:"stages"`
}

// TraceSpan is a single timed stage within the pipeline.
type TraceSpan struct {
	Name       string         `json:"name"`
	DurationMs int64          `json:"duration_ms"`
	Outcome    string         `json:"outcome"` // "ok", "skipped", "error"
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// TraceRecorder accumulates spans during a pipeline run.
type TraceRecorder struct {
	start   time.Time
	current *spanState
	spans   []TraceSpan
}

type spanState struct {
	name  string
	start time.Time
	meta  map[string]any
}

// NewTraceRecorder starts a new pipeline trace.
func NewTraceRecorder() *TraceRecorder {
	return &TraceRecorder{start: time.Now()}
}

// BeginSpan starts a named timing span. Any active span is auto-ended first.
func (r *TraceRecorder) BeginSpan(name string) {
	if r.current != nil {
		r.endCurrentSpan("ok")
	}
	r.current = &spanState{name: name, start: time.Now()}
}

// Annotate adds metadata to the current span.
func (r *TraceRecorder) Annotate(key string, value any) {
	if r.current == nil {
		return
	}
	if r.current.meta == nil {
		r.current.meta = make(map[string]any)
	}
	r.current.meta[key] = value
}

// EndSpan finishes the current span with the given outcome.
func (r *TraceRecorder) EndSpan(outcome string) {
	if r.current != nil {
		r.endCurrentSpan(outcome)
	}
}

func (r *TraceRecorder) endCurrentSpan(outcome string) {
	dur := time.Since(r.current.start).Milliseconds()
	r.spans = append(r.spans, TraceSpan{
		Name:       r.current.name,
		DurationMs: dur,
		Outcome:    outcome,
		Metadata:   r.current.meta,
	})
	r.current = nil
}

// Finish closes any active span and returns the completed trace.
func (r *TraceRecorder) Finish(turnID, channel string) *PipelineTrace {
	if r.current != nil {
		r.endCurrentSpan("ok")
	}
	return &PipelineTrace{
		TurnID:  turnID,
		Channel: channel,
		TotalMs: time.Since(r.start).Milliseconds(),
		Stages:  r.spans,
	}
}

// StagesJSON returns the stages as a JSON string for DB storage.
func (t *PipelineTrace) StagesJSON() string {
	b, _ := json.Marshal(t.Stages)
	return string(b)
}
