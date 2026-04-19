package llm

import "context"

// RoutingTracer is the minimal annotation surface the LLM service needs to
// report routing decisions without depending on the pipeline package.
type RoutingTracer interface {
	Annotate(key string, value any)
}

type routingTracerKey struct{}

// WithRoutingTracer attaches a routing tracer to ctx. Production callers pass
// *pipeline.TraceRecorder; tests can pass a fixture that satisfies Annotate.
func WithRoutingTracer(ctx context.Context, tracer RoutingTracer) context.Context {
	if tracer == nil {
		return ctx
	}
	return context.WithValue(ctx, routingTracerKey{}, tracer)
}

func routingTracerFromContext(ctx context.Context) RoutingTracer {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(routingTracerKey{})
	if v == nil {
		return nil
	}
	tr, _ := v.(RoutingTracer)
	return tr
}

func annotateRoutingDecision(ctx context.Context, router *Router, req *Request, target RouteTarget) {
	tr := routingTracerFromContext(ctx)
	if tr == nil || router == nil || req == nil {
		return
	}

	candidates := make([]string, 0, len(router.Targets()))
	for _, rt := range router.Targets() {
		model := ModelSpecForTarget(rt)
		if model == "" {
			model = rt.Model
		}
		if model != "" {
			candidates = append(candidates, model)
		}
	}

	winner := ModelSpecForTarget(target)
	if winner == "" {
		winner = target.Model
	}
	mode := "heuristic"
	if router.MetascoreSelector != nil {
		mode = "metascore"
	}

	tr.Annotate("inference.routing.candidates", candidates)
	tr.Annotate("inference.routing.winner", winner)
	tr.Annotate("inference.routing.winner_score", 0.0)
	tr.Annotate("inference.routing.mode", mode)
	tr.Annotate("inference.routing.trace_source", "actual_request")
	tr.Annotate("inference.routing.request_message_count", len(req.Messages))
	tr.Annotate("inference.routing.request_tool_count", len(req.Tools))
}
