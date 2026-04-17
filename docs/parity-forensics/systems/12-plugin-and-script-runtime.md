# System 12: Plugin and Script Runtime

## Status

- Owner: parity-forensics program
- Audit status: `not started`
- Last updated: 2026-04-17
- Related release: v1.0.6

## Why This System Matters

Plugins and scripts extend the tool/runtime surface. If parity or architectural
discipline is weak here, the system can look aligned in built-ins while drifting
at the extension boundary.

## Scope

In scope:

- plugin discovery and loading
- script/plugin execution contract
- tool registration from plugin/runtime sources
- lifecycle and failure handling for extension code

Out of scope:

- built-in tool semantics already covered elsewhere

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Plugin/script runtime | `src/.../plugin*`, `src/.../script*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Plugin runtime | `internal/plugin/*`, `script.go`, `internal/agent/tools/*` |

## Live Go Path

Extension runtime crosses discovery, registration, and execution. The audit
needs to track the full lifecycle, not just whether a script executor exists.

## Artifact Boundary

- discovered plugin/script entries
- registered tool surface from those entries
- execution outcome for an extension-backed tool

## Success Criteria

- Closure artifact(s):
  - registered extension-backed tool set
  - successful and failed execution outcomes
- Live-path proof:
  - tests prove extension-backed tools reach the same runtime surfaces as
    built-ins
- Blocking conditions:
  - extension runtime has helper-only coverage but no live-path proof
- Accepted deviations:
  - Go-native plugin ergonomics are acceptable if runtime ownership is still
    coherent

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-12-001 | P2 | Plugin/script runtime parity has not been explicitly classified | Rust extension behavior needs source-anchored comparison | Go has known plugin/script capability, but it is not yet first-class in the parity ledger | Open | Open | `internal/plugin/*`, `script.go` |

## Intentional Deviations

- Go may legitimately have richer plugin ergonomics; that still needs explicit
  classification, not assumption.

## Remediation Notes

Promoted from an implicit concern under System 08.

## Downstream Systems Affected

- System 02: tool exposure and pruning
- System 08: MCP and external integrations

## Open Questions

- Are plugins and scripts one runtime concern, or should they split later?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
