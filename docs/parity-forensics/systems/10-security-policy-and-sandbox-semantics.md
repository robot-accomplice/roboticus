# System 10: Security, Policy, and Sandbox Semantics

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-17
- Related release: v1.0.6

## Why This System Matters

This is the enforcement layer for what the agent is allowed to do. If it drifts,
the model can either self-censor incorrectly or execute outside the intended
operator contract.

## Scope

In scope:

- authority resolution and security claims
- tool policy evaluation and deny/approve flows
- workspace / allowed-path sandbox semantics
- filesystem path normalization rules
- config-protection and execution-truth enforcement where they intersect policy

Out of scope:

- generic verifier quality checks unrelated to policy
- installer/update lifecycle

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Security claim composition | `ARCHITECTURE.md`, `src/.../security_claim*` |
| Policy / sandbox semantics | `src/.../policy*`, `src/.../tool_runtime*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Security claims | `internal/core/security_claim.go` |
| Policy engine | `internal/agent/policy/engine.go` |
| Tool/runtime sandboxing | `internal/agent/tools/builtins.go`, `internal/pipeline/sandbox_*`, `internal/pipeline/guards*.go` |
| Authority resolution in pipeline | `internal/pipeline/config.go`, `internal/pipeline/*` |

## Live Go Path

Policy semantics are enforced across multiple live seams:

1. connector/pipeline authority setup
2. tool policy evaluation before execution
3. filesystem resolution inside tool/runtime helpers
4. guard-layer truth/protection checks after inference

This is a cross-cutting system by definition; the audit target is consistent
operator-visible behavior across all of those seams.

## Artifact Boundary

The key artifacts are:

- resolved authority / claim for a turn
- tool approval / denial result
- actual filesystem path admitted or denied
- final surfaced denial reason

## Success Criteria

- Closure artifact(s):
  - resolved authority/claim
  - tool allow/deny decisions
  - surfaced denial reason returned to the operator
- Live-path proof:
  - behavior tests prove path rules, config protection, and deny reasons on the
    actual tool/runtime path
  - no connector or helper bypasses the policy engine with its own shadow logic
- Blocking conditions:
  - sandbox semantics differ between prompt guidance and actual runtime
  - authority resolution differs by transport without explicit justification
  - denial reasons are lost or replaced with model speculation
- Accepted deviations:
  - Go-only protections may remain if they tighten safety without weakening the
    operator contract or hiding the true denial surface

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-10-001 | P1 | Security claim composition still needs full parity classification | Rust treats claim composition as a first-class pipeline concern | Go has `SecurityClaim` machinery plus simpler authority paths; the exact live ownership and transport consistency still need a line-by-line sweep | Open | Open | `internal/core/security_claim.go`, `internal/pipeline/config.go` |
| SYS-10-002 | P1 | Sandbox semantics are enforced in more than one layer | Rust intent is one coherent operator contract | Go currently enforces path constraints through policy, tool/runtime helpers, and protection guards; the system is stronger than before but still needs explicit cross-layer audit | Open | Open | `internal/agent/policy/engine.go`, `internal/agent/tools/builtins.go`, `internal/pipeline/guards*.go` |
| SYS-10-003 | P1 | Model self-censorship must not replace real policy decisions | Tool/runtime policy should be the source of truth | Go has already fixed several prompt/runtime mismatches here, but this concern deserves explicit ownership in the parity program | Improvement candidate | Open | soak fixes, `prompt.go`, policy/tool tests |

## Intentional Deviations

- Go may keep stricter surfaced protections than Rust if they remain grounded in
  actual policy outcomes rather than speculative refusals.

## Remediation Notes

This system was previously distributed across Systems 04, 07, and 08. It is
now explicit because policy/sandbox drift is too important to leave implicit.

## Downstream Systems Affected

- System 04: verification and guards
- System 07: install/update/config loading
- System 08: MCP and external integrations

## Open Questions

- Is `SecurityClaim` now the true cross-transport authority artifact, or still a
  partially bypassed capability?
- Which sandbox rules are authoritative when prompt guidance and runtime checks
  differ?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
