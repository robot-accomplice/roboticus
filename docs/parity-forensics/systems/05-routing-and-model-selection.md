# System 05: Routing and Model Selection

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-16
- Related release: v1.0.6

## Why This System Matters

Routing chooses the model that will see the final request artifact. If routing
observability drifts from real runtime selection, operators and future
remediations will optimize the wrong thing.

This system also sits directly on top of System 01: request construction.
Routing parity is not only "which heuristic exists," but "which exact request
shape the router actually evaluated."

## Scope

In scope:

- runtime router selection for normal and streaming inference
- routing trace annotations
- model-selection audit events
- the request shape used at each routing call site
- interaction between request shaping and complexity estimation

Out of scope:

- provider fallback behavior after model selection
- tool pruning itself
- retrieval-tier routing (that belongs to memory systems)

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Request prepared before downstream stages | `crates/roboticus-pipeline/src/core/context_builder.rs:472-509` |
| Unified intent classification ownership | `crates/roboticus-pipeline/src/core/context_builder.rs:482-509` |
| Tool-search stats propagated into inference context | `crates/roboticus-pipeline/src/context/inference.rs:306-373` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Router heuristics | `internal/llm/router.go:20-194` |
| Runtime selection in complete path | `internal/llm/service.go:214-218` |
| Runtime selection in stream path | `internal/llm/service.go:422-425` |
| Pipeline trace routing annotation | `internal/pipeline/pipeline_run_stages.go:683-716` |
| Retrieval intents carried per-call | `internal/agent/memory/intents_context.go:1-52`, `internal/agent/memory/retrieval.go:236-244` |

## Live Go Path

Current observed state on 2026-04-16:

1. The actual LLM service routes on the full `llm.Request` in both the normal
   and stream paths.
2. The pipeline trace annotation path does not use that same full request; it
   reconstructs a synthetic request containing only the last user message.
3. Memory retrieval intents were previously a shared mutable field on the
   retriever, but that specific concurrency drift has now been corrected by
   moving intents onto `context.Context` per retrieval call.

So the biggest remaining routing risk is not the core heuristic function
itself, but mismatched request shapes across the runtime, traces, and audit
events.

## Artifact Boundary

The artifact boundary for this system is:

- the selected model on the actual inference request
- the routing trace annotations emitted for the same turn
- any model-selection audit event derived from that turn

Parity is not satisfied unless those three agree on the effective input.

## Success Criteria

- Closure artifact(s):
  - the selected runtime model on the actual inference request
  - routing trace annotations for that same request
  - persisted model-selection audit events for that same turn
- Live-path proof:
  - runtime-facing tests or traces prove the router sees the same effective
    request shape in complete and stream paths
  - trace/audit surfaces are shown to reflect the actual winner decision, not a
    reconstructed approximation
  - any metascore/weight annotations are attached to the same winner that
    served inference
- Blocking conditions:
  - any authoritative trace or audit path still routes a synthetic user-only
    request while runtime inference routes the full request
  - operators can see different "winner" stories for the same turn depending on
    which route/surface they inspect
- Accepted deviations:
  - any retained diagnostic-only routing estimate must be explicitly marked
    non-authoritative and kept separate from runtime truth

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-05-001 | P1 | Routing trace request shape drift | Downstream observability should describe the same prepared request that inference uses | Routing trace annotation is now emitted at the actual selection site inside `llm.Service` using the final `llm.Request`; the old synthetic pipeline winner path has been removed | Improvement / synthesis | Closed (retain) | `internal/llm/routing_trace.go`, `internal/llm/service.go:225-226`, `internal/llm/service.go:438-439`, `internal/pipeline/pipeline_run_stages.go:766-789` |
| SYS-05-002 | P1 | Model-selection audit event request shape drift | Audit path should reflect the same routing decision surface as runtime inference | Routed model-selection events are now persisted from `llm.Service` using the actual request plus turn/session/channel context. The older synthetic `SelectAndAuditModel` path has been deleted, leaving one live audit owner | Improvement / synthesis | Closed (retain) | `internal/llm/service.go:225-227`, `internal/llm/service.go:438-440`, `internal/llm/model_selection_event_test.go`, `internal/core/context_keys.go`, `internal/pipeline/pipeline_stages.go:31-35` |
| SYS-05-003 | P2 | Complexity estimation depends on final request shape | Router complexity heuristics include message count, tool count, and content signals | Any path that omits tools/history from the routed request will understate complexity versus real inference | Degradation | Open | `internal/llm/router.go:199-260` plus synthetic-call sites above |
| SYS-05-004 | P1 | Per-call retrieval intents previously leaked across turns | Routing-adjacent retrieval planning should not use shared mutable intent state | This was a real bug, now fixed by context-carried intents | Improvement | Closed / retain as evidence | `internal/agent/memory/intents_context.go:1-52`, `internal/agent/memory/retrieval.go:236-244` |
| SYS-05-005 | P2 | Metascore / weight tracing can become decorrelated from the actual routed request when the winner is chosen from a synthetic request | Routing observability should expose the same effective inputs that produced the selected model and weight application | Go traces routing mode and weights, but the winner is still computed from a user-only reconstruction in the trace path | Degradation | Open | `internal/pipeline/pipeline_run_stages.go:683-716`, `internal/pipeline/trace.go:384-401`, `internal/llm/router.go:150-174` |

## Intentional Deviations

None accepted yet.

## Remediation Notes

Expected closure conditions:

- routing trace uses the same effective request or a structured projection of it
- model-selection audit events are sourced from the same decision as runtime
  inference
- any "user-only" shortcut path is either removed or explicitly documented as
  non-authoritative
- metascore/weight annotations remain attached to the same winner decision
  rather than a separately reconstructed approximation

## Downstream Systems Affected

- System 01: Request construction and context assembly
- System 04: Verification, guards, and post-processing
- System 09: Admin, dashboard, and observability surfaces

## Open Questions

- Should routing trace annotation move to the point where the final
  `llm.Request` is available rather than reconstructing an approximation?
- Is there any legitimate operator use case for a user-only routing estimate, or
  should that be demoted to a clearly non-authoritative diagnostic if retained?

## Progress Log

- 2026-04-16: Initialized routing/model-selection system document.
- 2026-04-16: Recorded that the core router selects on the full request, but
  trace and audit paths still use synthetic user-only requests.
- 2026-04-16: Recorded retrieval intent context migration as a closed
  routing-adjacent improvement so future agents do not reopen that already
  remediated bug.
- 2026-04-16: Deepened the routing seam from "trace drift" to "three different
  routing stories" — runtime inference, trace annotation, and persisted audit
  events currently do not share one authoritative request shape.
- 2026-04-17: Closed SYS-05-001. Routing winner/candidate annotations now come
  from the real selection site inside `llm.Service` via a trace-recorder
  context hook, so the trace reflects the final `llm.Request` rather than a
  synthetic user-only reconstruction. The pipeline stage keeps only stable
  routing-weight annotations plus explicit override metadata.
- 2026-04-17: Closed SYS-05-002 for the routed path. `llm.Service` now records
  model-selection events directly from the actual routed request when the
  pipeline threads turn/session/channel context into inference. This aligns
  persisted audit events with runtime truth instead of relying on a
  user-only reconstruction helper.
- 2026-04-17: Deleted the dead `internal/pipeline/inference_runner.go`
  scaffolding after the actual-request model-selection event path was proven
  live. This removes a fake second routing/audit owner from the codebase.
