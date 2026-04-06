# Goboticus Feature-Complete Checklist

This document defines the exact bar for declaring Goboticus feature-complete as
the successor to Roboticus.

## Baseline

- **Reference implementation:** `/Users/jmachen/code/roboticus`
- **Reference product line:** final Roboticus `v0.11.x` shipped behavior,
  especially `v0.11.0` through `v0.11.4`
- **Primary sources:**
  - `roboticus/docs/ROADMAP.md`
  - `roboticus/docs/releases/v0.11.0.md`
  - `roboticus/docs/releases/v0.11.4-spec.md`
  - `roboticus/docs/INTEGRATION_TEST_MATRIX.md`
  - `roboticus/docs/testing/regression-matrix.md`

## What "Feature Complete" Means

Goboticus is feature-complete only when all of the following are true:

1. Every required operator-visible and user-visible capability in this
   checklist exists in Goboticus.
2. No advertised feature is backed by placeholder behavior, fake success, or
   silent degraded fallback.
3. Every required capability has regression coverage at the right layer.
4. Release smoke tests exercise the full advertised runtime, not just isolated
   packages.
5. Any intentionally deferred capability is:
   - removed from product claims, docs, and UI affordances, or
   - explicitly labeled as unavailable with honest behavior (`501`,
     disabled control, or equivalent).

## Non-Goals For This Gate

These are not blockers for Goboticus feature-complete status unless Goboticus
chooses to advertise them:

- future Roboticus roadmap items beyond the final `v0.11.x` baseline
- speculative discovery/device/network flows that were still partial in
  Roboticus
- future voice-channel roadmap work
- generic "infinite parity" infrastructure after Goboticus is accepted as the
  primary codebase

## Required Capability Checklist

Status key for this checklist:

- `[ ]` required and not yet accepted as complete
- `[x]` required and accepted as complete
- `[D]` explicitly deferred and de-advertised

### A. Core Runtime Entry Paths

- [ ] `POST /api/agent/message` performs full validate -> session -> retrieval
  -> routing -> inference -> persistence flow.
- [ ] `POST /api/agent/message/stream` uses the same business pipeline as the
  non-stream path and preserves persistence/metrics parity.
- [ ] WebSocket or equivalent operator event stream is functional for live UI
  updates.
- [ ] `GET /api/health`, agent metadata, and logs are live and trustworthy.

### B. Channel Ingress And Delivery

- [ ] Telegram ingress is fully wired for the chosen runtime mode
  (webhook and/or poll) with the same policy and formatting behavior as web/API.
- [ ] WhatsApp ingress/verification is fully wired if Goboticus advertises it.
- [ ] Discord outbound messaging path is functional if advertised.
- [ ] Signal outbound messaging path is functional if advertised.
- [ ] Delivery retries persist across restart via `delivery_queue`.
- [ ] Dead-letter inspection and replay are functional through API/operator
  surfaces.
- [ ] Cross-channel behavior follows the shared pipeline rather than connector-
  specific business logic.

### C. Sessions, Scope, And Lifecycle

- [ ] Session auto-create and scope isolation are enforced.
- [ ] Session uniqueness and backfill/migration invariants are enforced.
- [ ] TTL/session expiration works through the background loop.
- [ ] Session rotation follows scheduler-driven semantics, not ad hoc timing.
- [ ] Archive/delete behavior preserves the promised history semantics.
- [ ] Session insights, turns, feedback, and messages surfaces are honest under
  failure and complete under success.

### D. Routing, Capacity, And Breakers

- [ ] Complexity-aware routing is active.
- [ ] Capacity-aware routing is active.
- [ ] Breaker pressure influences selection correctly.
- [ ] Breaker status/reset surfaces are functional.
- [ ] Capacity telemetry is exposed to operator surfaces.
- [ ] Metascore routing is the real execution path when enabled.
- [ ] Session-aware metascore penalties are honored.
- [ ] Per-context / per-intent metascore behavior is honored.
- [ ] Per-model timeout overrides are honored.
- [ ] Model exercise / bootstrap flow exists if Goboticus advertises model
  self-baselining.
- [ ] Model suggestion flow exists if Goboticus advertises suggestion/apply UX.
- [ ] Routing profile controls and spider-graph weighting are functional if
  Goboticus advertises user-tunable routing.

### E. Memory, Retrieval, And Context

- [ ] Retrieval contributes to prompt assembly.
- [ ] Post-turn ingestion persists working/episodic/semantic memory.
- [ ] Context explorer APIs are functional: turns, context, tools, model
  selection, analysis/tips.
- [ ] Memory search is functional across the advertised tiers.
- [ ] Memory analytics reflect live data, not placeholders or null defaults.
- [ ] Memory introspection/health surfaces report real counts and lifecycle
  state.
- [ ] Retrieval avoids self-echo and restart inconsistency regressions.

### F. Tools, Guardrails, Browser, Plugins, MCP

- [ ] Tool policy and approval gates function end-to-end.
- [ ] Browser admin/runtime APIs are functional for the advertised action set.
- [ ] Plugin discovery, enable/disable, and execution are functional.
- [ ] MCP management surfaces are functional and aligned across API/UI/CLI for
  the surfaces Goboticus claims.
- [ ] Config-protection guardrails block unsafe config mutation through tools.
- [ ] Action verification guardrails prevent fabricated financial/action claims.

### G. Analysis, Recommendations, And Operator Intelligence

- [ ] Turn analysis is real and returns complete behavior, not placeholders.
- [ ] Session analysis is real and returns complete behavior, not placeholders.
- [ ] Recommendation generation is real and returns concrete output.
- [ ] Workspace, roster, skills, subagent, and memory health views reflect real
  state.
- [ ] All admin/operator read paths fail honestly on DB/query errors.

### H. Scheduler, Cron, And Background Operations

- [ ] Cron CRUD is fully functional.
- [ ] Cron execution loop runs due jobs, leases safely, and records runs.
- [ ] UI-created schedule kinds are executable by the worker.
- [ ] Heartbeat tasks do real work or are explicitly de-scoped.
- [ ] Cache persistence/flush lifecycle is functional if advertised.

### I. Wallet, Treasury, Yield, And Payments

- [ ] Wallet read endpoints are functional and honest.
- [ ] Treasury state is served from cache/persisted state where promised.
- [ ] Treasury/runtime policy checks are enforced where advertised.
- [ ] EIP-3009 and x402 flows are complete for the surfaces Goboticus claims.
- [ ] Yield lifecycle behavior is either implemented or explicitly not claimed.

### J. Discovery, A2A, Runtime, And Device Surfaces

- [ ] A2A hello/handshake is functional if advertised.
- [ ] Discovery runtime features are either complete or explicitly de-advertised.
- [ ] Device pairing/runtime features are either complete or explicitly
  de-advertised.
- [ ] Runtime discovery/admin surfaces do not hide query failures behind empty
  success responses.

### K. Dashboard, TUI, And Operator UX

- [ ] Dashboard sessions/chat/context/metrics/operator controls work end-to-end.
- [ ] Markdown and rendering safety are enforced.
- [ ] Configuration editing and status surfaces are backed by real state.
- [ ] Theme/runtime/operator controls validate inputs and persist honest state.
- [ ] If Goboticus advertises TUI parity, every operator-critical dashboard
  surface has a TUI equivalent.
- [ ] If Goboticus does not yet provide TUI parity, that claim must be absent
  from docs and product messaging.

### L. CLI Product Surfaces

- [ ] `status`, `sessions`, `config`, `subagents`, and other operator-critical
  CLI flows work against a live runtime.
- [ ] `update` performs a real update check and does not print a placeholder.
- [ ] No CLI command presents fake-success output for unimplemented work.

## Hard Acceptance Rules

Goboticus may be declared feature-complete only if:

1. Every required checklist item above is either `[x]` or explicitly `[D]`.
2. No `[D]` item appears in README, dashboard UI, CLI help, or API docs as a
   completed feature.
3. The regression matrix in `docs/regression-test-matrix.md` has coverage for
   every required capability class.
4. `go test ./...` passes.
5. The live smoke suite passes.

## Required Documentation Hygiene

Before declaring feature-complete:

- README must match the actual shipped runtime.
- CLI help text must match actual behavior.
- Dashboard controls must not expose dead buttons for deferred features.
- Any remaining unavailable surface must be explicitly unavailable, not merely
  silently inert.
