package pipeline

import (
	"encoding/json"
	"sync"
	"time"

	"roboticus/internal/db"
)

type TurnDiagnosticSummary struct {
	TurnID                 string  `json:"turn_id"`
	SessionID              string  `json:"session_id"`
	Channel                string  `json:"channel"`
	Status                 string  `json:"status"`
	FinalModel             string  `json:"final_model,omitempty"`
	FinalProvider          string  `json:"final_provider,omitempty"`
	TotalMs                int64   `json:"total_ms"`
	InferenceAttempts      int     `json:"inference_attempts"`
	FallbackCount          int     `json:"fallback_count"`
	ToolCallCount          int     `json:"tool_call_count"`
	GuardRetryCount        int     `json:"guard_retry_count"`
	VerifierRetryCount     int     `json:"verifier_retry_count"`
	ReplaySuppressionCount int     `json:"replay_suppression_count"`
	RequestMessages        int     `json:"request_messages"`
	RequestTools           int     `json:"request_tools"`
	RequestApproxTokens    int     `json:"request_approx_tokens"`
	ContextPressure        string  `json:"context_pressure,omitempty"`
	ResourcePressure       string  `json:"resource_pressure,omitempty"`
	ResourceSnapshotJSON   string  `json:"resource_snapshot_json,omitempty"`
	PrimaryDiagnosis       string  `json:"primary_diagnosis,omitempty"`
	DiagnosisConfidence    float64 `json:"diagnosis_confidence,omitempty"`
	UserNarrative          string  `json:"user_narrative,omitempty"`
	OperatorNarrative      string  `json:"operator_narrative,omitempty"`
	RecommendationsJSON    string  `json:"recommendations_json,omitempty"`
}

type TurnDiagnosticEvent struct {
	EventID         string         `json:"event_id"`
	TurnID          string         `json:"turn_id"`
	Seq             int            `json:"seq"`
	Type            string         `json:"type"`
	AtMs            int64          `json:"at_ms"`
	DurationMs      int64          `json:"duration_ms,omitempty"`
	ParentEventID   string         `json:"parent_event_id,omitempty"`
	Status          string         `json:"status"`
	OperatorSummary string         `json:"operator_summary,omitempty"`
	UserSummary     string         `json:"user_summary,omitempty"`
	Details         map[string]any `json:"details,omitempty"`
}

type TurnRecommendation struct {
	Kind                string   `json:"kind"`
	Subject             string   `json:"subject,omitempty"`
	ReasonCode          string   `json:"reason_code"`
	Confidence          float64  `json:"confidence"`
	EvidenceEventIDs    []string `json:"evidence_event_ids,omitempty"`
	OperatorExplanation string   `json:"operator_explanation,omitempty"`
	UserExplanation     string   `json:"user_explanation,omitempty"`
	ApplyPayloadJSON    string   `json:"apply_payload_json,omitempty"`
}

type TurnDiagnosticsRecorder struct {
	mu                sync.Mutex
	start             time.Time
	seq               int
	flushedEvents     int
	dirty             bool
	finalized         bool
	activeAttempt     *diagnosticAttemptState
	completedAttempts int
	lastRetryKind     string
	summary           TurnDiagnosticSummary
	events            []TurnDiagnosticEvent
	recommendations   []TurnRecommendation
}

type diagnosticAttemptState struct {
	Provider string
	Model    string
	Started  time.Time
}

type LivenessSnapshot struct {
	Scope    string
	Phase    string
	Message  string
	Details  map[string]any
	Severity string
}

func NewTurnDiagnosticsRecorder(sessionID, turnID, channel string) *TurnDiagnosticsRecorder {
	return &TurnDiagnosticsRecorder{
		start: time.Now(),
		summary: TurnDiagnosticSummary{
			TurnID:    turnID,
			SessionID: sessionID,
			Channel:   channel,
			Status:    "running",
		},
		dirty: true,
	}
}

func (r *TurnDiagnosticsRecorder) RecordEvent(eventType, status, operatorSummary, userSummary string, details map[string]any) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	id := db.NewID()
	r.events = append(r.events, TurnDiagnosticEvent{
		EventID:         id,
		TurnID:          r.summary.TurnID,
		Seq:             r.seq,
		Type:            eventType,
		AtMs:            time.Since(r.start).Milliseconds(),
		Status:          status,
		OperatorSummary: operatorSummary,
		UserSummary:     userSummary,
		Details:         cloneDiagnosticMap(details),
	})
	r.applyLifecycleEvent(eventType, status, details, time.Now())
	r.dirty = true
	return id
}

func (r *TurnDiagnosticsRecorder) RecordTimedEvent(eventType, status, operatorSummary, userSummary string, started time.Time, parentEventID string, details map[string]any) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	id := db.NewID()
	r.events = append(r.events, TurnDiagnosticEvent{
		EventID:         id,
		TurnID:          r.summary.TurnID,
		Seq:             r.seq,
		Type:            eventType,
		AtMs:            started.Sub(r.start).Milliseconds(),
		DurationMs:      time.Since(started).Milliseconds(),
		ParentEventID:   parentEventID,
		Status:          status,
		OperatorSummary: operatorSummary,
		UserSummary:     userSummary,
		Details:         cloneDiagnosticMap(details),
	})
	r.applyLifecycleEvent(eventType, status, details, started)
	r.dirty = true
	return id
}

func (r *TurnDiagnosticsRecorder) SetSummaryField(key string, value any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch key {
	case "turn_id":
		r.summary.TurnID = diagnosticToString(value)
		for i := range r.events {
			if r.events[i].TurnID == "" {
				r.events[i].TurnID = r.summary.TurnID
			}
		}
	case "session_id":
		r.summary.SessionID = diagnosticToString(value)
	case "channel":
		r.summary.Channel = diagnosticToString(value)
	case "status":
		r.summary.Status = diagnosticToString(value)
	case "final_model":
		r.summary.FinalModel = diagnosticToString(value)
	case "final_provider":
		r.summary.FinalProvider = diagnosticToString(value)
	case "context_pressure":
		r.summary.ContextPressure = diagnosticToString(value)
	case "resource_pressure":
		r.summary.ResourcePressure = diagnosticToString(value)
	case "resource_snapshot", "resource_snapshot_json":
		if raw, ok := value.(string); ok {
			r.summary.ResourceSnapshotJSON = raw
		} else if buf, err := json.Marshal(value); err == nil {
			r.summary.ResourceSnapshotJSON = string(buf)
		}
	case "primary_diagnosis":
		r.summary.PrimaryDiagnosis = diagnosticToString(value)
	case "user_narrative":
		r.summary.UserNarrative = diagnosticToString(value)
	case "operator_narrative":
		r.summary.OperatorNarrative = diagnosticToString(value)
	case "diagnosis_confidence":
		r.summary.DiagnosisConfidence = diagnosticToFloat64(value)
	case "request_messages":
		r.summary.RequestMessages = diagnosticToInt(value)
	case "request_tools":
		r.summary.RequestTools = diagnosticToInt(value)
	case "request_approx_tokens":
		r.summary.RequestApproxTokens = diagnosticToInt(value)
	}
	r.dirty = true
}

func (r *TurnDiagnosticsRecorder) IncrementSummaryCounter(key string, delta int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch key {
	case "inference_attempts":
		r.summary.InferenceAttempts += delta
	case "fallback_count":
		r.summary.FallbackCount += delta
	case "tool_call_count":
		r.summary.ToolCallCount += delta
	case "guard_retry_count":
		r.summary.GuardRetryCount += delta
	case "verifier_retry_count":
		r.summary.VerifierRetryCount += delta
	case "replay_suppression_count":
		r.summary.ReplaySuppressionCount += delta
	}
	r.dirty = true
}

func (r *TurnDiagnosticsRecorder) AddRecommendation(rec TurnRecommendation) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recommendations = append(r.recommendations, rec)
	r.dirty = true
}

func (r *TurnDiagnosticsRecorder) Finish(finalStatus string) (TurnDiagnosticSummary, []TurnDiagnosticEvent) {
	if r == nil {
		return TurnDiagnosticSummary{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if finalStatus != "" {
		r.summary.Status = finalStatus
	}
	r.summary.TotalMs = time.Since(r.start).Milliseconds()
	if len(r.recommendations) > 0 {
		if buf, err := json.Marshal(r.recommendations); err == nil {
			r.summary.RecommendationsJSON = string(buf)
		}
	}
	outEvents := make([]TurnDiagnosticEvent, len(r.events))
	for i, ev := range r.events {
		if ev.TurnID == "" {
			ev.TurnID = r.summary.TurnID
		}
		outEvents[i] = ev
	}
	return r.summary, outEvents
}

func (r *TurnDiagnosticsRecorder) SnapshotForFlush(finalStatus string) (TurnDiagnosticSummary, []TurnDiagnosticEvent, bool) {
	if r == nil {
		return TurnDiagnosticSummary{}, nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if finalStatus != "" {
		r.summary.Status = finalStatus
		r.finalized = true
		r.dirty = true
	}
	if !r.dirty && r.flushedEvents >= len(r.events) {
		return TurnDiagnosticSummary{}, nil, false
	}
	summary := r.summary
	summary.TotalMs = time.Since(r.start).Milliseconds()
	if len(r.recommendations) > 0 {
		if buf, err := json.Marshal(r.recommendations); err == nil {
			summary.RecommendationsJSON = string(buf)
		}
	}
	derivedStatus := diagnosticsStatusFromEvents(r.events)
	if summary.Status == "" || summary.Status == "running" || summary.Status == "ok" || (summary.Status == "degraded" && derivedStatus == "ok") {
		summary.Status = derivedStatus
	}
	newEvents := make([]TurnDiagnosticEvent, len(r.events[r.flushedEvents:]))
	for i, ev := range r.events[r.flushedEvents:] {
		if ev.TurnID == "" {
			ev.TurnID = summary.TurnID
		}
		newEvents[i] = ev
	}
	r.flushedEvents = len(r.events)
	r.dirty = false
	return summary, newEvents, true
}

func (r *TurnDiagnosticsRecorder) LivenessSnapshot(stage string, runningFor time.Duration) LivenessSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := LivenessSnapshot{
		Scope:    "stage",
		Phase:    "orchestration_wait",
		Message:  "pipeline stage exceeded the liveness threshold",
		Severity: "error",
		Details: map[string]any{
			"stage":          stage,
			"running_for_ms": runningFor.Milliseconds(),
		},
	}

	if r.activeAttempt != nil {
		snap.Scope = "model_attempt"
		snap.Details["provider"] = r.activeAttempt.Provider
		snap.Details["model"] = r.activeAttempt.Model
		snap.Details["attempt_running_for_ms"] = time.Since(r.activeAttempt.Started).Milliseconds()
		if r.completedAttempts == 0 {
			snap.Phase = "initial_attempt_wait"
			snap.Message = "waiting on the first model attempt to return"
		} else {
			snap.Phase = "retry_attempt_wait"
			snap.Message = "a retry model attempt is still running after an earlier response"
			if r.lastRetryKind != "" {
				snap.Details["retry_kind"] = r.lastRetryKind
			}
		}
		return snap
	}

	if r.completedAttempts > 0 {
		snap.Scope = "post_attempt_recovery"
		snap.Phase = "verification_or_guard_recovery"
		snap.Message = "the turn is still running after at least one model response"
		if r.lastRetryKind != "" {
			snap.Details["retry_kind"] = r.lastRetryKind
		}
		return snap
	}

	snap.Phase = "pre_attempt_orchestration"
	snap.Message = "the inference stage is stalled before any model attempt completed"
	return snap
}

func (r *TurnDiagnosticsRecorder) applyLifecycleEvent(eventType, status string, details map[string]any, started time.Time) {
	switch eventType {
	case "model_attempt_started":
		r.activeAttempt = &diagnosticAttemptState{
			Provider: diagnosticMapString(details, "provider"),
			Model:    diagnosticMapString(details, "model"),
			Started:  started,
		}
	case "model_attempt_finished":
		if r.activeAttempt != nil {
			r.activeAttempt = nil
		}
		r.completedAttempts++
		if status == "ok" {
			r.lastRetryKind = ""
		}
	case "guard_retry_scheduled":
		r.lastRetryKind = "guard"
	case "verifier_retry_scheduled":
		r.lastRetryKind = "verifier"
	case "response_finalized":
		r.lastRetryKind = ""
	}
}

func cloneDiagnosticMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func diagnosticToString(v any) string {
	s, _ := v.(string)
	return s
}

func diagnosticToInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

func diagnosticToFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}

func diagnosticMapString(m map[string]any, key string) string {
	if len(m) == 0 {
		return ""
	}
	return diagnosticToString(m[key])
}
