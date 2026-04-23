// warmup.go defines the canonical warm-up prompt used by the baseline
// harness (cmd/models/models.go) and — once wired — by the runtime
// cold-start predictor before dispatching a known-cold model.
//
// Why a dedicated prompt vs reusing one from ExerciseMatrix:
//   * The matrix prompts are scored against intent-specific rubrics.
//     A warm-up call going through one of them would produce a quality
//     score we'd have to filter out of metascore computation.
//   * The warm-up prompt's purpose is pure cost amortization — load
//     the model weights, spin up KV cache, prime the tokenizer, warm
//     the pipeline stages. The content doesn't matter; the wall-clock
//     time does.
//   * A trivial, deterministic prompt keeps warm-up cost bounded (few
//     input tokens, few output tokens) and makes the cold-start
//     latency measurement comparable across runs.
//
// The prompt is shaped to produce exactly one short response through
// the full pipeline. If operators ever want to change the shape (e.g.,
// to exercise tool-calling as part of warm-up), change it here in ONE
// place rather than scattering warm-up prompts across callers.

package llm

// WarmupPrompt is the trivial prompt issued during warm-up calls.
//
// Design notes:
//   - "Reply with just: ready" constrains output to ~1 token so the
//     warm-up doesn't spend time generating long responses — we only
//     need the generation loop to start.
//   - Phrasing is unambiguous so every model (including strict local
//     ones) produces a terminating response rather than a rambling
//     introduction.
//   - No tool-calling hint, no reasoning hint — warm-up exercises the
//     "generate a short response" baseline path that's common to
//     every downstream call type.
const WarmupPrompt = "Reply with just: ready"

// WarmupResult captures one warm-up call's observed latency plus any
// error. Scored separately from ExerciseMatrix results because warm-up
// calls don't feed the quality-dimension metascore — only the
// cold-start-latency and warm-transition-latency dimensions.
type WarmupResult struct {
	// LatencyMs is the wall-clock time for the call. Pegged at the
	// timeout value if the call didn't complete — callers must check
	// TimedOut to distinguish that case from a genuine observation.
	LatencyMs int64

	// TimedOut is true when the warm-up call hit the timeout without
	// returning. For cold-start measurement this is still a data point
	// ("cold-start exceeds timeout") — callers should record the
	// lower bound rather than drop the sample.
	TimedOut bool

	// Err carries any non-timeout error (connection refused, invalid
	// response, etc.). Callers use this to distinguish "model cold but
	// still responsive" from "model genuinely broken."
	Err error
}
