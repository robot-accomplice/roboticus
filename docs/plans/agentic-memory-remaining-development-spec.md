# Agentic Memory Remaining Development Spec

> Status: Active development spec
> Date: 2026-04-15
> Companion documents:
> - [agentic-memory-architecture-roadmap.md](/Users/jmachen/code/roboticus/docs/plans/agentic-memory-architecture-roadmap.md)
> - [semantic-retrieval-fts-vector-migration.md](/Users/jmachen/code/roboticus/docs/plans/semantic-retrieval-fts-vector-migration.md)

---

## Purpose

This document defines the remaining implementation work for the agentic
memory architecture after the milestone audit. Its job is to turn the
roadmap's remaining open items into executable slices with clear scope,
file targets, acceptance criteria, and non-goals.

As of this spec:

- Milestones 1, 2, 4, 5, 6, 7, and 8 are materially delivered.
- Milestone 3 is delivered through M3.2, with M3.3 remaining as an
  operator-driven cleanup gate rather than an implementation gap.

The roadmap must not overstate cleanup closure: the residual `LIKE` deletion
step stays open until production telemetry proves a tier is dormant.

---

## Remaining Work Overview

### Open core work

1. M3.3 operator cleanup: use production telemetry to decide whether each
   covered tier's residual `LIKE` safety net can be removed.

### Deferred work

1. Appendix A: observability dashboards
2. Appendix B: evaluation matrix / harness
3. Appendix C: fallback strategy
4. Optional final cleanup: full removal of `LIKE` after telemetry proves the
   safety net is quiet tier by tier

---

## Track 1: M3 Semantic Read-Path Migration

### Problem statement

The ingestion-side semantic architecture is in place, and the read side is
now migrated through M3.2. Residual `LIKE` queries remain only as safety
nets pending the empirical retirement gate.

The migration plan in
[semantic-retrieval-fts-vector-migration.md](/Users/jmachen/code/roboticus/docs/plans/semantic-retrieval-fts-vector-migration.md)
is the authoritative scope for this track.

### Slice M3.1: FTS Trigger Completeness — **Shipped**

> Status: closed 2026-04-15. Implementation in migration 048 and
> regression coverage R-AGENT-136 / R-AGENT-137.

#### Goal

Make `memory_fts` correct for every currently covered tier so HybridSearch
can become the real primary read path without silently losing rows.

#### File targets

- [internal/db/migrations/](/Users/jmachen/code/roboticus/internal/db/migrations)
- [internal/db/schema.go](/Users/jmachen/code/roboticus/internal/db/schema.go)
- [testutil/](/Users/jmachen/code/roboticus/testutil)
- new regression coverage under `internal/db/` or `internal/agent/memory/`

#### Delivered work

- Add a migration `048_fts_trigger_completeness.sql` that:
  - adds the missing INSERT trigger for `semantic_memory`
  - adds UPDATE triggers for `procedural_memory`
  - adds UPDATE triggers for `relationship_memory`
  - backfills `memory_fts` for rows written before those triggers existed
- Ensure all trigger creation is idempotent.
- Add one regression that proves INSERT / UPDATE / DELETE keep `memory_fts`
  synchronized for all covered tiers.

#### Acceptance criteria

- `semantic_memory`, `procedural_memory`, `relationship_memory`, and
  `episodic_memory` all keep `memory_fts` synchronized across insert,
  update, and delete operations.
- A semantic row inserted through the normal manager path becomes
  FTS-discoverable without requiring a later mutation.
- Tests fail if a future migration removes or forgets one of these
  trigger classes.

#### Non-goals

- No retrieval ranking changes yet.
- No `LIKE` removal yet.

### Slice M3.2: HybridSearch-First Retrieval — **Shipped**

> Status: closed 2026-04-15. Implementation in
> `internal/agent/memory/retrieval_path.go` (trace surface),
> `internal/agent/memory/retrieval_tiers.go` (semantic / procedural /
> relationship rewrites), `internal/agent/memory/workflow.go`
> (`findWorkflowsHybrid` + `loadWorkflowsByIDs`), and
> `internal/pipeline/pipeline_run_stages.go` (per-turn tracer wiring).
> Regressions R-AGENT-138 through R-AGENT-144 lock the path-classification
> contract in.

#### Goal

Make HybridSearch the actual primary read path for semantic, procedural,
relationship, and workflow retrieval.

#### File targets

- [internal/agent/memory/retrieval_tiers.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval_tiers.go)
- [internal/agent/memory/workflow.go](/Users/jmachen/code/roboticus/internal/agent/memory/workflow.go)
- [internal/agent/tools/memory_recall.go](/Users/jmachen/code/roboticus/internal/agent/tools/memory_recall.go)
- [internal/pipeline/trace.go](/Users/jmachen/code/roboticus/internal/pipeline/trace.go)
- targeted tests in `internal/agent/memory/` and `internal/agent/tools/`

#### Required work

- Make HybridSearch primary in:
  - semantic evidence retrieval
  - procedural evidence retrieval
  - relationship evidence retrieval
  - workflow search
- Keep `LIKE` only as a safety net when both FTS and vector legs return
  nothing.
- Add trace attribution for retrieval path usage, e.g.:
  - `retrieval.path=fts`
  - `retrieval.path=vector`
  - `retrieval.path=hybrid`
  - `retrieval.path=like_fallback`
- Preserve current authority, canonical, provenance, and freshness handling.

#### Acceptance criteria

- Relevant rows are returned via HybridSearch without touching the `LIKE`
  block in the normal covered case.
- Trace metadata makes it possible to measure when `LIKE` fallback still
  fires.
- Existing semantic / procedural / workflow retrieval regressions still pass.
- New regressions prove HybridSearch-first behavior on seeded corpora.

#### Non-goals

- No full deletion of `LIKE` yet.
- No new vector infrastructure beyond using what already exists.

### Slice M3.3: Telemetry-Backed LIKE Retirement

#### Status (2026-04-15)

**Telemetry-collection surface shipped; deletion correctly gated on
production observation.** The dormancy-establishment work is in place
(`internal/agent/memory/retrieval_path_telemetry.go`,
regressions R-AGENT-153 through R-AGENT-157). The literal LIKE-block
removal step deliberately remains pending — the dev spec gates it on
"telemetry shows fallback is effectively unused before removal," and
that is an empirical condition that requires observed production
traces, not a decision the agent can make from a fresh fixture corpus.

#### Goal

Delete the `LIKE` safety net only after evidence shows it is no longer doing
meaningful work for FTS-covered tiers.

#### File targets

- [internal/agent/memory/retrieval_path_telemetry.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval_path_telemetry.go) — dormancy aggregator (shipped)
- [internal/agent/memory/retrieval_tiers.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval_tiers.go) — LIKE block removal target (pending observation)
- [internal/agent/tools/memory_recall.go](/Users/jmachen/code/roboticus/internal/agent/tools/memory_recall.go) — LIKE block removal target (pending observation)
- release docs / roadmap

#### Required work

- ✅ Use the trace path annotations from Slice M3.2 to establish whether
  fallback is effectively dormant. — `AggregateRetrievalPaths(ctx, store, limit)`
  scans persisted `pipeline_traces.stages_json`, parses each
  `retrieval.path.<tier>` annotation, and returns a per-tier
  `RetrievalPathTierStats` with `LikeFallbackPct` and a derived
  `IsDormant` flag. The flag is gated on BOTH a fallback share at or
  below `RetrievalPathRetirementThreshold` (1%) AND a sample size of
  at least `minSampleForDormancy` (200 observations), so a barely-
  queried tier can't be retired on weak evidence.
- ⏳ Remove `LIKE` blocks only for tiers whose `IsDormant` flag has
  been true across a meaningful operator-observed window. The
  retirement procedure is:
    1. Operator runs `AggregateRetrievalPaths` against production
       traces from a recent N-day window (suggested: 7 days minimum).
    2. For each tier where `IsDormant` is true with TotalMeasured
       comfortably above `minSampleForDormancy`, the operator deletes
       the corresponding LIKE block in `retrieval_tiers.go` and
       updates the tier method's docstring to note that the safety
       net was retired and on what evidence.
    3. After deletion, the existing M3.2 path-classification regression
       (R-AGENT-138 through R-AGENT-144) MUST still pass — the
       expectation flips so `like_fallback` becomes unreachable for
       the deleted tier.
    4. The retirement is recorded in the release notes and the dev
       spec is updated to mark the slice fully closed.
- ⏳ Leave non-covered tiers alone until they get explicit FTS support.

#### Acceptance criteria

- ✅ Telemetry surface available before removal: `AggregateRetrievalPaths`
  reports per-tier `LikeFallbackPct` with a sample-size guard so the
  dormancy decision is evidence-backed.
- ⏳ `LIKE` no longer appears in the covered retrieval paths except for
  comments or intentionally out-of-scope helpers (pending operator-
  observed dormancy).
- ⏳ Telemetry shows fallback is effectively unused before removal
  (pending observation window).
- Regression tests still pass after removal (will be checked at the
  retirement step).

#### Non-goals

- No expansion into tables that still lack FTS coverage such as
  `learned_skills`.
- No removal of the safety net based on test-fixture corpora alone —
  the empirical-evidence gate is the whole point of M3.3.

---

## Track 2: M8 Relational Distillation — **Shipped**

> Status: closed 2026-04-15. Implementation in
> `internal/agent/memory/reflection.go` (`Relations` field +
> `extractEpisodeRelations` + `FormatForStorage` round-trip),
> `internal/agent/memory/consolidation_distillation.go`
> (`MinRelationDistillSupport`, relation tally + `StoreKnowledgeFact`
> upsert via the canonical write gate), and
> `internal/agent/memory/m8_relational_distillation_test.go` (regressions
> R-AGENT-145 through R-AGENT-152).

### Problem statement

Reflection and distillation now promote recurring patterns into both
`semantic_memory` and `knowledge_facts`, closing the relational-promotion
gap that originally kept Milestone 8 open.

### Goal

Promote recurring entity-relation patterns extracted from enriched episode
summaries into `knowledge_facts` with conservative thresholds so the graph
layer learns from repeated successful experience without anecdote hijacking.

### File targets

- [internal/agent/memory/consolidation_distillation.go](/Users/jmachen/code/roboticus/internal/agent/memory/consolidation_distillation.go)
- [internal/agent/memory/reflection.go](/Users/jmachen/code/roboticus/internal/agent/memory/reflection.go)
- [internal/agent/memory/manager.go](/Users/jmachen/code/roboticus/internal/agent/memory/manager.go)
- [internal/db/memory_repo.go](/Users/jmachen/code/roboticus/internal/db/memory_repo.go)
- regression coverage in `internal/agent/memory/`

### Delivered work

- Extend the enriched episode representation so relation-bearing evidence
  can be recovered during distillation.
- Define a conservative extraction rule for recurring relation candidates:
  - same subject
  - same canonical relation type
  - same object
  - repeated across multiple successful, high-quality episodes
- Reuse the existing canonical graph relation write gate.
- Upsert promoted relations into `knowledge_facts` with provenance that
  indicates they were distilled from episodes rather than ingested from
  semantic docs.
- Keep thresholds stricter than direct semantic-ingestion extraction.

### Acceptance criteria

- Repeated high-quality relation patterns are promoted into
  `knowledge_facts`.
- Failed or low-quality episodes do not drive relational promotion.
- Promotion is idempotent across repeated consolidation runs.
- Graph retrieval can surface a distilled relation after promotion.

### Non-goals

- No free-form relation inference from arbitrary prose.
- No relaxation of canonical relation enforcement.

---

## Cross-Cutting Requirements

### Roadmap truthfulness

- The roadmap status table must match the implementation state.
- A milestone may not be marked `Acceptance met` while its own acceptance
  criteria are still explicitly open.
- When a remaining slice is large enough to deserve its own scoping doc,
  the roadmap should link to that doc instead of implying closure.

### Traceability

- Every remaining slice must emit enough trace metadata to prove it is
  actually being exercised in production behavior.
- New trace namespaces or keys must be documented in the roadmap or
  supporting spec docs.

### Regression discipline

- Every slice must add or update regression rows in
  [regression-test-matrix.md](/Users/jmachen/code/roboticus/docs/regression-test-matrix.md).
- The roadmap should reference the regression class for major acceptance
  claims where practical.

---

## Recommended Next Actions

1. Run `AggregateRetrievalPaths` against production traces over a meaningful
   window and record per-tier dormancy.
2. Retire residual `LIKE` safety nets only for tiers whose dormancy gate is
   satisfied with adequate sample size.
3. Update release-facing docs to reflect each retirement step.
4. Complete appendices A/B/C.

---

## Definition of Done

The remaining agentic-memory work is done when:

- per-tier telemetry-backed `LIKE` retirement decisions have been made and
  implemented where warranted
- the roadmap no longer overstates or understates milestone closure
- the regression matrix covers the shipped behavior and the cleanup gates
- the release docs describe the remaining state honestly
