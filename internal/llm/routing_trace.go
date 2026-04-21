package llm

import (
	"context"
	"strings"
)

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
	roleCandidates := make([]string, 0, len(router.Targets()))
	for _, rt := range filterTargetsForRole(router.Targets(), req) {
		model := ModelSpecForTarget(rt)
		if model == "" {
			model = rt.Model
		}
		if model != "" {
			roleCandidates = append(roleCandidates, model)
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
	tr.Annotate("inference.routing.role_eligible_candidates", roleCandidates)
	tr.Annotate("inference.routing.winner", winner)
	tr.Annotate("inference.routing.winner_score", 0.0)
	tr.Annotate("inference.routing.mode", mode)
	tr.Annotate("inference.routing.trace_source", "actual_request")
	tr.Annotate("inference.routing.agent_role", normalizeAgentRole(req.AgentRole))
	tr.Annotate("inference.routing.request_message_count", len(req.Messages))
	tr.Annotate("inference.routing.request_tool_count", len(req.Tools))
	tr.Annotate("inference.routing.policy_state", normalizeModelState(target.State))
	tr.Annotate("inference.routing.policy_source", strings.TrimSpace(target.PolicySource))
	tr.Annotate("inference.routing.primary_reason_code", strings.TrimSpace(target.PrimaryReasonCode))
	if len(target.ReasonCodes) > 0 {
		tr.Annotate("inference.routing.reason_codes", append([]string(nil), target.ReasonCodes...))
	}
	if humanReason := strings.TrimSpace(target.HumanReason); humanReason != "" {
		tr.Annotate("inference.routing.human_reason", humanReason)
	}
	if len(target.EvidenceRefs) > 0 {
		tr.Annotate("inference.routing.evidence_refs", append([]string(nil), target.EvidenceRefs...))
	}
	tr.Annotate("inference.routing.live_routable", liveRoutingAllowedForState(target.State))
	tr.Annotate("inference.routing.benchmark_eligible", benchmarkAllowedForState(target.State))
	tr.Annotate("inference.routing.orchestrator_eligible", target.OrchestratorEligible)
	tr.Annotate("inference.routing.subagent_eligible", target.SubagentEligible)
	if reason := strings.TrimSpace(target.EligibilityReason); reason != "" {
		tr.Annotate("inference.routing.eligibility_reason", reason)
	}
}
