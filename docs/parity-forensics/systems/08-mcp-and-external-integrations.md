# System 08: MCP and External Integrations

## Status

- Owner: parity-forensics program
- Audit status: `reopened`
- Last updated: 2026-04-20
- Related release: v1.0.7

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
| SYS-08-001 | P1 | Practical MCP confidence must remain separate from generic plumbing confidence | Real-server readiness must not be inferred from unit coverage alone | v1.0.6 accepts this as an explicit governance invariant: real blessed-target validation and generic plumbing confidence are tracked separately in the checklist and release notes | Accepted governance invariant | Closed | `docs/testing/mcp-release-blocker-checklist.md`, `docs/releases/v1.0.6-release-notes.md` |
| SYS-08-002 | P1 | MCP tool bridging affects live tool surface and must stay aligned with pruning/provenance | External tools should enter the runtime through a single truthful bridge | Go now hot-syncs MCP tools into the same live registry/embedding surface on connect, discover, and disconnect instead of leaving runtime-added MCP servers restart-only. Discover-time tool refresh is now manager-owned, so route-side discovery mutates the live connection instead of a copied snapshot. Bridged MCP tools now stay aligned with the live selected tool surface instead of stopping at the connection manager. | Improvement / remediation | Remediated / cross-check with System 02 | `internal/mcp/manager.go`, `internal/agent/tools/mcp_bridge.go`, `internal/api/mcp_tool_surface.go`, `internal/api/routes/mcp.go`, `internal/agent/tools/tool_search.go` |
| SYS-08-003 | P1 | Release-truth drift around MCP readiness can reappear even when code improves | Docs must state exactly what was validated | v1.0.6 closes this by making the checklist and release notes the canonical readiness surfaces. Fixture-only SSE confidence is called out explicitly rather than implied as cross-vendor proof | Degradation risk controlled | Closed | MCP checklist + release notes + runtime validation evidence |
| SYS-08-004 | P2 | Stdio/SSE transport behavior still needs line-by-line parity classification | Transport timeouts, env propagation, diagnostics, and reconnect semantics should be explicit and tested | v1.0.6 accepts the current transport contract: stdio and SSE transports are generic, per-call timeout is call-local, env propagation/diagnostics are explicit, and cross-vendor SSE proof remains a deferred validation item rather than an ambiguous gap | Accepted with explicit deferred validation scope | Closed for v1.0.6 | `internal/mcp/client.go`, manager/tests, checklist artifacts |
| SYS-08-005 | P2 | Integration-test guidance and checklist evidence are now stronger, but must remain synchronized with the real blessed targets | Validation guidance should point at the same targets the release checklist certifies | This is accepted as an ongoing governance rule, not unresolved parity debt | Accepted governance invariant | Closed | `internal/mcp/integration_test.go`, `docs/testing/mcp-release-blocker-checklist.md` |
| SYS-08-006 | P1 | Per-call timeout/cancellation must not tear down the whole MCP connection | A timed-out `tools/call` should fail that call without silently degrading the server's future availability unless the transport itself is irrecoverable | Go now uses a long-lived receive loop plus per-request pending-call channels. Timed-out calls are removed from the pending map; late responses are dropped; only real transport failure poisons the connection. This keeps stdio/SSE transport availability tied to transport health instead of a single caller timeout. | Improvement / remediation | Remediated | `internal/mcp/client.go`, `internal/mcp/client_test.go` |
| SYS-08-007 | P2 | WebSocket/HTTP operational evidence for MCP status is stronger when it reuses canonical handlers instead of a second summary path | Operator-facing status surfaces should share one data source where possible | Go's topic snapshots invoke the same HTTP handlers through `httptest`, and v1.0.6 accepts that as a genuine observability improvement tied to the canonical MCP route contract | Accepted improvement | Closed / cross-check with System 09 | `internal/api/ws_topics.go:12-68`, `internal/api/routes/mcp.go` |
| SYS-08-008 | P2 | MCP status/tool management surfaces should not drift on Go map iteration order | Operator-facing MCP lists should be stable across runs so UI/admin surfaces and downstream syncs are reproducible | `ConnectionManager.Statuses()` and `AllTools()` now emit connections in deterministic server-name order instead of inheriting Go map iteration order. This keeps route responses and live tool-surface sync stable across runs. | Improvement / remediation | Remediated | `internal/mcp/manager.go`, `internal/mcp/manager_test.go` |
| SYS-08-009 | P1 | Dead MCP transports were still reported as connected and kept advertising stale tools | Operator/runtime surfaces should distinguish a configured connection object from a live healthy transport | Go now keeps dead connections visible in `Statuses()` but marks them `connected=false`, zeros their live `tool_count`, and surfaces the transport error. Aggregated `AllTools()` excludes those dead connections, and runtime summaries count only healthy connections. | Improvement / remediation | Remediated | `internal/mcp/manager.go`, `internal/mcp/manager_test.go`, `internal/api/routes/mcp.go` |

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
- preserve the new request/response dispatcher model: per-call timeout is now a
  call-local failure, while transport failure remains connection-fatal
- preserve runtime MCP tool-surface truth: connect/discover/disconnect must
  sync the same registry/embedding surface that startup wiring uses, so runtime
  MCP changes do not require daemon restart to reach tool pruning or execution

## Downstream Systems Affected

- System 02: Tool exposure, pruning, and execution loop
- System 07: Install, update, service lifecycle, and config loading
- System 09: Admin, dashboard, and observability surfaces

## Final Disposition

System 08 is closed for v1.0.6.

- Runtime MCP connection semantics, status truth, tool-surface sync, and
  timeout behavior are now explicit and tested.
- The checklist and release notes are the canonical readiness surfaces.
- Cross-vendor SSE validation remains deliberately deferred and explicitly
  disclosed rather than buried under vague “improved but open” language.

## v1.0.7 Reopening

System 08 is reopened for one remaining validation seam:

- `PAR-008` — cross-vendor SSE MCP proof

v1.0.7 execution stance:

- fixture-only SSE evidence is no longer sufficient as the only positive proof
- one authoritative named-target SSE validation harness and evidence artifact
  must own this claim instead of ad hoc checklist prose
- the evidence artifact must record target, auth mode, discovery behavior,
  `initialize`, `tools/list`, `tools/call`, returned server metadata, and the
  closure verdict for each target
- the active real-target prospect list is currently:
  - FeedOracle MCP
  - Channel3 MCP
  - Atlassian Rovo MCP
  - OnceHub MCP
- Zapier is explicitly removed from the SSE prospect list because their current
  MCP docs have moved away from SSE support; using Zapier as an SSE proof
  target would create a false closure
- closure requires more than one real third-party target to validate through
  the same harness; one success plus one vendor-side transport deprecation does
  not count as cross-vendor proof
- MCP config-to-runtime conversion must be centralized so daemon startup, route
  tests, and validation harnesses cannot drift on auth/header/allowlist
  semantics
- the transport contract should be explicit about what is actually supported:
  endpoint-discovery events, auth-bearing GET/POST, standard SSE framing, and
  tool-call proof on the live path
- v1.0.7 explicitly narrows the supported contract: the harness and transport
  are release-grade, but cross-vendor third-party SSE proof is deferred until
  more than one real vendor target validates through the same named-target
  evidence path

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
- 2026-04-17: Remediated the per-call timeout seam by moving `Connection` onto
  a long-lived receive loop with per-request pending-call channels. Timed-out
  calls now fail locally without closing the transport, and late responses are
  dropped instead of poisoning the next call.
- 2026-04-18: Remediated hot MCP tool-surface drift. Runtime connect, discover,
  and disconnect paths now sync the live agent registry/descriptor embeddings
  through an API-owned MCP tool-surface adapter instead of leaving runtime MCP
  tool changes restart-only.
- 2026-04-18: Remediated manager-vs-route discovery ownership drift.
  `DiscoverMCPTools` now refreshes tools through `ConnectionManager` so
  runtime discovery mutates the live connection state instead of a copied
  snapshot that never reached the manager's authoritative tool surface.
- 2026-04-18: Remediated MCP operator-surface nondeterminism. Connection
  statuses and aggregated tools now emit in stable server-name order instead
  of drifting on Go map iteration, which keeps route/UI surfaces and tool-sync
  behavior reproducible across runs.
- 2026-04-18: Remediated dead-connection truth drift. Manager status output now
  keeps failed connections visible for diagnostics but marks them
  `connected=false` with an error string and zero live `tool_count`, while
  aggregated tool surfaces exclude those dead transports entirely.
- 2026-04-20: Reopened the remaining SSE validation seam as `PAR-008` for
  active v1.0.7 work. The next closure step is a central named-target SSE
  validation harness plus evidence artifact, not more fixture-only prose.
- 2026-04-20: Tightened the active remediation contract for `PAR-008`:
  central runtime config conversion, endpoint-discovery/auth-capable SSE
  transport, and a named-target `validate-sse` harness are now part of the
  required closure proof.
- 2026-04-20: Landed the central runtime pieces for `PAR-008`: daemon startup
  and route tests now share one MCP config-conversion seam, the SSE transport
  now supports endpoint-discovery events plus auth-bearing GET/POST requests,
  and the runtime exposes a named-target `validate-sse` harness that returns a
  structured evidence artifact. The remaining blocker is real multi-target
  third-party validation, not additional internal transport work.
