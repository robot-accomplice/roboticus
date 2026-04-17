# System 08: MCP and External Integrations

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-16
- Related release: v1.0.6

## Why This System Matters

MCP and other external integrations sit at the boundary between a healthy core
runtime and real-world tool reach. This family has already taught us that
"generic plumbing exists" is not the same thing as "operationally trustworthy."

This system must track both:

- implementation correctness
- validation truthfulness

because drift has shown up in both code and the claims made around it.

## Scope

In scope:

- MCP stdio/SSE client and connection manager
- MCP runtime routes and operational test surfaces
- MCP tool bridging into the agent tool registry
- release-readiness validation/checklist evidence
- practical confidence level of external integrations

Out of scope:

- tool-pruning logic except where MCP provenance affects ranking
- install/update/service lifecycle
- dashboard visual details beyond MCP-specific observability claims

## Rust / Baseline Anchors

The key baseline here is practical operator trust, not just code presence:

- runtime should connect to configured servers generically
- failures should be diagnosable
- release docs must distinguish unit coverage from real-server validation

Additional Rust/source-of-truth anchors should be added as the cutover audit
continues.

## Go Source Anchors

| Concern | Go / doc file(s) |
|---------|-------------------|
| Connection manager | `internal/mcp/manager.go` |
| Stdio/SSE client | `internal/mcp/client.go` |
| MCP runtime routes | `internal/api/routes/mcp.go` |
| MCP bridge into tools | `internal/agent/tools/mcp_bridge.go` |
| MCP release checklist | `docs/testing/mcp-release-blocker-checklist.md` |
| Release notes MCP truth claims | `docs/releases/v1.0.6-release-notes.md` |

## Live Go Path

Current observed state on 2026-04-16:

1. Runtime MCP behavior is config-driven, not hardcoded to a specific server.
2. There is a real connection manager and generic stdio/SSE transport support.
3. MCP tools can be bridged into the agent tool registry and now participate in
   tool-pruning provenance and latency penalty logic.
4. The repo now carries an explicit MCP release-blocker checklist and better
   diagnostic surfacing for stdio startup failures.

This family is in better shape than when the Playwright `initialize failed: EOF`
incident surfaced, but it still needs careful parity classification around the
gap between:

- implemented
- unit-tested
- mock-integrated
- real-server validated
- operationally trustworthy

## Artifact Boundary

The artifact boundaries for this system are:

- connection status/runtime metadata for configured MCP servers
- real `initialize` / `tools/list` / `tools/call` evidence
- child-process diagnostics for stdio failures
- bridged MCP tool descriptors in the live agent registry

Parity/readiness is not satisfied unless those artifacts are consistent and the
docs describe them honestly.

## Success Criteria

- Closure artifact(s):
  - connection/runtime metadata for configured MCP servers
  - real `initialize`, `tools/list`, and `tools/call` evidence
  - actionable stdio failure diagnostics
  - bridged MCP tool descriptors as they appear on the live tool surface
- Live-path proof:
  - at least one blessed stdio and one blessed SSE target are exercised through
    the actual runtime path
  - timeout/failure behavior is classified on the live connection path, not
    only in helper tests
  - MCP tool bridging is proven on the same request/tool surface audited in
    System 02
- Blocking conditions:
  - docs still overstate practical readiness beyond the available evidence
  - connection teardown / timeout semantics remain ambiguous or operator-hostile
  - blessed validation targets and integration guidance can drift apart
- Accepted deviations:
  - governance improvements, better diagnostics, and richer provenance may
    remain only if transport semantics are still described honestly

## Divergence Register

| ID | Priority | Concern | Baseline / desired behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|-----------------------------|-------------|----------------|--------|----------|
| SYS-08-001 | P1 | Practical MCP confidence must remain separate from generic plumbing confidence | Real-server readiness must not be inferred from unit coverage alone | Go now has a release-blocker checklist and improved diagnostics, but this distinction must remain enforced | Improvement with ongoing governance requirement | Open | `docs/testing/mcp-release-blocker-checklist.md`, `docs/releases/v1.0.6-release-notes.md` |
| SYS-08-002 | P1 | MCP tool bridging affects live tool surface and must stay aligned with pruning/provenance | External tools should enter the runtime through a single truthful bridge | Go bridges MCP tools into the registry and classifies them for tool-search penalties/provenance | Improvement | Open / cross-check with System 02 | `internal/agent/tools/mcp_bridge.go`, `internal/agent/tools/registry.go`, `internal/agent/tools/tool_search.go` |
| SYS-08-003 | P1 | Release-truth drift around MCP readiness can reappear even when code improves | Docs must state exactly what was validated | Go has already had overstated MCP confidence in prior iterations; this remains an audit target, not a one-time fix | Degradation risk | Open | MCP checklist + release notes + runtime validation evidence |
| SYS-08-004 | P2 | Stdio/SSE transport behavior still needs line-by-line parity classification | Transport timeouts, env propagation, diagnostics, and reconnect semantics should be explicit and tested | Several hardening fixes landed (stderr capture, env inheritance, release-blocker evidence), but the full cross-transport classification is not yet complete in this program | Open | Open | `internal/mcp/client.go`, manager/tests, checklist artifacts |
| SYS-08-005 | P2 | Integration-test guidance and checklist evidence are now stronger, but must remain synchronized with the real blessed targets | Validation guidance should point at the same targets the release checklist certifies | Go now documents current Playwright guidance and stores a checklist artifact with explicit targets/evidence; this is a genuine improvement but requires ongoing synchronization discipline | Improvement with governance requirement | Open | `internal/mcp/integration_test.go`, `docs/testing/mcp-release-blocker-checklist.md` |
| SYS-08-006 | P1 | Per-call timeout/cancellation currently tears down the whole MCP connection | A timed-out `tools/call` should fail that call without silently degrading the server's future availability unless the transport itself is irrecoverable | Go enforces timeout by selecting on `ctx.Done()` in `Connection.call(...)` and then closing the entire connection, because `Send`/`Receive` are blocking and not individually cancellable | Degradation seam | Open | `internal/mcp/client.go:287-322`, `internal/mcp/client.go:246-257` |
| SYS-08-007 | P2 | WebSocket/HTTP operational evidence for MCP status is stronger when it reuses canonical handlers instead of a second summary path | Operator-facing status surfaces should share one data source where possible | Go's topic snapshots invoke the same HTTP handlers through `httptest`, which is a real improvement in observability truthfulness even though transport semantics still need classification | Improvement candidate | Open / cross-check with System 09 | `internal/api/ws_topics.go:12-68`, `internal/api/routes/mcp.go` |

## Intentional Deviations

Possible likely improvements that still need explicit classification:

- child-stderr diagnostic capture for stdio startup failure
- MCP-specific release-blocker checklist
- MCP latency penalty in tool pruning
- corrected integration guidance for the blessed Playwright target
- parent-environment inheritance plus explicit env overrides
- WebSocket topic snapshots reusing the HTTP handlers instead of maintaining a
  second MCP-summary path

None are accepted yet until the full transport and operational audit is done.

## Remediation Notes

The key discipline for this system is governance as much as code:

- keep confidence labels explicit
- keep real-server validation evidence tied to concrete blessed targets
- do not let stale examples become de facto runtime truth again
- treat transport hardening and validation-governance as separate concerns so
  improved docs do not accidentally mask unfinished transport semantics work
- classify whether destructive timeout handling is an accepted transport tradeoff
  or still a release-grade operator-contract gap

## Downstream Systems Affected

- System 02: Tool exposure, pruning, and execution loop
- System 07: Install, update, service lifecycle, and config loading
- System 09: Admin, dashboard, and observability surfaces

## Open Questions

- Which remaining MCP transport behaviors still differ materially from the
  desired operator contract?
- Is closing the whole connection on per-call timeout an accepted simplification
  or a real reliability regression versus the desired contract?
- Are the blessed validation targets stable enough to keep using as release
  truth?
- Which MCP docs are canonical versus just evidence bundles?
- Which transport guarantees are now protected invariants versus still
  provisional hardening work?

## Progress Log

- 2026-04-16: Initialized System 08 document.
- 2026-04-16: Captured the main audit distinction for this family as
  "implementation confidence vs operational confidence."
- 2026-04-16: Recorded that validation governance is materially stronger now
  (checklist + corrected integration guidance), but transport semantics still
  need a separate line-by-line classification pass.
- 2026-04-16: Added a concrete transport seam: per-call timeout/cancellation
  currently works by closing the entire MCP connection, not just aborting the
  in-flight call.
- 2026-04-16: Recorded that WebSocket topic snapshots reuse HTTP handlers,
  which is a meaningful observability-truth improvement that should be
  preserved when classifying MCP operator surfaces.
