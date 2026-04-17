# System 11: Scheduler, Automation, and Cron Runtime

## Status

- Owner: parity-forensics program
- Audit status: `not started`
- Last updated: 2026-04-17
- Related release: v1.0.6

## Why This System Matters

Scheduled execution is a separate runtime contract. If it drifts, the system can
look healthy in interactive paths while silently failing on recurring work,
leases, wakeups, or execution guarantees.

## Scope

In scope:

- cron job storage and lease semantics
- scheduled execution entrypoints
- automation / heartbeat wakeup behavior
- dedup and execution ownership for scheduled turns

Out of scope:

- interactive channel connectors

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Scheduler runtime | `src/.../schedule*`, `src/.../cron*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Scheduler runtime | `internal/schedule/*` |
| Cron connectors / CLI | `cmd/schedule`, `internal/channel/*`, `internal/daemon/*` |

## Live Go Path

The live path spans persisted jobs, lease acquisition, schedule wakeup, and
pipeline invocation. This system should be audited as one lifecycle, not a bag
of helpers.

## Artifact Boundary

- persisted cron job row
- active lease state
- scheduled execution record / resulting turn

## Success Criteria

- Closure artifact(s):
  - scheduled job persistence + live execution outcome
- Live-path proof:
  - integration tests prove jobs lease, fire, and invoke the canonical pipeline
- Blocking conditions:
  - lease behavior differs from intended runtime semantics
  - scheduled paths bypass the same behavioral authority used by interactive
    turns
- Accepted deviations:
  - Go-specific automation UX is acceptable if runtime guarantees stay intact

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-11-001 | P1 | Scheduler lifecycle has not yet been parity-classified end to end | Rust scheduler semantics need explicit comparison | Go scheduling code exists and has known fixes, but the full runtime lifecycle is not yet documented in parity-forensics | Open | Open | `internal/schedule/*`, prior lease fixes |

## Intentional Deviations

- Thread heartbeat automation may remain a Go/Codex environment-specific
  extension if it does not undermine the underlying scheduler guarantees.

## Remediation Notes

Promoted from an implicit concern under System 07.

## Downstream Systems Affected

- System 07: install/update/service lifecycle
- System 09: observability

## Open Questions

- Are automation and cron semantics one system or two tightly-coupled layers?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
