# System 13: Channel Adapter Behavior

## Status

- Owner: parity-forensics program
- Audit status: `not started`
- Last updated: 2026-04-17
- Related release: v1.0.6

## Why This System Matters

Connectors are supposed to be thin, but that does not mean channel semantics are
irrelevant. If behavior diverges by transport, the architecture can remain
structurally clean while still violating the operator contract.

## Scope

In scope:

- channel-specific parsing and formatting
- transport-preserved metadata
- consent / sender / chat identity propagation
- connector-specific behavior that can alter the effective pipeline input/output

Out of scope:

- core pipeline behavior shared across all channels

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Channel adapters | `src/.../channel*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Channel adapters | `internal/channel/*`, `internal/api/routes/*`, CLI entrypoints |

## Live Go Path

Each adapter translates an external protocol into `pipeline.Input` and formats
`Outcome` back out. The audit target is transport semantics, not shared
business logic.

## Artifact Boundary

- canonical `pipeline.Input` produced by each transport
- channel-visible output shape

## Success Criteria

- Closure artifact(s):
  - normalized pipeline input per channel
  - formatted output per channel
- Live-path proof:
  - integration tests prove that connectors stay thin while preserving the
    right user/session metadata
- Blocking conditions:
  - channel-specific behavior exists but is undocumented
  - connectors mutate shared behavior instead of only translating it
- Accepted deviations:
  - transport-specific formatting is allowed if the underlying behavioral
    contract remains equivalent

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-13-001 | P2 | Channel semantics are not yet first-class in parity documentation | Rust channel behavior needs explicit mapping | Go enforces connector thinness structurally, but the transport-specific semantics are not yet cataloged as a parity system | Open | Open | `internal/channel/*`, route/connectors tests |

## Intentional Deviations

- Go may keep different adapter ergonomics if the normalized `pipeline.Input`
  and user-visible contract remain equivalent.

## Remediation Notes

Promoted from an implicit concern. This system only needs deeper work if actual
behavior drift is found.

## Downstream Systems Affected

- System 01: request construction
- System 07: service/config lifecycle

## Open Questions

- Which channel surfaces are truly behaviorally distinct enough to require their
  own sub-audits?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
