# System 14: Cache and Replay Semantics

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-17
- Related release: v1.0.6

## Why This System Matters

Cache behavior can create some of the most misleading regressions: the runtime
looks fast and stable while silently serving stale, weakly-guarded, or
differently-routed output. This concern is cross-cutting enough to deserve its
own audit track.

## Scope

In scope:

- semantic/speculation cache lookup and bypass semantics
- cached-response guard application
- replay/reuse behavior where it materially changes the live result
- prompt-compression interaction when cache is involved

Out of scope:

- general routing unrelated to cache

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Cache / replay semantics | `src/.../cache*`, `src/.../inference_pipeline*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| LLM cache | `internal/llm/cache*`, `internal/llm/service_cache.go` |
| Pipeline cache stage | `internal/pipeline/pipeline_run_stages.go` |
| Prompt-compression soak harness | `scripts/run-prompt-compression-soak.py` |

## Live Go Path

Cache semantics cross the LLM service and the pipeline. The authoritative audit
surface is what reaches the user after cache lookup, guard filtering, and any
replay-specific logic.

## Artifact Boundary

- cache hit/miss decision
- cached response after guard processing
- observable response content delivered to the caller

## Success Criteria

- Closure artifact(s):
  - cached response artifact after all live-path filtering
- Live-path proof:
  - tests prove cache-hit outputs go through the same relevant guard/policy
    boundaries as fresh inference
  - paired live soaks prove compression/cache interactions do not create
    quality regressions
- Blocking conditions:
  - cached path is weaker than fresh inference in a user-visible way
  - replay semantics are inferred from helpers rather than proven on the live
    path
- Accepted deviations:
  - Go-specific cache optimizations are acceptable if safety and behavioral
    equivalence are preserved

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-14-001 | P1 | Cached path historically bypassed stronger live-path filtering | Cached responses should remain behaviorally equivalent enough to fresh inference | Go has already closed one major gap by applying contextual guards on cache hits, but the full cache/replay surface is still not classified end to end | Improved, not closed | Open | `internal/pipeline/pipeline_run_stages.go`, `internal/pipeline/guard_retry_artifacts_test.go` |
| SYS-14-002 | P1 | Prompt compression quality risk needs its own cache-aware audit surface | Rust had a compression gate, but quality acceptance must be proved, not assumed | Go now has a paired soak harness specifically because the feature is considered suspect until live evidence clears it | Open | Open | `scripts/run-prompt-compression-soak.py`, release notes |

## Intentional Deviations

- Go is right to reject Rust-era compression behavior if it degrades quality.

## Remediation Notes

Promoted from Systems 01/04/05 because cache semantics repeatedly created
surprise behavior and deserve a first-class artifact boundary.

## Downstream Systems Affected

- System 01: request construction
- System 04: guards and post-processing
- System 05: routing and model selection

## Open Questions

- Which replay surfaces besides the main semantic cache materially affect live
  behavior?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
