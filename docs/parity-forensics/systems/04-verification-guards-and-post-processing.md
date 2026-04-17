# System 04: Verification, Guards, and Post-Processing

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-17
- Related release: v1.0.6

## Why This System Matters

This system is where the agent decides whether a drafted answer is supported,
complete, policy-safe, and fit to ship back to the user. It is the last line
of defense against confident wrongness.

It is also one of the clearest migration-risk seams because Go has both:

- a stronger typed-evidence direction than before
- a continued fallback to parsing rendered memory text

That means the architecture is improved, but not yet fully disciplined.

## Scope

In scope:

- verifier context construction
- typed verification evidence handoff
- rendered-text fallback behavior
- claim audits, subgoal coverage, freshness/policy checks
- guard-chain and retry ownership at a high level

Out of scope:

- raw retrieval assembly itself
- routing/model selection
- long-term persistence after verification

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
| --------- | ----------------------------- |
| Guard registry and typed guard context | `crates/roboticus-pipeline/src/guard_registry.rs` |
| Full guard retry pipeline | `crates/roboticus-pipeline/src/core/guard_retry.rs` |
| Inference pipeline guard application | `crates/roboticus-pipeline/src/core/inference_pipeline.rs` |
| Truthfulness / action-verification guards | `crates/roboticus-pipeline/src/guard_impls/truthfulness.rs`, `guard_impls/action_verification.rs` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
| --------- | --------------------------- |
| Verification context + verifier | `internal/pipeline/verifier.go:20-280` |
| Typed evidence artifact | `internal/session/verification_evidence.go:1-78` |
| Typed evidence produced by memory assembly | `internal/agent/memory/context_assembly.go:162-225` |
| Guard registry / guard chains | `internal/pipeline/guard_registry.go`, `internal/pipeline/guards*.go` |

## Live Go Path

Current observed state on 2026-04-16:

1. Stage 8.5 retrieval can attach a typed `VerificationEvidence` artifact to
   the session.
2. `BuildVerificationContext` now consumes typed evidence only.
3. Compatibility callers that still set only `MemoryContext` are normalized at
   the session boundary into `VerificationEvidence`, so the verifier itself no
   longer owns rendered-text parsing.
4. Verification logic now performs richer checks than earlier versions:
   subgoal coverage, unsupported subgoals, contradictions, freshness overclaim,
   and action-plan presence.

So the system is materially better than a pure string-parsing verifier. The
remaining architecture work is no longer verifier fallback ownership; it is the
deeper guard-context and retry-path classification work.

## Artifact Boundary

The key artifacts are:

- `session.VerificationEvidence()`
- `VerificationContext`
- final `VerificationResult` / claim audits

Parity is not satisfied unless those artifacts are sourced from structured
inputs on the live path rather than depending on formatting conventions.

## Success Criteria

- Closure artifact(s):
  - `session.VerificationEvidence()`
  - `VerificationContext`
  - applied guard outcomes for the live response
  - final `VerificationResult`
- Live-path proof:
  - runtime-facing tests or traces prove the verifier consumes structured
    evidence on the authoritative path
  - guard outcomes and retries are captured from the live application path, not
    recomputed after the fact
  - contextual guards are shown to receive the intended `GuardContext` on all
    relevant paths
- Blocking conditions:
  - rendered-text fallback still acts as a silent primary path
  - guard retry and verifier retry remain split across multiple plausible
    owners without explicit justification
  - retry evaluation still uses stale context or non-contextual guard
    application remains on important live paths
- Accepted deviations:
  - Go-only verifier enrichments may remain only if they are explicitly
    classified and tied to the same live artifacts

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
| ---- | ---------- | --------- | --------------- | ------------- | ---------------- | -------- | ---------- |
| SYS-04-001 | P1 | Structured evidence had not been fully authoritative | Rust guard pipeline is built around typed guard context and structured inputs | Go now consumes typed evidence only in the verifier. Non-pipeline callers that set only `MemoryContext` are normalized into `VerificationEvidence` at the session boundary before verification begins | Improved | Closed 2026-04-17 | `internal/pipeline/verifier.go`, `internal/session/verification_evidence.go`, `internal/session/session.go` |
| SYS-04-002 | P1 | Verifier behavior could drift with prompt formatting on non-pipeline paths | Guard semantics should not depend on section-header strings | Format-sensitive parsing now lives only in the session-boundary compatibility normalization. The verifier itself no longer depends on rendered section-header strings | Improved, compatibility seam retained | Closed 2026-04-17 | `internal/session/verification_evidence.go`, `internal/pipeline/verifier.go` |
| SYS-04-003 | P1 | `GuardContext` is richer on paper than on the live path | Rust guard/verification context is populated from the actual pipeline state that later guards consume | Go now populates live `GuardContext` with task-intent hints, delegation intent from planned action, enabled subagent names, delegation provenance inferred from delegation/subagent tool results, and the latest selected model when one has already been recorded. Guard-score precompute also now runs on the live path. Remaining gap: not every declared field is sourced from an explicit pipeline artifact yet | Improved, not closed | Open | `internal/pipeline/pipeline.go`, `internal/pipeline/guard_context.go`, `internal/pipeline/guard_context_population_test.go` |
| SYS-04-004 | P1 | Guard retry ownership was duplicated instead of flowing through one authoritative helper | Rust retry behavior is centralized in the guard pipeline | Go now routes live guard-triggered retry through `retryWithGuardsDetailed(...)`, which owns contextual guard application, retry prompt injection, fresh-context re-evaluation, and final applied guard-result capture. The older `retryWithGuards(...)` wrapper is now only a thin compatibility shim over the same implementation | Improved | Closed 2026-04-17 | `internal/pipeline/guard_retry.go`, `internal/pipeline/pipeline_stages.go`, `internal/pipeline/guard_retry_test.go` |
| SYS-04-005 | P1 | Some guard application paths still bypass contextual guard evaluation entirely | Guard behavior should not silently get weaker on specific early-return paths | Early-return `guardOutcome(...)` now rebuilds `GuardContext` from the live session and applies `ApplyFullWithContext(...)`, so skill/shortcut exits no longer silently degrade contextual guards to text-only checks | Fixed | Closed 2026-04-17 | `internal/pipeline/pipeline.go`, `internal/pipeline/pipeline_run_stages.go`, `internal/pipeline/guard_retry_artifacts_test.go::TestGuardOutcome_UsesContextualGuardsWhenSessionAvailable` |
| SYS-04-006 | P2 | Go verifier appears more featureful than earlier parity baseline, but needs explicit classification | Rust guard pipeline has typed context, retries, and deterministic fallbacks | Go now has claim audits, typed evidence, richer freshness checks, and structured verifier trace output, which may be true improvements rather than parity regressions | Improvement candidate | Open | Rust `guard_registry.rs`, `guard_retry.rs`; Go `verifier.go`, `trace.go:164-184` |
| SYS-04-007 | P2 | Guard registry parity still needs its own line-by-line sweep | Rust guard ownership is centralized and explicit | Go now has a centralized `GuardRegistry` with Rust-aligned ordering plus additive Go-only guards, but this system audit has not yet traced every live call site and preset against Rust expectations | Improvement candidate | Open | `internal/pipeline/guard_registry.go`, `internal/pipeline/guards*.go` |
| SYS-04-008 | P1 | Guard retry reuses stale `GuardContext` when evaluating the retry result | Guard retries should evaluate against context derived from the actual retry attempt, not the pre-retry session snapshot | Go now rebuilds `GuardContext` after the retry `RunLoop(...)` before reapplying contextual guards, so retry evaluation sees newly-attached tool results/messages from the actual retry attempt | Fixed | Closed 2026-04-17 | `internal/pipeline/pipeline_stages.go`, `internal/pipeline/guard_retry_artifacts_test.go::TestStandardInference_GuardRetryUsesFreshContext` |
| SYS-04-009 | P2 | Trace/inference metadata capture re-runs guards after the fact instead of preserving the exact applied result | Observability for guards should be derived from the actual guard outcome used on the live path | Go now carries the final applied guard result forward and serializes `InferenceParams.GuardViolations` / `GuardRetried` from that live outcome instead of recomputing on already-sanitized content | Fixed | Closed 2026-04-17 | `internal/pipeline/pipeline_stages.go`, `internal/pipeline/guard_retry_artifacts_test.go::TestStandardInference_InferenceParamsCaptureAppliedGuardViolations` |

## Intentional Deviations

Potential likely improvements that still need explicit classification:

- claim-level audits
- typed evidence artifact living on `session`
- some of the newer freshness and unsupported-subgoal checks
- centralized `GuardRegistry` ordering that now explicitly mirrors the Rust
  chain before appending Go-only guards
- structured verifier trace annotations carrying claim audit JSON

None are accepted yet until the full guard/verification path is compared
line-by-line with Rust.

## Remediation Notes

The main architectural target here is clear:

- typed evidence should remain authoritative on the live path
- rendered-text parsing should stay isolated to explicit compatibility
  normalization and eventually be retired if possible
- guard/retry ownership should remain collapsed onto one authoritative helper
  rather than drifting back to multiple live implementations
- retry-time guard evaluation should rebuild `GuardContext` from the actual
  post-retry session state instead of reusing the pre-retry snapshot
- trace / `InferenceParams` capture must serialize the actual final guard
  outcome used on the live path, not a post-hoc recomputation on the final
  content
- `GuardContext` should either be fully populated on the live path or shrunk so
  it does not imply richer runtime context than guards actually receive
- every live guard-application path should either supply `GuardContext` or be
  explicitly documented as non-contextual

## Downstream Systems Affected

- System 03: Memory retrieval, compaction, and injection
- System 05: Routing and model selection
- System 09: Admin, dashboard, and observability surfaces

## Open Questions

- Is the session-boundary compatibility normalization still needed once all
  callers populate `VerificationEvidence` directly?
- How much of Rust’s guard-retry semantics is already matched by Go versus
  still diverging?
- Should guard-trace / `InferenceParams` capture preserve the exact applied
  guard result instead of recomputing violations after the fact?
- Which `GuardContext` fields are truly needed by live guards, and which are
  currently dead scaffolding from the migration?
- Should the manual verifier retry remain a separate stage after guard retry, or
  should retry orchestration be unified more explicitly?

## Progress Log

- 2026-04-16: Initialized System 04 document.
- 2026-04-16: Recorded the main seam as "typed evidence preferred, but text
  fallback still live."
- 2026-04-16: Added two deeper architecture seams: the live `GuardContext` is
  only partially populated, and retry ownership is duplicated between a shared
  helper and manual pipeline orchestration.
- 2026-04-16: Recorded that some early-return guard application paths still use
  non-contextual guard execution, creating another "weaker live path" seam.
- 2026-04-16: Added two more live-path seams: guard retry currently reuses a
  stale pre-retry `GuardContext`, and final inference metadata recomputes guard
  violations instead of preserving the exact applied guard outcome.
- 2026-04-17: Closed the two concrete live-path seams above. Standard
  inference now rebuilds `GuardContext` after a guard-triggered retry and
  persists `InferenceParams.GuardViolations` / `GuardRetried` from the exact
  final applied guard result instead of re-running guards after the fact.
- 2026-04-17: Closed the early-return contextual-guard seam as well. Skill and
  shortcut exits now run `guardOutcome(...)` with the live session so
  contextual guards receive `GuardContext` instead of silently degrading to
  text-only checks.
- 2026-04-17: Materially improved `GuardContext` population. The live builder
  now carries pipeline task-intent hints, delegation intent, enabled subagent
  names, delegation provenance inferred from tool results, and latest selected
  model when available. Guard-score precompute is now part of the actual guard
  chain path instead of being test-only scaffolding.
- 2026-04-17: Closed the verifier-owned fallback seam. `BuildVerificationContext`
  now consumes typed evidence only; compatibility callers that set only
  rendered `MemoryContext` are normalized at the session boundary via
  `Session.SetMemoryContext(...)`.
- 2026-04-17: Collapsed live guard-triggered retry onto one implementation.
  `runStandardInferenceWithTrace(...)` now delegates guard retry to
  `retryWithGuardsDetailed(...)`, which owns contextual guard application,
  fresh-context rebuild across retries, retry prompt injection, and final
  applied guard-result capture. The older `retryWithGuards(...)` entry point is
  now just a thin wrapper for unit-compatibility callers.
