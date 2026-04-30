// exercise_models.go is the pipeline-mode exercise orchestrator — the
// business logic behind `roboticus models exercise`. CLI commands and the
// `/api/models/exercise` route are thin connectors to this function so
// baselines reflect the same runtime request path.
//
// What lives here: warm-up sequencing, per-model iteration loop,
// per-prompt dispatch, quality scoring, per-intent aggregation,
// cross-model result accumulation.
//
// What lives in the caller (CLI): progress rendering, final report
// formatting, user confirmation, config loading, score flushing via
// admin API, model enumeration from config. Those are connector
// concerns — they change shape between CLI and HTTP surfaces, but the
// orchestration is identical.
//
// This is the v1.0.6 consolidation of the formerly separate
// `models exercise` and `models baseline` commands. They differed only
// in selector (one model vs many) and output shape (single-model
// detail vs cross-model comparison). Both are now one command that
// invokes this orchestrator; display is a thin shell around
// OnPromptFn callbacks and the returned ExerciseReport.

package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"roboticus/internal/core"
	"roboticus/internal/hostresources"
	"roboticus/internal/modelstate"
)

type ExerciseOutcomeClass string

const (
	ExerciseOutcomeCleanPass          ExerciseOutcomeClass = "clean_pass"
	ExerciseOutcomeSlowPass           ExerciseOutcomeClass = "slow_pass"
	ExerciseOutcomeProviderTimeout    ExerciseOutcomeClass = "provider_timeout"
	ExerciseOutcomeTransportError     ExerciseOutcomeClass = "transport_error"
	ExerciseOutcomeValidityAmbiguous  ExerciseOutcomeClass = "validity_ambiguous"
	ExerciseOutcomeEmptyResponse      ExerciseOutcomeClass = "empty_response"
	ExerciseOutcomeQualityGateFailure ExerciseOutcomeClass = "quality_gate_failure"
)

type ExerciseWarmupMode string

const (
	ExerciseWarmupAuto  ExerciseWarmupMode = "auto"
	ExerciseWarmupSkip  ExerciseWarmupMode = "skip"
	ExerciseWarmupForce ExerciseWarmupMode = "force"
)

// ModelSender is the pluggable transport for dispatching a single
// scored-prompt call against a model. It returns the response content,
// observed latency, optional turn/phase telemetry, and any transport
// error. The concrete implementation decides whether the call goes through
// the pipeline (CLI baseline flow) or directly to the LLM; the orchestrator
// doesn't care.
//
// Implementations MUST honor ctx cancellation. Callers rely on this
// for clean Ctrl-C behavior during long baseline runs.
type ModelSender func(ctx context.Context, model, content string, timeout time.Duration) (PromptDispatch, error)

// ExercisePhaseTimings captures prompt-level latency attribution. It is kept
// deliberately simple: model inference, tool execution, and residual framework
// overhead, plus optional per-stage timings for deeper RCA.
type ExercisePhaseTimings struct {
	TotalMs                int64            `json:"total_ms"`
	ModelInferenceMs       int64            `json:"model_inference_ms"`
	ToolExecutionMs        int64            `json:"tool_execution_ms"`
	FrameworkOverheadMs    int64            `json:"framework_overhead_ms"`
	InferenceAttempts      int              `json:"inference_attempts,omitempty"`
	ToolCallCount          int              `json:"tool_call_count,omitempty"`
	GuardRetryCount        int              `json:"guard_retry_count,omitempty"`
	VerifierRetryCount     int              `json:"verifier_retry_count,omitempty"`
	ReplaySuppressionCount int              `json:"replay_suppression_count,omitempty"`
	StageMs                map[string]int64 `json:"stage_ms,omitempty"`
}

// PromptDispatch is the transport result for one exercised prompt.
type PromptDispatch struct {
	ResponseText string
	LatencyMs    int64
	TurnID       string
	PhaseTimings *ExercisePhaseTimings
}

// PromptOutcome is the per-prompt result surfaced to the OnPromptFn
// callback. Carries enough context for the caller to render progress
// lines, update live dashboards, or persist per-prompt audit rows.
// The orchestrator has already categorized the outcome via Passed +
// Quality; callers use those rather than reinterpreting Content/Err.
type PromptOutcome struct {
	PromptIndex     int            // 1-based index within TotalPrompts for this model
	TotalPrompts    int            // iterations × len(ExerciseMatrix)
	Prompt          ExercisePrompt // the prompt that was dispatched
	Model           string
	Iteration       int // 1-based iteration number
	TotalIterations int
	// LatencyMs is whole prompt dispatch latency. ModelLatencyMs is the
	// model-attributable portion used by benchmark scorecards.
	LatencyMs       int64
	ModelLatencyMs  int64
	TurnID          string
	Content         string  // response text (empty on err)
	Quality         float64 // measured quality for evaluable rows
	Passed          bool    // true iff response is evaluable and meets the pass-quality floor
	OutcomeClass    ExerciseOutcomeClass
	Err             error // transport error, nil on success
	PhaseTimings    *ExercisePhaseTimings
	ResourceStart   *hostresources.Snapshot
	ResourceEnd     *hostresources.Snapshot
	ModelStateStart *modelstate.Snapshot
	ModelStateEnd   *modelstate.Snapshot
}

// OnPromptFn is invoked once per scored prompt AFTER the call
// returns (success, timeout, or error). Callers use this for
// streaming a result trailer onto the prefix line the orchestrator
// already printed. Pass nil if no per-prompt callback is needed —
// the orchestrator is silent by default (beyond the prefix print).
type OnPromptFn func(o PromptOutcome)

// ExerciseRequest bundles the parameters for one end-to-end exercise
// run against N models. All dispatch-shaped fields are pluggable so
// the orchestrator can be reused across CLI, HTTP, and future runtime
// callers without duplicating the iteration loop.
type ExerciseRequest struct {
	// Models is the list of model identifiers to exercise. Order is
	// preserved in the returned report. Must be non-empty.
	Models []string

	// IntentFilter optionally narrows the canonical ExerciseMatrix to a single
	// intent class. The exercise factory owns this filtering so connectors do
	// not drift into ad hoc prompt subsets.
	IntentFilter *IntentClass

	// PromptFilter optionally narrows the canonical ExerciseMatrix to one exact
	// row in the form INTENT:Cn, for RCA-grade scope diagnosis.
	PromptFilter string

	// Iterations is the number of passes each model makes through
	// ExerciseMatrix. Values < 1 are coerced to 1.
	Iterations int

	// SendPrompt is the transport for scored prompts. Required.
	SendPrompt ModelSender

	// SendWarmup is the transport for warm-up calls. Usually the
	// same underlying layer as SendPrompt but with a WarmupSender
	// signature (it receives a fixed prompt — WarmupPrompt). Required.
	SendWarmup WarmupSender

	// OnPrompt, if non-nil, is invoked for each scored prompt AFTER
	// the call returns. The orchestrator has already printed a
	// "[N/M] INTENT:Cx ... " prefix with spinner to req.Progress
	// before the call dispatched, so typical OnPrompt implementations
	// just print the result trailer (pass/fail + quality + latency)
	// ending with a newline. Callers that only need the final
	// report can leave this nil.
	OnPrompt OnPromptFn

	// Progress receives warm-up progress lines from RunWarmupStage.
	// Nil is treated as io.Discard. Independent from OnPrompt
	// because warm-up is a pre-scoring phase, not a scored prompt.
	Progress io.Writer

	// WarmupMode controls whether local warm-up is applied. Empty means auto:
	// local models warm, cloud models skip. Skip bypasses warm-up even for
	// local models; force warms every model.
	WarmupMode ExerciseWarmupMode

	// SampleModelState captures provider/model runtime state for the exact model
	// under test. It is optional but strongly recommended for benchmarks so
	// empty/failed rows can be attributed to real model readiness instead of
	// guessed after the fact.
	SampleModelState func(ctx context.Context, model string) *modelstate.Snapshot

	// IsLocal reports whether a model is local-hosted (and thus
	// should go through warm-up). Required — implementations read
	// config or router metadata to answer.
	IsLocal func(model string) bool

	// ModelTimeout returns the per-call timeout for a model.
	// Required. The warm-up cold-call gets 2× this value.
	ModelTimeout func(model string) time.Duration
}

// ModelExerciseResult is the per-model outcome of an ExerciseModels run.
// Carries the warm-up telemetry (separate from scored averages by
// design — see warmup_stage.go) alongside the aggregated scored data.
type ModelExerciseResult struct {
	Model string

	// Warmup is the two-call warm-up stage outcome. Zero-valued
	// when the model was cloud-hosted (Skipped=true) or when the
	// caller passed isLocal=false.
	Warmup WarmupStageResult

	// Scored prompt aggregate counters.
	Pass, Fail int
	AvgQuality float64

	// Per-intent breakdown. IntentQuality values are already-averaged
	// over the model's iterations × matrix-entries-for-that-intent.
	IntentQuality  map[string]float64
	Latencies      map[string][]int64 // intent → model-attributable observations, in ms
	PhaseLatencies map[string][]int64 // phase key → every observation, in ms
}

// ExerciseReport is the final aggregated outcome of an ExerciseModels
// call, one result per input model. Order matches ExerciseRequest.Models.
type ExerciseReport struct {
	Models []ModelExerciseResult
}

// ExerciseModels runs the full exercise flow for each requested model:
// warm-up → iterations × scored matrix → per-intent aggregation. The
// returned report is self-contained; callers render it without any
// further orchestrator calls.
//
// Error semantics: the orchestrator itself does not fail on individual
// prompt or model errors — those are captured on the per-model result
// (Fail counter, per-prompt Err via OnPrompt). A non-nil returned
// error indicates a usage mistake (empty Models list, nil SendPrompt,
// nil IsLocal, nil ModelTimeout) OR ctx cancellation. Transport and
// quality problems are DATA, not errors.
func ExerciseModels(ctx context.Context, req ExerciseRequest) (ExerciseReport, error) {
	// Validate required fields up front rather than allowing nil-deref
	// deep in the loop.
	if len(req.Models) == 0 {
		return ExerciseReport{}, errors.New("ExerciseModels: Models list is empty")
	}
	if req.SendPrompt == nil {
		return ExerciseReport{}, errors.New("ExerciseModels: SendPrompt is required")
	}
	if req.SendWarmup == nil {
		return ExerciseReport{}, errors.New("ExerciseModels: SendWarmup is required")
	}
	if req.IsLocal == nil {
		return ExerciseReport{}, errors.New("ExerciseModels: IsLocal is required")
	}
	if req.ModelTimeout == nil {
		return ExerciseReport{}, errors.New("ExerciseModels: ModelTimeout is required")
	}
	if req.IntentFilter != nil && !IsValidIntentClass(*req.IntentFilter) {
		return ExerciseReport{}, fmt.Errorf("ExerciseModels: invalid IntentFilter %d", *req.IntentFilter)
	}
	if req.WarmupMode == "" {
		req.WarmupMode = ExerciseWarmupAuto
	}
	if req.WarmupMode != ExerciseWarmupAuto && req.WarmupMode != ExerciseWarmupSkip && req.WarmupMode != ExerciseWarmupForce {
		return ExerciseReport{}, fmt.Errorf("ExerciseModels: invalid WarmupMode %q", req.WarmupMode)
	}
	if req.Iterations < 1 {
		req.Iterations = 1
	}
	// Normalize nil progress writer to io.Discard so downstream
	// helpers (RunWarmupStage, RunWithSpinner) can always write
	// unconditionally without nil guards. Tests that don't care
	// about progress output rely on this normalization.
	if req.Progress == nil {
		req.Progress = io.Discard
	}
	prompts := ExerciseMatrix
	if req.IntentFilter != nil {
		prompts = filterExerciseMatrix(*req.IntentFilter)
	}
	if strings.TrimSpace(req.PromptFilter) != "" {
		filtered, err := filterExerciseMatrixRow(prompts, req.PromptFilter)
		if err != nil {
			return ExerciseReport{}, err
		}
		prompts = filtered
	}
	if len(prompts) == 0 {
		return ExerciseReport{}, errors.New("ExerciseModels: no prompts matched the requested filter")
	}

	report := ExerciseReport{}

	for _, model := range req.Models {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		modelTimeout := req.ModelTimeout(model)
		mr := ModelExerciseResult{
			Model:          model,
			IntentQuality:  make(map[string]float64),
			Latencies:      make(map[string][]int64),
			PhaseLatencies: make(map[string][]int64),
		}

		// Warm-up BEFORE the scored matrix. Fires once per model;
		// all iterations share the warmed model. See warmup_stage.go
		// for the per-phase rationale.
		warmLocal := req.IsLocal(model)
		if req.WarmupMode == ExerciseWarmupSkip {
			mr.Warmup = WarmupStageResult{Skipped: true}
			_, _ = fmt.Fprintln(req.Progress, "    Warm-up: skipped (--warmup skip)")
			_, _ = fmt.Fprintln(req.Progress)
		} else {
			if req.WarmupMode == ExerciseWarmupForce {
				warmLocal = true
			}
			mr.Warmup = RunWarmupStage(ctx, req.Progress, model, warmLocal, modelTimeout, req.SendWarmup)
		}
		totalPrompts := len(prompts) * req.Iterations

		// Aggregation accumulators. Quality averages must use the same
		// denominator semantics as persisted scorecards: every evaluable scored
		// row counts. Quality-gate failures retain their measured quality instead
		// of disappearing from the average or collapsing to zero.
		intentSums := make(map[string]float64)
		intentCounts := make(map[string]int)
		var qualitySum float64
		var qualityCount int

		for iter := 1; iter <= req.Iterations; iter++ {
			for i, ep := range prompts {
				if err := ctx.Err(); err != nil {
					// Partial result is preserved on the report — the
					// caller sees whatever accumulated before cancel.
					if qualityCount > 0 {
						mr.AvgQuality = qualitySum / float64(qualityCount)
					}
					for intent, sum := range intentSums {
						if intentCounts[intent] > 0 {
							mr.IntentQuality[intent] = sum / float64(intentCounts[intent])
						}
					}
					report.Models = append(report.Models, mr)
					return report, err
				}

				promptNum := (iter-1)*len(prompts) + i + 1

				// Per the v1.0.6 "no silent blocking calls" rule:
				// print a prefix line and run the prompt dispatch
				// under a spinner so the operator has visible
				// feedback during the wait. core.RunWithSpinner is
				// a no-op on non-TTY progress writers, so piped /
				// logged output stays clean.
				prefix := fmt.Sprintf("    [%d/%d] %s:C%d ... ", promptNum, totalPrompts, ep.Intent.String(), ep.Complexity)
				var (
					dispatch        PromptDispatch
					err             error
					modelStateStart *modelstate.Snapshot
					modelStateEnd   *modelstate.Snapshot
				)
				if req.SampleModelState != nil {
					modelStateStart = req.SampleModelState(ctx, model)
				}
				resourceStart := hostresources.Sample(ctx)
				start := time.Now()
				promptTimeout := ExercisePromptTimeout(modelTimeout, ep)
				core.RunWithSpinner(req.Progress, prefix, func() {
					dispatch, err = req.SendPrompt(ctx, model, ep.Prompt, promptTimeout)
				})
				resourceEnd := hostresources.Sample(ctx)
				if req.SampleModelState != nil {
					modelStateEnd = req.SampleModelState(ctx, model)
				}
				// SendPrompt is allowed to return latencyMs==0 if it
				// doesn't track its own timing; fall back to our
				// start-based measurement so callers always see a
				// reasonable number.
				if dispatch.LatencyMs == 0 {
					dispatch.LatencyMs = time.Since(start).Milliseconds()
				}
				modelLatencyMs := modelAttributableLatencyMs(dispatch)

				intent := ep.Intent.String()
				mr.Latencies[intent] = append(mr.Latencies[intent], modelLatencyMs)
				appendPhaseLatencies(mr.PhaseLatencies, dispatch.PhaseTimings)

				outcome := PromptOutcome{
					PromptIndex:     promptNum,
					TotalPrompts:    totalPrompts,
					Prompt:          ep,
					Model:           model,
					Iteration:       iter,
					TotalIterations: req.Iterations,
					LatencyMs:       dispatch.LatencyMs,
					ModelLatencyMs:  modelLatencyMs,
					TurnID:          dispatch.TurnID,
					PhaseTimings:    dispatch.PhaseTimings,
					ResourceStart:   &resourceStart,
					ResourceEnd:     &resourceEnd,
					ModelStateStart: modelStateStart,
					ModelStateEnd:   modelStateEnd,
				}

				switch {
				case err != nil:
					mr.Fail++
					outcome.Err = err
					outcome.OutcomeClass = classifyExerciseFailure(err)
				case dispatch.ResponseText == "":
					mr.Fail++
					outcome.OutcomeClass = ExerciseOutcomeEmptyResponse
					// Distinct failure mode from transport error —
					// caller can differentiate via outcome.Err==nil
					// && !outcome.Passed.
				default:
					quality := ScoreExerciseResponse(ep, dispatch.ResponseText)
					outcome.Content = dispatch.ResponseText
					outcome.Quality = quality
					outcome.Passed = quality >= DefaultExercisePassQualityFloor
					if outcome.Passed {
						mr.Pass++
						outcome.OutcomeClass = classifyExercisePass(modelLatencyMs, promptTimeout)
					} else {
						mr.Fail++
						outcome.OutcomeClass = ExerciseOutcomeQualityGateFailure
					}
				}

				if exerciseOutcomeCountsAsEfficacyEvidence(outcome.OutcomeClass) {
					qualityCount++
					intentCounts[intent]++
					qualitySum += outcome.Quality
					intentSums[intent] += outcome.Quality
				}

				if req.OnPrompt != nil {
					req.OnPrompt(outcome)
				}
			}
		}

		if qualityCount > 0 {
			mr.AvgQuality = qualitySum / float64(qualityCount)
		}
		for intent, sum := range intentSums {
			if intentCounts[intent] > 0 {
				mr.IntentQuality[intent] = sum / float64(intentCounts[intent])
			}
		}

		report.Models = append(report.Models, mr)
	}

	return report, nil
}

func modelAttributableLatencyMs(dispatch PromptDispatch) int64 {
	if dispatch.PhaseTimings != nil && dispatch.PhaseTimings.ModelInferenceMs > 0 {
		return dispatch.PhaseTimings.ModelInferenceMs
	}
	return dispatch.LatencyMs
}

func classifyExercisePass(modelLatencyMs int64, promptTimeout time.Duration) ExerciseOutcomeClass {
	if promptTimeout > 0 && modelLatencyMs > 0 {
		threshold := int64(float64(promptTimeout.Milliseconds()) * 0.80)
		if threshold > 0 && modelLatencyMs >= threshold {
			return ExerciseOutcomeSlowPass
		}
	}
	return ExerciseOutcomeCleanPass
}

func classifyExerciseFailure(err error) ExerciseOutcomeClass {
	if err == nil {
		return ExerciseOutcomeQualityGateFailure
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ExerciseOutcomeProviderTimeout
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "client.timeout") ||
		strings.Contains(msg, "awaiting headers") {
		return ExerciseOutcomeProviderTimeout
	}
	return ExerciseOutcomeTransportError
}

func exerciseOutcomeCountsAsEfficacyEvidence(class ExerciseOutcomeClass) bool {
	switch class {
	case ExerciseOutcomeTransportError, ExerciseOutcomeProviderTimeout, ExerciseOutcomeValidityAmbiguous:
		return false
	default:
		return true
	}
}

func filterExerciseMatrix(intent IntentClass) []ExercisePrompt {
	filtered := make([]ExercisePrompt, 0, len(ExerciseMatrix))
	for _, prompt := range ExerciseMatrix {
		if prompt.Intent == intent {
			filtered = append(filtered, prompt)
		}
	}
	return filtered
}

func filterExerciseMatrixRow(prompts []ExercisePrompt, selector string) ([]ExercisePrompt, error) {
	intent, complexity, err := ParseExerciseRowSelector(selector)
	if err != nil {
		return nil, err
	}
	for _, prompt := range prompts {
		if prompt.Intent == intent && prompt.Complexity == complexity {
			return []ExercisePrompt{prompt}, nil
		}
	}
	return nil, fmt.Errorf("ExerciseModels: no prompt matched row selector %s", strings.ToUpper(strings.TrimSpace(selector)))
}

func appendPhaseLatencies(target map[string][]int64, timings *ExercisePhaseTimings) {
	if target == nil || timings == nil {
		return
	}
	target["MODEL_INFERENCE"] = append(target["MODEL_INFERENCE"], timings.ModelInferenceMs)
	target["TOOL_EXECUTION"] = append(target["TOOL_EXECUTION"], timings.ToolExecutionMs)
	target["FRAMEWORK_OVERHEAD"] = append(target["FRAMEWORK_OVERHEAD"], timings.FrameworkOverheadMs)
	if timings.TotalMs > 0 {
		target["TOTAL_PIPELINE"] = append(target["TOTAL_PIPELINE"], timings.TotalMs)
	}
}
