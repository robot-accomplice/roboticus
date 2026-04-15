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

- Milestones 1, 2, 4, 5, 6, and 7 are materially delivered.
- Milestone 3 is still open on the **semantic read path**.
- Milestone 8 is still open on **relational promotion during distillation**.

The roadmap must not mark these items closed until the acceptance criteria
below are met.

---

## Remaining Work Overview

### Open core work

1. M3 read-path migration: semantic / procedural / relationship retrieval
   must stop relying on residual `LIKE` as a primary path.
2. M8 relational distillation: enriched episode summaries must promote
   recurring entity-relation pairs into `knowledge_facts`.

### Deferred work

1. Appendix A: observability dashboards
2. Appendix B: evaluation matrix / harness
3. Appendix C: fallback strategy
4. Optional cleanup: full removal of `LIKE` after telemetry proves the
   safety net is quiet

---

## Track 1: M3 Semantic Read-Path Migration

### Problem statement

The ingestion-side semantic architecture is in place, but the read side is
not fully migrated. `retrieveSemanticEvidence` still falls back to direct
`LIKE` queries, and the scoping work found correctness gaps in FTS trigger
coverage that make the fallback operationally important today.

The migration plan in
[semantic-retrieval-fts-vector-migration.md](/Users/jmachen/code/roboticus/docs/plans/semantic-retrieval-fts-vector-migration.md)
is the authoritative scope for this track.

### Slice M3.1: FTS Trigger Completeness

#### Goal

Make `memory_fts` correct for every currently covered tier so HybridSearch
can become the real primary read path without silently losing rows.

#### File targets

- [internal/db/migrations/](/Users/jmachen/code/roboticus/internal/db/migrations)
- [internal/db/schema.go](/Users/jmachen/code/roboticus/internal/db/schema.go)
- [testutil/](/Users/jmachen/code/roboticus/testutil)
- new regression coverage under `internal/db/` or `internal/agent/memory/`

#### Required work

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

### Slice M3.2: HybridSearch-First Retrieval

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

#### Goal

Delete the `LIKE` safety net only after evidence shows it is no longer doing
meaningful work for FTS-covered tiers.

#### File targets

- [internal/agent/memory/retrieval_tiers.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval_tiers.go)
- [internal/agent/tools/memory_recall.go](/Users/jmachen/code/roboticus/internal/agent/tools/memory_recall.go)
- release docs / roadmap

#### Required work

- Use the trace path annotations from Slice M3.2 to establish whether
  fallback is effectively dormant.
- Remove `LIKE` blocks only for tiers with complete FTS coverage.
- Leave non-covered tiers alone until they get explicit FTS support.

#### Acceptance criteria

- `LIKE` no longer appears in the covered retrieval paths except for
  comments or intentionally out-of-scope helpers.
- Telemetry shows fallback is effectively unused before removal.
- Regression tests still pass after removal.

#### Non-goals

- No expansion into tables that still lack FTS coverage such as
  `learned_skills`.

---

## Track 2: M8 Relational Distillation

### Problem statement

Reflection and distillation now promote recurring patterns into
`semantic_memory`, but Milestone 8's original acceptance criteria require
promotion into semantic, procedural, and relational stores. The relational
promotion step is still missing.

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

### Required work

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

## Recommended Implementation Order

1. M3.1 — FTS trigger completeness
2. M3.2 — HybridSearch-first retrieval
3. M8 relational distillation
4. M3.3 — optional `LIKE` retirement after telemetry
5. Appendices A/B/C

This ordering reduces correctness risk first, then upgrades the read path,
then closes the remaining consolidation gap, and only then removes the
fallback safety net.

---

## Definition of Done

The remaining agentic-memory work is done when:

- M3 is fully closed in both ingestion and read paths
- M8 promotes into semantic, procedural, and relational stores
- the roadmap no longer overstates milestone closure
- the regression matrix covers the new behavior
- the release docs describe the remaining state honestly
