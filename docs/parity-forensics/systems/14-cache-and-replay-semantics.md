# System 14: Cache and Replay Semantics

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-18
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
| SYS-14-001 | P1 | Pipeline cache stage previously drifted from live TTL semantics | Cached responses should remain behaviorally equivalent enough to fresh inference | Closed in v1.0.6: pipeline cache reads now honor `expires_at`, pipeline cache writes stamp the same SQLite-friendly TTL window as the main LLM cache, and the pipeline owns its configured TTL explicitly instead of relying on timeless rows | Remediated | Closed | `internal/pipeline/pipeline_cache.go`, `internal/pipeline/pipeline.go`, `internal/daemon/daemon.go`, `internal/pipeline/behavioral_fitness_test.go` |
| SYS-14-002 | P1 | Prompt compression quality risk needs its own cache-aware audit surface | Rust had a compression gate, but quality acceptance must be proved, not assumed | Go now has a paired soak harness specifically because the feature is considered suspect until live evidence clears it | Open | Open | `scripts/run-prompt-compression-soak.py`, release notes |
| SYS-14-003 | P1 | Streaming no-escalate requests previously still allowed cache replay | Benchmark/no-escalate paths should measure fresh model behavior consistently across complete and stream modes | Closed in v1.0.6: `Service.Stream(...)` now mirrors `Complete(...)` and skips cache replay when `NoEscalate` is set directly or via context | Remediated | Closed | `internal/llm/service.go`, `internal/llm/coverage_boost_test.go` |
| SYS-14-004 | P2 | Maintenance cleanup carried a second cache-expiry rule outside the live cache path | Cache cleanup should age out rows on the same `expires_at` contract used by lookup/write paths | `MaintenanceLoopTask` now deletes expired rows from the live `semantic_cache` table by `expires_at <= now` instead of a separate age heuristic on a stale `response_cache` name, removing both the second expiration rule and the stale-table drift | Remediated | Closed | `internal/schedule/tasks.go`, `internal/schedule/tasks_test.go`, `internal/pipeline/pipeline_cache.go`, `internal/llm/cache.go` |
| SYS-14-005 | P1 | Pipeline cache keyed only on normalized user text and ran before request shaping completed | Cache replay should not cross materially different conversation/memory/tool scaffolds, and benchmark/no-escalate turns must bypass replay on the live pipeline path too | Closed in v1.0.6: pipeline cache lookup now runs after tool pruning and hippocampus summary, fingerprints the shaped session scaffold (history, memory artifacts, selected tools, channel/agent context), and skips both replay and store when `NoEscalate` is set | Remediated | Closed | `internal/pipeline/pipeline.go`, `internal/pipeline/pipeline_cache.go`, `internal/pipeline/pipeline_run_stages.go`, `internal/pipeline/behavioral_fitness_test.go` |

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
- 2026-04-17: Closed the stale-entry seam. Pipeline cache reads now filter on
  `expires_at`, writes stamp explicit TTL metadata, and cache timestamps use a
  SQLite-friendly format consistently enough that lexical expiry checks are no
  longer relying on mixed timestamp encodings.
- 2026-04-17: Closed the stream replay contamination seam. `NoEscalate`
  now suppresses cache replay for streaming requests too, so benchmark/raw
  capability paths do not diverge between complete and stream modes.
- 2026-04-18: Closed the maintenance-expiry seam. Scheduler cleanup no longer
  carries a second age-based eviction rule for a stale cache-table name; it now
  deletes expired rows from the live `semantic_cache` table on the same
  `expires_at` contract used by the cache read/write paths.
- 2026-04-18: Closed the pipeline replay-equivalence seam. The pipeline cache
  no longer keys on bare prompt text or runs before the request-shaping stages
  complete. Cache lookup now happens after tool pruning and hippocampus summary,
  fingerprints the shaped session scaffold, and treats `NoEscalate` as a
  replay/store bypass on the pipeline path just like the lower LLM cache path.
- 2026-04-18: Hardened the paired prompt-compression soak harness so a stalled
  lane now fails decisively with structured `harness_error` output when it
  times out before producing a report. This does not clear prompt compression;
  it makes the remaining quality gate produce actionable failure artifacts
  instead of hanging or dying opaquely.
