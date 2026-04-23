// retrieval_path.go is the M3.2 surface that lets the memory retriever
// declare which physical search leg actually answered a tier query, without
// taking a hard dependency on internal/pipeline (which would create an import
// cycle: pipeline already depends on memory).
//
// The contract is one annotation per covered tier per turn, written under
// "retrieval.path.<tier>" with a value drawn from the RetrievalPath* set:
//
//   retrieval.path.semantic     = "fts" | "vector" | "hybrid" |
//                                 "like_fallback" | "empty"
//   retrieval.path.procedural   = …
//   retrieval.path.relationship = …
//   retrieval.path.workflow     = …
//
// The pipeline trace recorder satisfies the RetrievalTracer interface via Go
// structural typing — its Annotate(key, value) method is the only thing this
// package needs. Production callers pass *pipeline.TraceRecorder; tests pass
// a fixture recorder defined in this package's tests.
//
// Concurrency note: the tracer travels via context.Context rather than a
// Retriever struct field. The Retriever instance is shared across turns, so
// per-turn ambient state on the struct itself would race; ctx values give us
// per-call scoping for free.

package memory

import "context"

// RetrievalTracer is the minimal annotation interface the memory tier methods
// need. *pipeline.TraceRecorder satisfies it implicitly.
type RetrievalTracer interface {
	Annotate(key string, value any)
}

// retrievalTracerKey is the unexported context key used by
// WithRetrievalTracer / retrievalTracerFromContext.
type retrievalTracerKey struct{}

// WithRetrievalTracer returns a context that carries tracer for retrieval-path
// annotations. Passing a nil tracer is a no-op and returns ctx unchanged so
// callers can hand off a context with or without an active recorder
// uniformly.
func WithRetrievalTracer(ctx context.Context, tracer RetrievalTracer) context.Context {
	if tracer == nil {
		return ctx
	}
	return context.WithValue(ctx, retrievalTracerKey{}, tracer)
}

// retrievalTracerFromContext returns the tracer set by WithRetrievalTracer,
// or nil if none was set on this context. The retriever tier methods call
// this once per query and emit a single annotation; not finding a tracer is
// a normal condition (smoke tests, ad-hoc CLI calls, etc.) and the helpers
// silently skip annotation in that case.
func retrievalTracerFromContext(ctx context.Context) RetrievalTracer {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(retrievalTracerKey{})
	if v == nil {
		return nil
	}
	t, _ := v.(RetrievalTracer)
	return t
}

// Retrieval path values. These are the legal values for the
// "retrieval.path.<tier>" annotation written by tier methods.
//
//	RetrievalPathFTS          — FTS5 leg returned the result; vector leg
//	                            either skipped (no embedding) or returned
//	                            nothing for this tier
//	RetrievalPathVector       — vector leg returned the result; FTS leg
//	                            returned nothing for this tier
//	RetrievalPathHybrid       — both legs contributed candidates that were
//	                            blended into the final result
//	RetrievalPathLikeFallback — both FTS and vector legs returned zero rows
//	                            for this tier and the residual LIKE safety
//	                            net produced the surfaced rows
//	RetrievalPathEmpty        — no leg produced any row; tier returned an
//	                            empty result (this is still annotated so
//	                            operators can distinguish "tier was queried
//	                            and found nothing" from "tier was not
//	                            queried at all")
const (
	RetrievalPathFTS          = "fts"
	RetrievalPathVector       = "vector"
	RetrievalPathHybrid       = "hybrid"
	RetrievalPathLikeFallback = "like_fallback"
	RetrievalPathEmpty        = "empty"
)

// Tier identifiers used as the namespaced suffix on retrieval.path.* keys.
// Kept as constants so callers and tests share one source of truth.
const (
	RetrievalTierSemantic     = "semantic"
	RetrievalTierProcedural   = "procedural"
	RetrievalTierRelationship = "relationship"
	RetrievalTierWorkflow     = "workflow"
)

// retrievalPathKey returns the trace annotation key for a tier. Centralised
// so tests can assert against one helper rather than hard-coding the prefix
// in N places.
func retrievalPathKey(tier string) string { return "retrieval.path." + tier }

// annotateRetrievalPath emits the per-tier retrieval-path annotation if the
// context carries a tracer. Safe to call with no tracer in scope — that case
// is the silent no-op required to keep ad-hoc callers (CLI, smoke tests)
// working without injection.
func annotateRetrievalPath(ctx context.Context, tier, path string) {
	tracer := retrievalTracerFromContext(ctx)
	if tracer == nil {
		return
	}
	tracer.Annotate(retrievalPathKey(tier), path)
}

// classifyHybridPath collapses (ftsHits, vectorHits) into a RetrievalPath*
// label. The blended HybridSearch result is one merged ranked list, so the
// tier methods inspect per-leg counts before merge to decide the label:
//
//   - both legs contributed     → RetrievalPathHybrid
//   - FTS only                  → RetrievalPathFTS
//   - vector only               → RetrievalPathVector
//   - neither                   → "" (caller should attempt LIKE fallback,
//     then annotate RetrievalPathLikeFallback or
//     RetrievalPathEmpty as appropriate)
//
// The empty-string return for the all-zero case is intentional: the caller
// runs the safety-net query first and then annotates a more specific label
// based on that query's outcome.
func classifyHybridPath(ftsHits, vectorHits int) string {
	switch {
	case ftsHits > 0 && vectorHits > 0:
		return RetrievalPathHybrid
	case ftsHits > 0:
		return RetrievalPathFTS
	case vectorHits > 0:
		return RetrievalPathVector
	default:
		return ""
	}
}
