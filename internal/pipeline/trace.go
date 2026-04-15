package pipeline

import (
	"encoding/json"
	"time"
)

// Trace namespace constants for structured trace span naming (Wave 8, #88).
// Use these as prefixes for TraceSpan.Name to enable filtering and aggregation.
const (
	TraceNSPipeline   = "pipeline"   // Top-level pipeline orchestration spans
	TraceNSGuard      = "guard"      // Guard chain evaluation spans
	TraceNSInference  = "inference"  // LLM inference spans (standard and streaming)
	TraceNSRetrieval  = "retrieval"  // Memory retrieval and context assembly spans
	TraceNSToolSearch = "toolsearch" // Tool discovery and pruning spans
	TraceNSMCP        = "mcp"        // MCP server interaction spans
	TraceNSDelegation = "delegation" // Subagent delegation spans
	TraceNSTaskState  = "taskstate"  // Task state machine transition spans
	TraceNSVerifier   = "verifier"   // Verification / critic stage annotations
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

// ── Structured Trace Annotation Helpers ────────────────────────────────────
// These functions write trace annotations under consistent namespace prefixes,
// matching Rust's annotate_task_state_trace(), annotate_delegation_trace(), and
// annotate_retrieval_strategy().

// AnnotateVerifierTrace writes structured verifier output onto the current
// span. The full claim-to-evidence map is embedded as JSON so operators can
// audit every unsupported-claim decision without re-running the verifier.
func AnnotateVerifierTrace(tr *TraceRecorder, result VerificationResult) {
	if tr == nil {
		return
	}
	summary := SummarizeVerification(result)
	tr.Annotate(TraceNSVerifier+".passed", summary.Passed)
	if len(summary.IssueCodes) > 0 {
		tr.Annotate(TraceNSVerifier+".issue_codes", summary.IssueCodes)
	}
	tr.Annotate(TraceNSVerifier+".claim_count", summary.ClaimCount)
	tr.Annotate(TraceNSVerifier+".absolute_count", summary.AbsoluteCount)
	tr.Annotate(TraceNSVerifier+".anchored_count", summary.AnchoredCount)
	tr.Annotate(TraceNSVerifier+".unsupported_absolute_count", summary.UnsupportedAbs)
	tr.Annotate(TraceNSVerifier+".coverage_ratio", summary.CoverageRatio)
	tr.Annotate(TraceNSVerifier+".flagged_claims", summary.FlaggedClaims)
	if len(result.ClaimAudits) > 0 {
		if buf, err := json.Marshal(result.ClaimAudits); err == nil {
			tr.Annotate(TraceNSVerifier+".claim_map_json", string(buf))
		}
	}
}

// AnnotateTaskStateTrace writes task synthesis results as namespaced trace
// annotations. Groups annotations under the "task_state" namespace.
func AnnotateTaskStateTrace(tr *TraceRecorder, synthesis TaskSynthesis) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSTaskState+".intent", synthesis.Intent)
	tr.Annotate(TraceNSTaskState+".complexity", synthesis.Complexity)
	tr.Annotate(TraceNSTaskState+".planned_action", synthesis.PlannedAction)
	tr.Annotate(TraceNSTaskState+".confidence", synthesis.Confidence)
	tr.Annotate(TraceNSTaskState+".capability_fit", synthesis.CapabilityFit)
	tr.Annotate(TraceNSTaskState+".retrieval_needed", synthesis.RetrievalNeeded)
	if len(synthesis.MissingSkills) > 0 {
		tr.Annotate(TraceNSTaskState+".missing_skills", synthesis.MissingSkills)
	}
}

// AnnotateDelegationTrace writes delegation decision metadata as namespaced
// trace annotations. Groups under the "delegation" namespace.
func AnnotateDelegationTrace(tr *TraceRecorder, agentName string, subtaskCount int, provenance string) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSDelegation+".agent", agentName)
	tr.Annotate(TraceNSDelegation+".subtask_count", subtaskCount)
	if provenance != "" {
		tr.Annotate(TraceNSDelegation+".provenance", provenance)
	}
}

// AnnotateRetrievalStrategy writes memory retrieval decision metadata.
// Groups under the "retrieval" namespace.
func AnnotateRetrievalStrategy(tr *TraceRecorder, strategy string, budget int, fragmentCount int) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSRetrieval+".strategy", strategy)
	tr.Annotate(TraceNSRetrieval+".budget", budget)
	tr.Annotate(TraceNSRetrieval+".fragments", fragmentCount)
}

// AnnotateInferenceTrace writes inference metadata (model selection, escalation).
// Groups under the "inference" namespace.
func AnnotateInferenceTrace(tr *TraceRecorder, model, provider string, escalated bool) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSInference+".model", model)
	tr.Annotate(TraceNSInference+".provider", provider)
	tr.Annotate(TraceNSInference+".escalated", escalated)
}

// ── Guard Trace Annotations ──────────────────────────────────────────────────

// GuardTraceEntry records a single guard's evaluation result for tracing.
type GuardTraceEntry struct {
	Outcome string `json:"outcome"` // "pass", "fail", "rewrite", "retry"
	Reason  string `json:"reason,omitempty"`
}

// AnnotateGuardTrace writes guard chain evaluation results as namespaced trace
// annotations. Groups under the "guard" namespace.
func AnnotateGuardTrace(tr *TraceRecorder, results map[string]GuardTraceEntry, chainType string, totalMs int64) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSGuard+".results", results)
	tr.Annotate(TraceNSGuard+".chain", chainType)
	tr.Annotate(TraceNSGuard+".total_ms", totalMs)
}

// ── Routing Trace Annotations ────────────────────────────────────────────────

// AnnotateRoutingTrace writes model routing decision metadata as namespaced
// trace annotations. Groups under the "inference" namespace (routing sub-group).
func AnnotateRoutingTrace(tr *TraceRecorder, candidates []string, winner string, winnerScore float64, mode string) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSInference+".routing.candidates", candidates)
	tr.Annotate(TraceNSInference+".routing.winner", winner)
	tr.Annotate(TraceNSInference+".routing.winner_score", winnerScore)
	tr.Annotate(TraceNSInference+".routing.mode", mode)
}

// AnnotateRoutingWeightsTrace writes the active routing weights at selection time.
func AnnotateRoutingWeightsTrace(tr *TraceRecorder, weights map[string]float64) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSInference+".routing.weights", weights)
}

// ── Memory Trace Annotations ─────────────────────────────────────────────────

// AnnotateMemoryTrace writes detailed memory retrieval stats as namespaced
// trace annotations. Groups under the "retrieval" namespace.
func AnnotateMemoryTrace(tr *TraceRecorder, tiersQueried []string, hitsPerTier map[string]int, budgetConsumed int) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSRetrieval+".tiers_queried", tiersQueried)
	tr.Annotate(TraceNSRetrieval+".hits", hitsPerTier)
	tr.Annotate(TraceNSRetrieval+".budget_consumed", budgetConsumed)
}

// ── Context Budget Trace Annotations ─────────────────────────────────────────

// AnnotateContextBudgetTrace writes context window budget allocation as
// namespaced trace annotations. Groups under the "retrieval" namespace
// (context sub-group).
func AnnotateContextBudgetTrace(tr *TraceRecorder, budgetTotal, systemPromptTokens, toolDefsTokens, memoryTokens, historyTokens int) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSRetrieval+".context.budget_total", budgetTotal)
	tr.Annotate(TraceNSRetrieval+".context.system_prompt_tokens", systemPromptTokens)
	tr.Annotate(TraceNSRetrieval+".context.tool_defs_tokens", toolDefsTokens)
	tr.Annotate(TraceNSRetrieval+".context.memory_tokens", memoryTokens)
	tr.Annotate(TraceNSRetrieval+".context.history_tokens", historyTokens)
}
