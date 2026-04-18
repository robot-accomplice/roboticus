# System 09: Admin, Dashboard, and Observability Surfaces

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-16
- Related release: v1.0.6

## Why This System Matters

This family does not define agent behavior directly, but it defines what
operators and future engineers believe the system is doing. A drift here can
turn a healthy backend into an untrustworthy product because the traces, admin
surfaces, and dashboard copy are describing the wrong runtime path.

This family should be audited after backend truth is reasonably stable, but it
still deserves a seeded doc now because several existing drifts already depend
on observability surfaces:

- routing trace inputs versus actual routed request
- MCP readiness claims versus validation scope
- system warnings / dashboard contract stability

## Scope

In scope:

- pipeline traces and observability routes
- admin/runtime routes that summarize system state
- dashboard-critical response shapes
- system warnings surfaces
- release-truth interactions where observability docs or endpoints make claims
  about live behavior

Out of scope:

- core runtime inference logic itself
- installer/update flows
- MCP transport behavior itself

## Rust / Baseline Anchors

The key baseline here is that observability must tell the truth about the live
runtime. Where Rust-specific route or trace shapes matter, they should be added
systematically during the deeper pass.

## Go Source Anchors

| Concern | Go / doc file(s) |
|---------|-------------------|
| Trace routes | `internal/api/routes/traces.go`, `internal/api/routes/observability.go` |
| WebSocket topic snapshots | `internal/api/ws_topics.go`, `internal/api/ws_protocol.go` |
| Pipeline trace helpers | `internal/pipeline/trace.go`, `internal/pipeline/pipeline_run_stages.go` |
| Dashboard/system warning shape tests | `internal/api/routes/system_warnings_test.go`, `internal/api/response_shape_test.go` |
| Release-facing truth surfaces | `docs/releases/v1.0.6-release-notes.md`, `docs/architecture-gap-report.md` |

## Live Go Path

Current observed state on 2026-04-16:

1. There are strong route-shape and system-warning tests protecting several
   dashboard-critical JSON contracts.
2. Trace infrastructure is rich and increasingly structured.
3. Some observability outputs still describe approximations rather than the
   exact runtime artifact, especially around routing selection inputs.
4. Release docs remain part of this family because they are operator-facing
   observability/truth surfaces in practice.

## Artifact Boundary

The artifacts for this system are:

- trace rows and stage annotations
- admin/runtime API JSON shapes
- dashboard-critical warning/summary payloads
- release-facing operational claims

Parity/truth is not satisfied unless those artifacts accurately reflect live
runtime behavior.

## Success Criteria

- Closure artifact(s):
  - canonical trace rows / stage annotations
  - canonical admin/runtime JSON responses
  - WebSocket snapshot payloads
  - release-facing truth surfaces that summarize runtime behavior
- Live-path proof:
  - route-shape tests and trace evidence prove the documented operator surfaces
    are generated from the same runtime facts the backend actually used
  - overlapping surfaces are mapped so a reader can tell which one is
    canonical for a given signal
  - release docs and observability routes are cross-checked against current
    backend behavior after remediation
- Blocking conditions:
  - a canonical operator-facing surface still reports an approximation while
    another surface reports runtime truth
  - route overlap remains undocumented enough that operators can draw the wrong
    conclusion from the wrong endpoint
  - release-facing docs drift from the backend again
- Accepted deviations:
  - richer trace namespaces, WebSocket snapshot reuse, and explicit confidence
    caveats may remain only if they are documented as intentional truth-surface
    improvements

## Divergence Register

| ID | Priority | Concern | Baseline / desired behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|-----------------------------|-------------|----------------|--------|----------|
| SYS-09-001 | P1 | Routing trace annotations do not yet reflect the same request shape runtime routing uses | Observability should describe the actual routed request | Current trace path still uses a synthetic user-only request in at least one path | Degradation | Open / depends on System 05 | `internal/pipeline/pipeline_run_stages.go:683-716`, `internal/llm/service.go:214-218` |
| SYS-09-002 | P1 | Release docs are part of the observability contract and must stay synchronized | Operator-facing docs should match runtime truth | This has drifted repeatedly in prior iterations and remains an active audit concern | Degradation risk | Open | `docs/releases/v1.0.6-release-notes.md`, `docs/architecture-gap-report.md` |
| SYS-09-003 | P2 | Dashboard/admin response-shape stability is stronger than earlier versions, but not yet classified system-wide | Admin surfaces should have stable contracts with tests where possible | Go already has several strong shape tests and a richer in-memory log surface, which is likely a real improvement over weaker file-backed observability | Improvement candidate | Open | `internal/api/response_shape_test.go`, `internal/api/routes/system_warnings_test.go`, `internal/api/logbuffer.go` |
| SYS-09-004 | P2 | Observability route duplication and overlap still needs a map | Operators should know which route is canonical for a given signal | Go has both `/api/traces` and `/api/observability/*` families plus WebSocket snapshots; canonical ownership is not yet documented in this audit program | Open | Open | `internal/api/routes/traces.go`, `internal/api/routes/observability.go`, `internal/api/ws_topics.go` |
| SYS-09-005 | P2 | Trace search had been implemented via ad hoc `LIKE` over serialized JSON blobs | Observability search should stay truthful and evolvable as trace structure changes | `SearchTraces` now filters `tool_name` through exact `tool_calls` matches and evaluates `guard_name` against parsed JSON in process instead of SQL `LIKE` over `stages_json` / `react_trace_json`. It also now applies the `guard_name` filter before the final result limit, so matching guarded traces are not silently dropped because earlier non-matching rows consumed the SQL `LIMIT`. Remaining work is broader route-family contract mapping, not blob search in SQL | Degradation remediated | Accepted | `internal/api/routes/traces.go:37-118`, `internal/api/routes/routes_test.go` |
| SYS-09-006 | P2 | Release notes now include explicit confidence caveats, which is healthier than overclaiming, but this discipline must remain part of the observability contract | Operator-facing docs should distinguish validated truth from provisional confidence | v1.0.6 release notes explicitly caveat fixture-level SSE MCP confidence and open soak follow-up items instead of claiming full closure | Improvement with governance requirement | Open | `docs/releases/v1.0.6-release-notes.md` |
| SYS-09-007 | P2 | WebSocket topic snapshots are truthier than a bespoke second data path because they invoke the HTTP handlers directly | Realtime snapshots should not fork semantics from the canonical HTTP routes | Go builds topic snapshots via `httptest` against the actual handlers, which is a real observability improvement and reduces drift between transport surfaces | Improvement candidate | Open | `internal/api/ws_topics.go:12-68` |
| SYS-09-008 | P2 | Trace route families overlap but serve different artifact shapes, and that boundary is still under-documented | Operators should know when to use `/api/traces` versus `/api/observability/traces` and what fidelity each one preserves | Go exposes both a lighter search/list family and a heavier observability/page family; this is not automatically wrong, but canonical ownership is still implicit rather than documented | Idiomatic shift needing explicit contract | Open | `internal/api/routes/traces.go`, `internal/api/routes/observability.go` |

## Intentional Deviations

Possible likely improvements that still need explicit classification:

- richer trace namespaces and annotations
- dashboard/system warning contract tests
- retrieval-path telemetry and dormancy aggregation
- structured log ring buffer instead of file-tail-based operator surfaces
- explicit confidence caveats in release-facing truth surfaces
- WebSocket snapshot reuse of canonical HTTP handlers

## Remediation Notes

This system should be finalized after Systems 01-08 are more stable, because
its job is to reflect their truth accurately.

Still, two rules are already clear:

- stronger observability contracts are real assets and should be preserved
- operator-facing honesty (including explicit caveats) is part of the product,
  not optional release prose
- when multiple transport surfaces exist, prefer shared artifact generation over
  parallel summary logic

## Downstream Systems Affected

- All other systems, because this family is the primary operator-facing truth
  surface for them

## Open Questions

- Which observability routes are canonical versus legacy/overlapping?
- Which route family is canonical for trace list/search versus trace detail /
  waterfall views?
- Which release-facing docs should be treated as part of the audited truth
  surface versus historical record?
- Can some trace annotations move closer to the final runtime artifact boundary?
- Should trace search stay JSON-`LIKE` based, or is that now technical debt in
  the observability layer itself?

## Progress Log

- 2026-04-16: Initialized System 09 document.
- 2026-04-16: Recorded that this system should lag backend stabilization, but
  still needs to exist now because drift here has already affected trust.
- 2026-04-16: Recorded that some observability surfaces are already stronger
  than before (shape tests, log ring buffer, explicit release caveats), while
  trace-search/query fidelity still needs a more disciplined pass.
- 2026-04-16: Added a concrete positive seam to preserve: WebSocket topic
  snapshots invoke the HTTP handlers directly, reducing transport-level drift.
- 2026-04-16: Split route-overlap concerns into two parts: the brittle JSON
  `LIKE` search seam, and the still-underdocumented boundary between
  `/api/traces` and `/api/observability/traces`.
- 2026-04-18: Closed a second trace-search truth seam. `SearchTraces` no
  longer applies SQL `LIMIT` before the in-process `guard_name` filter, which
  previously let newer non-matching rows hide older valid matches from the
  operator.
