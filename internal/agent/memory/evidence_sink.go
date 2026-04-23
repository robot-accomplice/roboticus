// evidence_sink.go threads the typed verification-evidence artifact
// from Retriever.Retrieve back to the caller (pipeline Stage 8.5)
// without expanding the MemoryRetriever interface.
//
// The pattern mirrors WithRetrievalTracer and WithIntents — a per-call
// context value that serves as a sink the retriever writes into and
// the caller reads from. This keeps the interface contract unchanged
// (Retrieve still just returns a string) while giving structured
// consumers (verifier) a format-independent view of the same data.
//
// Why a sink rather than a return-value extension:
//   * The MemoryRetriever interface is already wired through a dozen
//     test doubles, mocks, and adapters. Adding a method would cascade
//     into every one of them.
//   * The sink is opt-in: callers that don't care (smoke tests,
//     ad-hoc tools) don't pay the wiring cost. Callers that do care
//     (pipeline) attach a sink and read it back.
//   * Context values carry per-call ambient state in Go idiomatically.
//     Pair this with WithIntents and WithRetrievalTracer — all three
//     capture the same conceptual "this request's sidecar data."

package memory

import (
	"context"

	"roboticus/internal/session"
)

// EvidenceSink is a single-slot holder for the structured verification
// evidence artifact Retriever.Retrieve produces. The pipeline
// instantiates an empty sink per request, attaches it to ctx via
// WithEvidenceSink, calls Retrieve, then reads the populated pointer
// back. A nil sink (no WithEvidenceSink call) is the silent-drop
// default for callers that don't need the typed artifact.
type EvidenceSink struct {
	// Evidence is set by the retriever when structured assembly
	// completes. Nil after construction; populated on Retrieve
	// success; left nil on early-return paths (empty query, broken
	// store).
	Evidence *session.VerificationEvidence
}

// evidenceSinkKey is the unexported context key.
type evidenceSinkKey struct{}

// WithEvidenceSink returns ctx carrying sink. Nil sinks are a no-op
// (returning ctx unchanged) so callers can branch on availability
// without re-wiring.
func WithEvidenceSink(ctx context.Context, sink *EvidenceSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, evidenceSinkKey{}, sink)
}

// evidenceSinkFromContext returns the sink set by WithEvidenceSink or
// nil if none was attached. The retriever's typed-evidence hand-off
// checks this once per call.
func evidenceSinkFromContext(ctx context.Context) *EvidenceSink {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(evidenceSinkKey{})
	if v == nil {
		return nil
	}
	sink, _ := v.(*EvidenceSink)
	return sink
}
