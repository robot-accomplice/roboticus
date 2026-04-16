// warmup_stage.go owns the orchestration of warm-up calls — the two-call
// cold → warm-transition → ready sequence that precedes any scored
// exercise or, at runtime, any known-cold invocation. Orchestration lives
// here (internal/llm) rather than in cmd/models so both the CLI baseline
// harness AND the future runtime cold-start predictor call into the
// same business logic. CLI and runtime are thin connectors; warm-up is
// the factory.
//
// The transport is pluggable via WarmupSender. CLI callers supply an
// HTTP-based sender that posts to /api/agent/message; runtime callers
// (the predictor when it lands) will supply an in-process pipeline
// sender. Neither calling surface duplicates the orchestration.

package llm

import (
	"context"
	"fmt"
	"io"
	"time"

	"roboticus/internal/core"
)

// WarmupSender is the injectable transport for a single warm-up call.
// The orchestrator (RunWarmupStage) invokes it twice per model with
// different timeouts (extended for cold, normal for warm-transition)
// and records the outcomes. Implementations must be goroutine-safe if
// callers plan to warm multiple models in parallel; the orchestration
// itself is serial per model so a single invocation is sequential.
type WarmupSender func(ctx context.Context, model string, timeout time.Duration) WarmupResult

// WarmupStageResult captures the full outcome of the two-call warm-up
// sequence for one model. Callers carry these values forward onto
// whatever result struct they use (baseline harness modelResult,
// runtime metascore dimensions, etc.) — warm-up never touches the
// scored-prompt latency buckets directly.
type WarmupStageResult struct {
	// Skipped is true when the model wasn't warmed (cloud-hosted;
	// caller passed isLocal=false). Both latency fields are 0 in
	// this case.
	Skipped bool

	// ColdStartMs is the wall-clock latency of the first warm-up
	// call. This is the "model wasn't resident yet" number: weight
	// load + KV cache allocation + pipeline init. For timed-out
	// calls, this is the timeout value as a lower bound (see
	// ColdStartTimedOut).
	ColdStartMs       int64
	ColdStartTimedOut bool

	// WarmTransitionMs is the wall-clock latency of the second
	// warm-up call. Expected to land close to steady-state once
	// warm-up #1 completes. If it doesn't, WarmTransitionOK is
	// false — the warm-up didn't take and scored data should be
	// treated with skepticism.
	WarmTransitionMs int64
	WarmTransitionOK bool
}

// warmTransitionOKCeilingMs is the heuristic upper bound for a
// healthy warm-transition call. If call #2 is still over this after
// call #1 completed, something in the warm-up didn't prime fully
// (tokenizer not cached? KV cache evicted between calls?) and the
// operator should know before 30 minutes of scored data accrues.
const warmTransitionOKCeilingMs = int64(30_000)

// RunWarmupStage orchestrates the two-call warm-up sequence for model.
// Parameters:
//   - ctx: dispatched into each WarmupSender call; respects cancellation.
//   - progress: optional io.Writer for operator-visible progress lines
//     (CLI plumbs os.Stdout here; non-interactive callers pass
//     io.Discard or a buffer).
//   - model: the model identifier the sender understands.
//   - isLocal: when false, the stage is a no-op (returns Skipped=true);
//     callers determine locality from whatever source is authoritative
//     in their context (CLI reads config map, runtime reads Router
//     metadata).
//   - modelTimeout: the timeout the caller uses for a typical scored
//     call. Warm-up #1 gets 2× this as extended cold-start tolerance;
//     warm-up #2 uses it unchanged.
//   - send: the WarmupSender transport.
//
// Returns WarmupStageResult; never returns an error — transport errors
// are captured per-call and reflected in the result struct so callers
// can record lower-bound observations rather than drop samples.
func RunWarmupStage(
	ctx context.Context,
	progress io.Writer,
	model string,
	isLocal bool,
	modelTimeout time.Duration,
	send WarmupSender,
) WarmupStageResult {
	if progress == nil {
		progress = io.Discard
	}
	out := WarmupStageResult{}

	if !isLocal {
		out.Skipped = true
		fmt.Fprintf(progress, "    Warm-up: skipped (cloud model)\n\n")
		return out
	}

	// Warm-up #1: cold. Extended timeout — the first call may pay
	// model-load cost that scales with model size and disk speed.
	// Runs under core.RunWithSpinner per the v1.0.6 "no silent
	// blocking calls" rule. Non-TTY progress writers get no spinner
	// output (see core.RunWithSpinner for TTY detection).
	coldTimeout := 2 * modelTimeout
	var coldRes WarmupResult
	core.RunWithSpinner(progress,
		fmt.Sprintf("    Warm-up 1/2 (cold, timeout: %s): ", coldTimeout),
		func() {
			coldRes = send(ctx, model, coldTimeout)
		})
	out.ColdStartMs = coldRes.LatencyMs
	out.ColdStartTimedOut = coldRes.TimedOut
	switch {
	case coldRes.TimedOut:
		fmt.Fprintf(progress, "TIMEOUT  (cold-start exceeded %s — recorded as lower bound)\n", coldTimeout)
	case coldRes.Err != nil:
		fmt.Fprintf(progress, "ERROR  %v\n", coldRes.Err)
	default:
		fmt.Fprintf(progress, "%.1fs\n", float64(coldRes.LatencyMs)/1000.0)
	}

	// Warm-up #2: warm-transition. Normal timeout. Confirms the
	// warm-up took.
	var warmRes WarmupResult
	core.RunWithSpinner(progress,
		fmt.Sprintf("    Warm-up 2/2 (warm-transition, timeout: %s): ", modelTimeout),
		func() {
			warmRes = send(ctx, model, modelTimeout)
		})
	out.WarmTransitionMs = warmRes.LatencyMs
	out.WarmTransitionOK = !warmRes.TimedOut && warmRes.Err == nil && warmRes.LatencyMs < warmTransitionOKCeilingMs
	switch {
	case warmRes.TimedOut:
		fmt.Fprintf(progress, "TIMEOUT  (warm-up didn't take — scored prompts may be unreliable)\n")
	case warmRes.Err != nil:
		fmt.Fprintf(progress, "ERROR  %v\n", warmRes.Err)
	default:
		marker := "OK"
		if !out.WarmTransitionOK {
			marker = "SLOW — warm-up may not have fully primed the model"
		}
		fmt.Fprintf(progress, "%.1fs  (%s)\n", float64(warmRes.LatencyMs)/1000.0, marker)
	}
	fmt.Fprintln(progress)
	return out
}
