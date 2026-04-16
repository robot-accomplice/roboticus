// exercise_models.go is the pipeline-mode exercise orchestrator — the
// business logic behind `roboticus models exercise`. CLI commands in
// cmd/models are thin connectors to this function; the HTTP route
// /api/models/exercise uses a different orchestrator (RunExercise, in
// exercise.go) that bypasses the pipeline for direct-LLM measurement.
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
	"time"

	"roboticus/internal/core"
)

// ModelSender is the pluggable transport for dispatching a single
// scored-prompt call against a model. Returns the response content,
// observed latency in milliseconds, and any transport error. The
// concrete implementation decides whether the call goes through the
// pipeline (CLI baseline flow) or directly to the LLM; the
// orchestrator doesn't care.
//
// Implementations MUST honor ctx cancellation. Callers rely on this
// for clean Ctrl-C behavior during long baseline runs.
type ModelSender func(ctx context.Context, model, content string, timeout time.Duration) (response string, latencyMs int64, err error)

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
	LatencyMs       int64
	Content         string  // response text (empty on err)
	Quality         float64 // 0 when Passed=false
	Passed          bool    // true iff response non-empty AND no error
	Err             error   // transport error, nil on success
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
	IntentQuality map[string]float64
	Latencies     map[string][]int64 // intent → every observation, in ms
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

	report := ExerciseReport{}

	for _, model := range req.Models {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		modelTimeout := req.ModelTimeout(model)
		mr := ModelExerciseResult{
			Model:         model,
			IntentQuality: make(map[string]float64),
			Latencies:     make(map[string][]int64),
		}

		// Warm-up BEFORE the scored matrix. Fires once per model;
		// all iterations share the warmed model. See warmup_stage.go
		// for the per-phase rationale.
		mr.Warmup = RunWarmupStage(ctx, req.Progress, model, req.IsLocal(model), modelTimeout, req.SendWarmup)

		totalPrompts := len(ExerciseMatrix) * req.Iterations

		// Aggregation accumulators. Per-intent sums and counts so the
		// final average isn't biased by intent-class sample-size imbalance.
		intentSums := make(map[string]float64)
		intentCounts := make(map[string]int)
		var qualitySum float64
		var qualityCount int

		for iter := 1; iter <= req.Iterations; iter++ {
			for i, ep := range ExerciseMatrix {
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

				promptNum := (iter-1)*len(ExerciseMatrix) + i + 1

				// Per the v1.0.6 "no silent blocking calls" rule:
				// print a prefix line and run the prompt dispatch
				// under a spinner so the operator has visible
				// feedback during the wait. core.RunWithSpinner is
				// a no-op on non-TTY progress writers, so piped /
				// logged output stays clean.
				prefix := fmt.Sprintf("    [%d/%d] %s:C%d ... ", promptNum, totalPrompts, ep.Intent.String(), ep.Complexity)
				var (
					content   string
					latencyMs int64
					err       error
				)
				start := time.Now()
				core.RunWithSpinner(req.Progress, prefix, func() {
					content, latencyMs, err = req.SendPrompt(ctx, model, ep.Prompt, modelTimeout)
				})
				// SendPrompt is allowed to return latencyMs==0 if it
				// doesn't track its own timing; fall back to our
				// start-based measurement so callers always see a
				// reasonable number.
				if latencyMs == 0 {
					latencyMs = time.Since(start).Milliseconds()
				}

				intent := ep.Intent.String()
				mr.Latencies[intent] = append(mr.Latencies[intent], latencyMs)

				outcome := PromptOutcome{
					PromptIndex:     promptNum,
					TotalPrompts:    totalPrompts,
					Prompt:          ep,
					Model:           model,
					Iteration:       iter,
					TotalIterations: req.Iterations,
					LatencyMs:       latencyMs,
				}

				switch {
				case err != nil:
					mr.Fail++
					outcome.Err = err
				case content == "":
					mr.Fail++
					// Distinct failure mode from transport error —
					// caller can differentiate via outcome.Err==nil
					// && !outcome.Passed.
				default:
					mr.Pass++
					quality := ScoreExerciseResponse(ep, content)
					qualitySum += quality
					qualityCount++
					intentSums[intent] += quality
					intentCounts[intent]++
					outcome.Content = content
					outcome.Quality = quality
					outcome.Passed = true
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
