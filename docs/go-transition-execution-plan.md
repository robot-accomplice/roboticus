# Roboticus Go Transition Execution Plan

This document turns the migration policy into an actionable execution program.

It answers:

- what happens first
- what workstreams run in parallel
- what must be proven before release
- what is allowed and not allowed during the transition

## Governing Documents

This plan is subordinate to:

- `docs/migration-release-policy.md`
- `docs/feature-complete-checklist.md`
- `docs/regression-test-matrix.md`
- `docs/roadmap-architecture-intake.md`

## Goal

Deliver Roboticus as the provable `v0.11.4` successor and then ship the next
feature wave only from Go.

## Success Conditions

This plan is complete only when:

1. Roboticus `v0.11.4` is released and frozen as the final Rust baseline.
2. Roboticus satisfies the feature-complete checklist.
3. Roboticus satisfies the regression test matrix.
4. Roboticus passes the blocking release gates from
   `docs/migration-release-policy.md`.
5. Roboticus can begin the next planned feature wave without reopening parity
   uncertainty.

## Program Structure

The transition is divided into six phases:

1. Baseline Freeze
2. Gap Enumeration
3. Parity Closure
4. Regression Hardening
5. Release Readiness
6. Go-Only Launch Preparation

## Phase 1 — Baseline Freeze

### Objective

Freeze the Rust implementation so parity has a stable target.

### Tasks

1. Release Roboticus `v0.11.4`.
2. Record the exact `v0.11.4` tag/commit in Roboticus docs.
3. Freeze the parity target to that release and stop treating Roboticus as a
   co-evolving implementation.
4. Snapshot the final reference docs used for parity interpretation:
   - `docs/ROADMAP.md`
   - `docs/releases/v0.11.0.md`
   - `docs/releases/v0.11.4-spec.md`
   - `docs/INTEGRATION_TEST_MATRIX.md`
   - `docs/testing/regression-matrix.md`

### Deliverables

- frozen baseline reference recorded in docs
- explicit parity target commit/tag
- migration policy accepted as the release rule

### Exit Gate

- There is one and only one baseline: Roboticus `v0.11.4`

## Phase 2 — Gap Enumeration

### Objective

Turn “parity” into a concrete defect and coverage backlog.

### Tasks

1. Walk every item in `docs/feature-complete-checklist.md`.
2. For each item, classify it:
   - complete
   - incomplete
   - deferred and must be de-advertised
   - unknown and needs verification
3. Build the parity backlog by workstream, not by random file list.
4. Map each incomplete item to at least one regression row in
   `docs/regression-test-matrix.md`.
5. Identify any checklist item that lacks a test strategy and add the missing
   regression entry before implementation starts.

### Workstream Buckets

- Core entry paths
- Channels and delivery
- Sessions and lifecycle
- Routing and metascore behavior
- Memory and context
- Tools/browser/plugins/MCP
- Analysis/operator intelligence
- Scheduler/background operations
- Wallet/treasury/payments
- Discovery/runtime surfaces
- Dashboard/CLI/TUI/doc surfaces

### Deliverables

- parity backlog with status per checklist item
- regression ownership per backlog item
- explicit deferred items list

### Exit Gate

- Every checklist item is classified and assigned to a workstream

## Phase 3 — Parity Closure

### Objective

Close all required functional gaps against the frozen baseline.

### Execution Rule

No new Go-only product features during this phase unless they are required for:

- parity completion
- regression protection
- architecture needed to avoid blocking roadmap-critical future work

### Workstream Plan

#### 3A. Core Runtime Paths

Tasks:

- enforce non-stream and stream behavioral parity
- verify persistence, metrics, and session semantics match the baseline
- close any remaining connector-specific divergence

Done when:

- core entry path checklist items are complete
- stream/non-stream parity is tested and stable

#### 3B. Channels And Delivery

Tasks:

- finish channel surfaces that are still claimed but incomplete
- verify retry queue, dead-letter, and replay behavior
- ensure channel paths use the shared pipeline

Done when:

- all claimed channel surfaces are complete or de-advertised
- durable delivery behavior is release-safe

#### 3C. Sessions, Scope, And Lifecycle

Tasks:

- verify scope uniqueness and isolation invariants
- close TTL/rotation/archive/delete behavior gaps
- remove any remaining fake-success or silent fallback behavior

Done when:

- session lifecycle behavior matches the documented contract

#### 3D. Routing, Metascores, And Breakers

Tasks:

- verify execution actually honors router output
- close any remaining session-aware/context-aware routing gaps
- implement user-weighting/spider-graph behavior if Roboticus intends to claim
  it at parity time
- close any route/UI discrepancy around routing diagnostics

Done when:

- routing behavior is both functionally complete and test-proven

#### 3E. Memory And Context

Tasks:

- close any remaining retrieval, ingestion, explorer, or analytics gaps
- verify memory introspection and context explorer data are live and accurate
- remove placeholder or null-default analytics behavior

Done when:

- memory surfaces are complete and regression-protected

#### 3F. Tools, Browser, Plugins, MCP

Tasks:

- complete any remaining browser/admin/runtime action parity
- verify policy + approval loops
- verify plugin and MCP operator surfaces
- keep browser support behind a backend abstraction suitable for future
  `agent-browser` work

Done when:

- these surfaces are complete or explicitly de-advertised

#### 3G. Analysis, Recommendations, And Operator Surfaces

Tasks:

- remove any remaining analysis/recommendation placeholders
- ensure workspace/roster/skills/health views reflect real state
- eliminate misleading success behavior on operator endpoints

Done when:

- all operator-visible intelligence surfaces are real and honest

#### 3H. Scheduler, Wallet, Runtime, And Miscellaneous Domains

Tasks:

- verify cron worker and schedule compatibility
- verify wallet/treasury/x402/EIP-3009 surfaces
- verify discovery/runtime/A2A surfaces that Roboticus still advertises

Done when:

- no required system domain remains incomplete

### Exit Gate

- Every required checklist item is either complete or intentionally deferred and
  de-advertised

## Phase 4 — Regression Hardening

### Objective

Make the parity claim durable.

### Principle

No feature is considered complete until the test matrix proves it.

### Tasks

1. Expand `L1` unit coverage for isolated logic:
   - routing
   - breakers
   - wallet artifacts
   - policy and guards
   - config merging/validation
2. Expand `L2` subsystem and route coverage:
   - honest error semantics
   - CRUD contract edges
   - persistence/state transitions
   - stream/non-stream parity
3. Expand `L3` smoke coverage:
   - every advertised operator-critical surface
   - every claimed subsystem
4. Expand `L4` behavioral/efficacy coverage where needed:
   - metascore correctness and efficacy
   - channel metadata leakage prevention
   - guard effectiveness
5. Add soak batteries for:
   - long-running session stability
   - retry queue/delivery stability
   - streaming stability
   - scheduler reliability
6. Add documentation/claim checks:
   - if UI/README/CLI claims a feature, a corresponding test must exist

### Exit Gate

- Every required regression class in `docs/regression-test-matrix.md` has
  ownership and real coverage

## Phase 5 — Release Readiness

### Objective

Decide whether Roboticus is eligible to replace Rust in practice, not just in
theory.

### Release Readiness Checklist

1. Run full test battery:
   - `go test ./...`
   - architecture fitness tests
   - route/integration suites
   - live smoke
   - soak/regression batteries
2. Run parity audit against frozen Rust baseline.
3. Review all remaining checklist items for accidental incompleteness.
4. Review docs, README, CLI help, and dashboard controls for claim drift.
5. Review deferred items to ensure they are truly de-advertised.
6. Review roadmap-architecture intake to confirm no parity fixes created future
   architectural traps.
7. Dry-run the `roboticus.ai` release sync against the Go release source.
8. Validate public install and upgrade compatibility:
   - `roboticus.ai/install.sh`
   - `roboticus.ai/install.ps1`
   - `roboticus update all`
   - `roboticus upgrade all`
9. Validate release artifact completeness and checksum publication.

### Release Decision Rule

Roboticus is ready to replace Rust only if all blocking gates in
`docs/migration-release-policy.md` are satisfied.

### Exit Gate

- Roboticus is accepted as the `v0.11.4` replica

## Phase 6 — Go-Only Launch Preparation

### Objective

Prepare the first Go-native post-parity release.

### Tasks

1. Close the parity freeze.
2. Re-open roadmap-driven feature work only in Roboticus.
3. Select the first Go-native feature slice from
   `docs/roadmap-architecture-intake.md`.
4. Convert that slice into:
   - implementation plan
   - architecture constraints
   - regression additions
5. Preserve all parity-era regression protection as the new product baseline.

### Exit Gate

- Roboticus is no longer in parity catch-up mode and can ship the next feature
  wave

## Immediate Backlog To Make This Operational

These are the first concrete actions to take now.

### Week 1 — Program Setup

1. Freeze the exact Roboticus `v0.11.4` commit/tag in docs.
2. Create a parity board from `docs/feature-complete-checklist.md`.
3. Mark each item:
   - complete
   - in progress
   - blocked
   - deferred
4. Attach regression IDs from `docs/regression-test-matrix.md`.
5. Identify missing regression rows and add them.

### Week 2 — High-Risk Gap Closure

Prioritize these first because they are the most release-blocking:

1. Stream/non-stream parity
2. Channel ingress/delivery reliability
3. Cron worker execution invariants
4. Memory explorer and analytics truthfulness
5. Tool policy/approval/browser/admin parity
6. Routing profile/metascore execution correctness

### Week 3+ — Coverage Completion

1. Fill remaining route and integration holes.
2. Expand smoke and soak coverage.
3. Add missing docs/claim checks.
4. Re-run the full gate repeatedly until stable.
5. Add release/site/install/update compatibility tests and dry runs.

## Allowed Architectural Work During Transition

Allowed:

- refactors that improve parity delivery
- abstractions needed for roadmap-critical future features
- fitness tests, regression tests, and smoke/soak tooling
- de-advertising incomplete surfaces

Not allowed:

- shipping new Go-only features that make parity harder to prove
- connector-local business-logic patches that create new divergence
- UI/CLI claims for incomplete features
- replacing honest explicit unavailability with silent no-op behavior

## Recommended Tracking Format

The execution board should track each item with:

- checklist ID
- workstream
- status
- owner
- linked regression IDs
- implementation PR/reference
- remaining blockers

## Final Operating Sequence

1. Release Roboticus `v0.11.4`
2. Freeze baseline
3. Enumerate gaps
4. Close required gaps
5. Harden regression coverage
6. Pass release gates
7. Accept Roboticus as the replica
8. Start the next Go-native feature wave

That is the actionable path for the transition.
