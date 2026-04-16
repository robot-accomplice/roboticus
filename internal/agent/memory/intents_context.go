// intents_context.go threads per-call intent classification through the
// retrieval path via context values, matching the same pattern used by
// retrieval_path.go for RetrievalTracer.
//
// Why this is a context value instead of a field on *Retriever: the
// Retriever is constructed once at daemon startup and shared across every
// request. Pre-v1.0.6 the daemon adapter mutated a `mr.intents` slice on
// the shared Retriever before each Retrieve call — turn A's SetIntents
// could race with turn B's read in router.Plan(sg.Question, mr.intents),
// corrupting retrieval routing under concurrent traffic.
//
// The v1.0.6 architecture audit flagged this as a P1 correctness bug.
// The fix: drop SetIntents and the mr.intents field entirely, carry
// intents on context.Context for the duration of one retrieval call.
// Context values are per-call ambient state by design — they're the
// idiomatic Go answer to "this data travels with the request."

package memory

import "context"

// intentsKey is the unexported context key used by WithIntents /
// intentsFromContext.
type intentsKey struct{}

// WithIntents returns ctx carrying the intent classification results for
// the current retrieval call. Passing nil or empty intents is a no-op and
// returns ctx unchanged; retrieval code paths that don't have intent
// classification available (ad-hoc callers, smoke tests) continue to work
// with the router's default plan.
func WithIntents(ctx context.Context, intents []IntentSignal) context.Context {
	if len(intents) == 0 {
		return ctx
	}
	return context.WithValue(ctx, intentsKey{}, intents)
}

// intentsFromContext returns the intents set by WithIntents, or nil if
// none. The router.Plan call inside Retrieve reads this once per subgoal;
// a nil return is a normal condition and router.Plan handles it by
// returning a default plan.
func intentsFromContext(ctx context.Context) []IntentSignal {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(intentsKey{})
	if v == nil {
		return nil
	}
	intents, _ := v.([]IntentSignal)
	return intents
}
