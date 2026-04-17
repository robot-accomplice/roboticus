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
| --------- | ----------------------------- |
| Security claim composition | `ARCHITECTURE.md`, `src/.../security_claim*` |
| Policy / sandbox semantics | `src/.../policy*`, `src/.../tool_runtime*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
| --------- | --------------------------- |
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
| ---- | ---------- | --------- | --------------- | ------------- | ---------------- | -------- | ---------- |
| SYS-10-001 | P1 | Security claim composition needed live-path proof, not just helper existence | Rust treats claim composition as a first-class pipeline concern | Go now has live evidence that Stage 8 resolves `SecurityClaim`, attaches it to `session.SecurityClaim`, annotates `authority` / `claim_sources` on the trace, and applies threat-caution downgrade on the actual pipeline path. Full transport-by-transport classification is still open, but the old "helper exists but is bypassed" concern is closed | Improved, not closed | Open | `internal/pipeline/config.go`, `internal/pipeline/pipeline_run_stages.go`, `internal/pipeline/security_claim_stage_test.go` |
| SYS-10-002 | P1 | Sandbox semantics are enforced in more than one layer | Rust intent is one coherent operator contract | Go now has a tighter shared runtime helper: filesystem tools resolve through `tools.ResolvePath(...)`, `ValidatePath(...)` shares the same allowed-path semantics, and pipeline session bootstrap snapshots `AllowedPaths` instead of sharing the pipeline slice header. Policy-layer path denial and tool-layer resolution are still distinct seams, but the helper split is materially narrower | Improved, not closed | Open | `internal/agent/tools/sandbox.go`, `internal/agent/tools/builtins.go`, `internal/pipeline/pipeline_stages.go`, `internal/pipeline/sandbox_propagation_test.go`, `internal/agent/tools/sandbox_test.go` |
| SYS-10-003 | P1 | Model self-censorship must not replace real policy decisions | Tool/runtime policy should be the source of truth | Go has already fixed several prompt/runtime mismatches here, but this concern deserves explicit ownership in the parity program | Improvement candidate | Open | soak fixes, `prompt.go`, policy/tool tests |
| SYS-10-004 | P1 | Sensitive config mutation vocabulary drifted between the policy engine and the post-inference guard | Rust intent is one coherent operator contract for protected settings | Go now centralizes protected config filenames and field patterns under `internal/security/config_protection.go`, and both `configProtectionRule` and `ConfigProtectionGuard` consume that shared matcher. This materially reduces policy/guard divergence, though the broader cross-layer audit is still open | Improved, not closed | Open | `internal/security/config_protection.go`, `internal/agent/policy/engine.go`, `internal/pipeline/guards_config_protection.go`, related tests |
| SYS-10-005 | P1 | `FilesystemDenialGuard` treated all filesystem-access disclaimers as false, even when the tool layer had returned a real sandbox denial | Rust intent is to suppress fake capability disclaimers, not overwrite legitimate policy outcomes | Go now lets `FilesystemDenialGuard` pass when tool results contain a real sandbox/path denial marker, while still rewriting false "can't access your files" boilerplate when the turn context contradicts it | Improved, not closed | Open | `internal/pipeline/guards_truthfulness.go`, `internal/pipeline/guards_truthfulness_test.go` |

## Intentional Deviations

- Go may keep stricter surfaced protections than Rust if they remain grounded in
  actual policy outcomes rather than speculative refusals.

## Remediation Notes

This system was previously distributed across Systems 04, 07, and 08. It is
now explicit because policy/sandbox drift is too important to leave implicit.

Current known good state:

- Stage 8 (`authority_resolution`) is a real live owner for `SecurityClaim`
- the resolved claim is attached to the session
- the trace records `authority` and `claim_sources`
- threat-caution downgrade is applied on the live path
- API-key routes do not need to synthesize `ChannelClaimContext`; under
  `AuthorityAPIKey`, Stage 8 resolves directly through `ResolveAPIClaim(...)`
- filesystem tools and generic sandbox validation now share one path-resolution
  contract (`ResolvePath` / `ValidatePath`) for:
  - `~` rejection
  - workspace-relative anchoring
  - boundary-safe allowed-path extension for explicit absolute paths
- live sessions snapshot `AllowedPaths` at creation/load time instead of sharing
  the pipeline slice header, so config reloads or in-place mutations cannot
  silently retcon active sessions' sandbox surface
- sensitive config filenames and field patterns are now owned by one shared
  matcher, so pre-execution policy denial and post-inference config-protection
  guards evaluate the same protected surface
- filesystem-denial truthfulness now respects real sandbox outcomes from the
  tool layer instead of treating every filesystem-access disclaimer as false on
  sight

## Downstream Systems Affected

- System 04: verification and guards
- System 07: install/update/config loading
- System 08: MCP and external integrations

## Open Questions

- Which transport paths still need direct evidence that they feed the right
  claim inputs into the Stage 8 owner?
- Which sandbox rules are authoritative when prompt guidance and runtime checks
  differ?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
- 2026-04-17: Corrected the stale "claim helper may be bypassed" assumption.
  Stage 8 has live-path tests proving `SecurityClaim` resolution, session
  attachment, trace annotation, and threat-caution downgrade. The remaining
  work is transport coverage and broader sandbox classification, not basic
  claim-owner wiring.
- 2026-04-17: Removed dead API-route `Input.Claim` scaffolding for
  `AuthorityAPIKey` presets. The live API authority path is `ResolveAPIClaim`
  at Stage 8, so carrying a channel-claim object in the route only obscured the
  real owner.
- 2026-04-17: Consolidated tool/runtime sandbox resolution under
  `tools.ResolvePath(...)` and updated `ValidatePath(...)` to share the same
  absolute-path semantics. Also moved live session sandbox snapshot ownership
  into `Pipeline.applyRuntimeSessionContext(...)` so `AllowedPaths` are copied
  instead of header-shared across the pipeline/session boundary.
- 2026-04-17: Centralized protected config filenames and field patterns under
  `internal/security/config_protection.go`. The policy engine and the
  `ConfigProtectionGuard` now consume the same matcher instead of carrying
  diverging sensitive-key vocabularies.
- 2026-04-17: `FilesystemDenialGuard` now consults tool results and passes when
  the turn contains a real sandbox/path denial, while still stripping or
  retrying the old fake capability-disclaimer boilerplate when the turn context
  contradicts it.
