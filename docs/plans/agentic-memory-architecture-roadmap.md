# Agentic Memory Architecture Roadmap

> Purpose: turn the hybrid-memory / retrieval audit into a concrete delivery
> plan with milestones, file targets, and acceptance criteria.
>
> Status: Active execution document
> Date: 2026-04-14
> Scope: perception/intent, planning, multi-store retrieval, context assembly,
> verification, learning loop, and memory persistence.

---

## Executive Summary

Roboticus already has a meaningful amount of the target architecture in place:

- A distinct memory subsystem with working, episodic, semantic, procedural,
  and relationship stores
- Query decomposition, retrieval routing, reranking, and structured context
  assembly on the read path
- Guarded tool execution with policy enforcement and auditability
- Post-turn reflection and consolidation machinery
- Working memory persistence on shutdown, plus restore-and-vet behavior on
  startup

The biggest remaining gap is not "missing concepts" so much as "runtime
decisiveness." Several layers exist in code but are not yet the authoritative
production path for reasoning and action. The roadmap below focuses on fixing
that first.

---

## Execution Tracker

This document is the working source of truth for `v1.0.6` execution. It should
be updated whenever a milestone meaningfully advances, acceptance criteria are
closed, or the critical path changes.

### Milestone Status

| Milestone | Title | Status | Notes |
|-----------|-------|--------|-------|
| 1 | Unify The Production Retrieval Path | Acceptance met | Pipeline-prepared memory/index preferred by runtime; inference stage emits a `retrieval.*` artifact hash (context + index + combined) onto every trace; a parity fitness proves standard and streaming sessions compute identical hashes and detects drift |
| 2 | Make Intent And Retrieval Routing Real Decision Inputs | Acceptance met | Unified `PerceptionArtifact` (intent, risk, source-of-truth, required tiers, decomposition, freshness, confidence) is computed in pipeline, stashed on session, and emitted to traces; retrieval modes already honour intent-driven routing |
| 3 | Upgrade Semantic Memory Into A Canonical Knowledge Layer | In progress | Ingestion-side follow-on is closed: schema now carries `version`, `effective_date`, `superseded_by`, `is_canonical`, `source_label`, and `asserter_id`; canonical is a caller-asserted persisted flag; `IngestPolicyDocument` + `ingest_policy` provide an end-to-end ingestion surface with null-default effective_date, explicit canonical provenance, and no-silent-overwrite guardrails. Read-path migration off residual `LIKE` remains open and is scoped in `semantic-retrieval-fts-vector-migration.md` |
| 4 | Turn Procedural Memory Into Workflow Memory | Acceptance met (follow-on closed) | Workflow schema, Manager API, retrieval precedence over tool stats, post-turn promotion with auto-extracted error modes + preconditions + intent tags, consolidation confidence sync, and an agent-facing `find_workflow` tool with Laplace-smoothed ranking all land |
| 5 | Replace Relationship Memory With Persisted Relational Memory | Acceptance met (follow-ons closed) | Persisted `knowledge_facts` store, graph-aware retrieval, reusable `KnowledgeGraph` API with multi-hop `ShortestPath` / `Impact` / `Dependencies`, a `query_knowledge_graph` agent tool, and a retired permissive path-search fallback with a single canonical-relation source of truth enforced at the write gate |
| 6 | Add A Real Verifier / Critic Stage | Acceptance met (follow-ons closed) | Claim-level certainty classification, provenance coverage, contradiction reconciliation, per-intent proof obligations, a structured claim-to-evidence trace map, and an embedding-backed semantic certainty classifier (lexical-first, semantic-second) that catches paraphrased absolute / hedged claims the lexical markers miss |
| 7 | Deepen Working Memory Into Executive State | Acceptance met (follow-ons closed) | Executive state is persisted, surfaced in context assembly, grows automatically in post-turn, survives restart with a cross-turn regression test, emits operator-auditable trace/log writes, and harvests tool-output facts via a narrow allowlist (recall_memory / search_memories / read_file / query_knowledge_graph / find_workflow) gated on whether the final response actually references them |
| 8 | Improve Reflection And Consolidation Quality | In progress | Enriched episode summaries and semantic distillation are in place, but relational promotion from enriched summaries into `knowledge_facts` remains open, so the milestone is not yet fully closed against its original acceptance criteria |
| A | Observability Dashboards (Appendix A) | Post-plan | Only pick up after milestones 1–8 complete; see Appendix A |
| B | Evaluation Matrix and Test Harness (Appendix B) | Post-plan | Only pick up after milestones 1–8 complete; see Appendix B |
| C | Fallback Strategy (Appendix C) | Post-plan | Verifier retry and routing modes cover some layers today; full fallback ladder only scheduled after milestones 1–8 complete; see Appendix C |

### Completed Slices

- Pipeline-prepared memory context and memory index are now carried through
  session state and preferred by daemon inference assembly.
- Clarification-deflection canned responses are now guarded against when the
  required context is already present.
- Production retrieval now receives intent signals and honors routed retrieval
  modes instead of treating them as advisory only.
- Semantic evidence now preserves provenance, canonical markers, authority
  scoring, and freshness signals through reranking and context assembly.
- The verifier now runs as a retry gate with checks for unsupported certainty,
  contradiction handling, policy anchoring, remediation coverage, and stale
  "current/latest" claims.
- The verifier now parses structured retrieved-evidence items and can reject
  answers that claim a subgoal is resolved without support in the assembled
  evidence.
- Relationship evidence now preserves provenance and age, and a persisted
  `knowledge_facts` store now captures typed graph facts extracted from
  semantic memory.
- Relationship routing now has a graph-aware retrieval mode with one-hop
  dependency expansion from matched entities.
- Graph retrieval can now synthesize explicit path evidence between named
  entities and reverse-dependency impact chains for blast-radius style queries.
- The verifier now parses responses into structured claims with certainty
  (hedged, moderate, high, absolute) and checks each absolute claim for
  evidence support, canonical anchoring, and contradiction reconciliation.
- High-risk queries now fail verification when absolute-claim provenance
  coverage falls below 50% (weak_provenance_coverage) or when individual
  absolute claims lack both evidence support and canonical anchors
  (unsupported_absolute_claim).
- Working memory is now an executive-state store with structured entry types
  for plan, assumption, unresolved_question, verified_conclusion,
  decision_checkpoint, and stopping_criteria, each persisted with a JSON
  payload and grouped by task_id.
- Executive state now survives shutdown/startup vetting under a longer max-age
  cutoff, while transient turn summaries and notes are still discarded.
- Context assembly now surfaces executive state at the top of the
  `[Working State]` section and the verifier now consumes that section to
  reject responses that abandon unresolved questions or claim task completion
  without addressing the current stopping criteria.
- Post-turn reflection now grows executive state automatically: verified
  conclusions are recorded for subgoals that are both covered in the
  response and supported by retrieved evidence, unresolved questions are
  opened for subgoals the turn could not close, and prior unresolved
  questions whose keywords appear in a confident response are resolved.
  Growth is idempotent across repeated runs.
- Pipeline traces now carry a `verifier.*` annotation group with per-claim
  audits (`claim_map_json`), coverage ratio, and counts so operators can
  audit exactly which claims were supported, anchored, or flagged.
- Cross-turn restart regression now proves that a multi-step task survives a
  simulated shutdown/startup cycle with plan, unresolved question,
  assumption, and stopping criterion intact, while transient turn-summary
  and note entries are discarded by the default vet rules.
- Financial/compliance/security queries now enforce per-claim proof
  obligations: absolute claims must either explicitly anchor themselves or
  trace back to canonical-marked evidence, and failures surface as
  `proof_obligation_unmet` in both verifier issues and the trace claim map.
- Decision checkpoints are now recorded automatically when task synthesis
  produces a subgoal set different from the prior plan for the same task,
  with the add/remove diff preserved on the checkpoint payload.
- Executive-state writes are now observable: plan + checkpoint writes are
  annotated onto the pipeline trace under `executive.*`, and post-turn
  growth emits structured log events with an `executive_write` category
  so operators can audit every write.
- Post-turn growth now also records assumptions the agent names explicitly
  in the response, so the next turn's context carries forward the state the
  agent was taking for granted.
- Milestone 7 now also harvests assumption-like facts from tool outputs via
  a narrow allowlist (`recall_memory`, `search_memories`, `read_file`,
  `query_knowledge_graph`, `find_workflow`). Confidence varies by source:
  memory recalls inherit (capped at 0.9); file reads, graph lookups, and
  named workflow gets land at 0.75; searches and find inventories at 0.65.
  A reference gate keeps a fact only when the final response uses
  enough of its keywords, so observation alone never floods working
  memory with ambient facts.
- Milestone 5 now ships a reusable `KnowledgeGraph` API
  (`internal/agent/memory/graph.go`) with multi-hop `ShortestPath`,
  `Impact`, and `Dependencies` traversals over persisted `knowledge_facts`.
- The graph API is now surfaced to the model as the
  `query_knowledge_graph` agent tool (`path`/`impact`/`dependencies`) so
  multi-hop structural queries can be asked directly, not only through
  semantic retrieval side effects.
- Milestone 4 now ships a workflow-memory schema upgrade and a first
  iteration of the workflow manager: procedural retrieval prefers
  workflows over bare tool stats, post-turn detection promotes repeated
  tool chains into versioned workflow entries, and consolidation's
  confidence sync lands instead of silently skipping.

### Current Critical Path

The remaining core execution work is concentrated in two milestone
follow-ons plus the post-plan appendices:

1. **M3 follow-on (read path)** — migrate semantic retrieval off the
   residual `LIKE` path onto hybrid FTS+vector. The ingestion surface
   is closed, but the read path is still open. Scoping document:
   [semantic-retrieval-fts-vector-migration.md](semantic-retrieval-fts-vector-migration.md).
   That plan is currently sequenced as:
   - Slice 1: FTS trigger completeness and backfill correctness
   - Slice 2: HybridSearch-first migration for semantic / procedural /
     relationship / workflow retrieval
   - Slice 3: optional LIKE removal after telemetry
2. **M8 follow-on (relational distillation)** — promote recurring
   entity-relation pairs from enriched episode summaries into
   `knowledge_facts` so consolidation genuinely promotes into semantic,
   procedural, and relational stores.
3. **Appendices A, B, C** — observability dashboards, evaluation
   matrix, and fallback strategy spec work remain sequenced after the
   M3/M8 follow-ons above complete.

---

## Current Assessment

### Strong Today

- Tool execution guardrails and policy enforcement
- Episodic memory storage and hybrid retrieval
- Working memory persistence and startup vetting
- Reflection and consolidation scaffolding
- Pipeline traces and guard-chain observability

### Partial Today

- Semantic retrieval still relies on a residual `LIKE` safety path, and
  the FTS trigger surface is not yet complete for every covered tier
- Planner/decomposer exists and is useful, but remains heuristic rather
  than an explicit dependency/stopping-criteria task graph
- Context assembly is structured and provenance-aware, but still thinner
  than the full claim/source/chronology model in the reference design
- Consolidation now promotes into semantic and procedural memory, but
  relational promotion from enriched episode summaries is still open

### Remaining Gaps

- Semantic read-path migration to hybrid FTS+vector with complete trigger
  coverage and telemetry-backed LIKE retirement
- Relational distillation from enriched episodic summaries into
  `knowledge_facts`
- Appendix work for dashboards, evaluation harnesses, and fallback policy

---

## Explicit Success To Preserve

### Working Memory Persistence

This is already a success and should be treated as a baseline capability, not
as future work.

- Shutdown persistence exists in [working_persistence.go](/Users/jmachen/code/roboticus/internal/agent/memory/working_persistence.go:49)
- Startup vetting exists in [working_persistence.go](/Users/jmachen/code/roboticus/internal/agent/memory/working_persistence.go:68)
- Schema support exists in [041_working_memory_persistence.sql](/Users/jmachen/code/roboticus/internal/db/migrations/041_working_memory_persistence.sql:1)

The roadmap builds on this by making working memory richer, not by rebuilding
the persistence mechanism.

---

## Delivery Principles

1. Make one runtime path authoritative before adding new layers.
2. Prefer improving semantic quality and provenance before adding more volume.
3. Treat episodic memory as suggestive, semantic/procedural memory as
   authoritative, and verification as mandatory for high-risk claims.
4. Keep every milestone shippable with measurable acceptance criteria.

---

## Milestone Plan

## Milestone 1: Unify The Production Retrieval Path

**Status**: Acceptance met

### Goal

Make the pipeline-owned retrieval path authoritative so decomposition, routing,
fusion, reranking, and context assembly are the same path the model actually
sees at runtime.

### Why First

Right now the pipeline builds a memory block, but the daemon/context builder
also reconstructs direct memory and memory index state independently. That
creates architectural drift and makes it hard to trust retrieval-layer
behavior.

### File Targets

- [internal/pipeline/pipeline_run_stages.go](/Users/jmachen/code/roboticus/internal/pipeline/pipeline_run_stages.go:271)
- [internal/pipeline/pipeline_stages.go](/Users/jmachen/code/roboticus/internal/pipeline/pipeline_stages.go:315)
- [internal/daemon/daemon_adapters.go](/Users/jmachen/code/roboticus/internal/daemon/daemon_adapters.go:108)
- [internal/agent/context.go](/Users/jmachen/code/roboticus/internal/agent/context.go:96)
- [internal/session/session.go](/Users/jmachen/code/roboticus/internal/session/session.go:49)
- [internal/pipeline/interfaces.go](/Users/jmachen/code/roboticus/internal/pipeline/interfaces.go:33)

### Work

- Define a single retrieval artifact for inference preparation.
  Suggested shape:
  - direct memory block
  - memory index block
  - retrieval metrics
  - provenance summary for traces
- Change the executor/context-builder path to consume pipeline-prepared memory
  instead of rebuilding its own direct sections independently.
- Preserve the two-stage design:
  - direct inject: working memory + recent activity
  - index inject: query-aware memory index + `recall_memory` / `search_memories`
- Ensure streaming and non-streaming paths use the same retrieval artifact.

### Acceptance Criteria

- Non-streaming and streaming runs receive identical retrieval artifacts for
  the same turn input
- The daemon no longer independently calls `RetrieveDirectOnly()` when the
  pipeline has already prepared retrieval state
- A pipeline trace can show the exact retrieval artifact that reached the model
- Existing smoke and pipeline parity tests remain green

### Progress

- Pipeline-prepared direct memory and memory index are now stored in session
  state and preferred by daemon context assembly.
- Regression coverage exists for preferring pipeline-prepared memory artifacts.
- The inference stage now emits a `RetrievalArtifact` (SHA1 of the session's
  memory context + memory index, a combined hash, byte counts, and
  bounded previews) onto the pipeline trace under the `retrieval.*`
  namespace, so operators can see the exact artifact that reached the
  model on every turn.
- A parity fitness
  (`TestRetrievalArtifact_StandardAndStreamingSessionsMatch`) proves
  standard and streaming sessions that carry identical memory state
  produce identical artifact hashes, and
  `TestRetrievalArtifact_DetectsContextDrift` proves the fitness
  actually catches silent drift between the two paths.

---

## Milestone 2: Make Intent And Retrieval Routing Real Decision Inputs

**Status**: Acceptance met

### Goal

Turn intent/perception from a loose heuristic into a concrete routing decision
that controls memory selection, retrieval mode, and risk posture.

### File Targets

- [internal/agent/intent.go](/Users/jmachen/code/roboticus/internal/agent/intent.go:17)
- [internal/pipeline/intent_registry.go](/Users/jmachen/code/roboticus/internal/pipeline/intent_registry.go:9)
- [internal/pipeline/task_synthesis.go](/Users/jmachen/code/roboticus/internal/pipeline/task_synthesis.go:22)
- [internal/agent/memory/router.go](/Users/jmachen/code/roboticus/internal/agent/memory/router.go:49)
- [internal/agent/memory/retrieval.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval.go:246)

### Work

- Introduce a normalized perception artifact with:
  - intent type
  - risk level
  - preferred source of truth
  - required memory tiers
  - decomposition needed
  - freshness required
- Wire actual intent signals into the retriever.
  The retriever already exposes `SetIntents`; it now needs a production caller.
- Make `RetrievalTarget.Mode` matter during retrieval instead of being advisory.
- Add policy-sensitive routing presets:
  - policy/compliance
  - debugging/root-cause
  - planning/migration
  - open-ended research

### Acceptance Criteria

- Retrieval traces include intent, risk, source-of-truth class, and selected
  memory tiers
- The retriever uses routed mode decisions, not just tier selection
- Policy-style queries prefer semantic/procedural authoritative sources over
  anecdotal episodic recall
- Debugging-style queries show a different tier mix than policy queries

### Progress

- Production daemon retrieval now classifies the query and passes intent
  signals into the retriever.
- `RetrievalTarget.Mode` now affects semantic, procedural, and relationship
  retrieval behavior.
- Relationship-oriented queries now route to `graph` mode instead of plain
  keyword-only lookup.
- `internal/pipeline/perception.go` now builds a unified `PerceptionArtifact`
  (intent, risk, source-of-truth, required_memory_tiers, decomposition_needed,
  freshness_required, confidence) from the task synthesis. High-risk queries
  automatically pull in semantic + relationship tiers; decomposition-heavy
  queries pull in procedural; current-state queries route to an external
  source-of-truth and flag freshness.
- The artifact is stashed on session state (`TaskRisk`, `TaskSourceOfTruth`,
  `TaskRequiredTiers`, `TaskFreshness`) and emitted to pipeline traces under
  the `perception.*` namespace so operators can audit the exact
  classification on every turn.

---

## Milestone 3: Upgrade Semantic Memory Into A Canonical Knowledge Layer

**Status**: In progress

### Goal

Make semantic memory a dependable store of facts, policies, docs, and normalized
knowledge instead of mostly long assistant responses.

### File Targets

- [internal/agent/memory/manager.go](/Users/jmachen/code/roboticus/internal/agent/memory/manager.go:248)
- [internal/agent/memory/retrieval_tiers.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval_tiers.go:10)
- [internal/db/schema.go](/Users/jmachen/code/roboticus/internal/db/schema.go:128)
- [internal/db/hybrid_search.go](/Users/jmachen/code/roboticus/internal/db/hybrid_search.go:21)
- [internal/agent/tools/memory_recall.go](/Users/jmachen/code/roboticus/internal/agent/tools/memory_recall.go:342)

### Work

- Add semantic-source metadata:
  - authority level
  - canonical flag
  - version or effective date
  - source kind
  - superseded-by / supersedes where applicable
- Expand ingestion so semantic entries can come from:
  - docs
  - policy files
  - architecture docs
  - normalized distilled summaries
- Replace semantic `LIKE` retrieval with a real hybrid read path:
  - BM25/FTS
  - vector similarity
  - authority and recency scoring
  - reranking
- Introduce canonical-source enforcement for policy-sensitive questions.

### Acceptance Criteria

- Semantic retrieval no longer depends primarily on `LIKE` search
- Policy queries can return canonical current sources ahead of merely similar
  historical entries
- Semantic memory entries carry enough metadata to distinguish current truth
  from stale but related knowledge
- Tests cover "current canonical source outranks archived source"

### Progress

- Semantic evidence now carries source identity, labels, canonical flags,
  authority scores, and age through retrieval and context assembly.
- Context assembly and verifier now surface freshness risks and canonical-risk
  failures instead of burying them in scoring.
- Migration `046_semantic_supersession.sql` adds `version`,
  `effective_date`, and `superseded_by` to `semantic_memory` with
  supporting indexes. The manager's upsert bumps `version` on value
  change, keeps it stable on idempotent rewrites, and refreshes
  `effective_date` only when a fact actually changes.
- The consolidation contradiction phase now writes `superseded_by`
  alongside the existing `memory_state = 'stale'` flip so retrieval
  and audit tooling can follow the chain.
- `internal/agent/memory/semantic_supersession.go` adds
  `CurrentSemanticValue` (walks the chain with cycle + length guards)
  and `MarkSemanticSuperseded` (manual supersession with replacement
  liveness validation).
- Migration `047_policy_ingestion.sql` adds `is_canonical`,
  `source_label`, and `asserter_id` columns plus a `(is_canonical,
  memory_state, category)` index so the canonical-first read path is
  cheap. Canonical status is now a persisted caller-asserted flag —
  the old "infer canonical from substring matches on category/key"
  path in `semanticAuthority` is gone.
- `Manager.IngestPolicyDocument` provides the ingestion surface with
  the design's full set of guardrails: required category / key /
  content / source_label; `effective_date` defaults to NULL (never
  silently substituted with `now()`); `Canonical=true` requires
  `AsserterID` AND (`Version > 0` OR `EffectiveDate`); silent
  overwrite of an existing `(category, key)` row is rejected unless
  the caller passes `ReplacePriorVersion=true` or supplies a
  strictly-higher version. Replacements flip the prior row to stale
  with `superseded_by` set, integrating with the Milestone 3
  supersession chain so audit trails stay intact.
- New `ingest_policy` agent tool wraps the Manager method as
  `RiskDangerous`. It blocks the calling agent's own identity from
  being used as the canonical asserter, so the agent cannot
  auto-mark its own output as canonical.
- The retrieval reader now SELECTs `is_canonical` and `source_label`
  directly and prefers the persisted source label when present;
  ordering puts canonical rows ahead of non-canonical at equal
  confidence.
- Read-path follow-on remains open: migrating semantic retrieval off the
  residual `LIKE` path onto hybrid FTS+vector. This work is now scoped in
  [semantic-retrieval-fts-vector-migration.md](semantic-retrieval-fts-vector-migration.md)
  because investigation surfaced a correctness gap, not just a quality
  cleanup: `semantic_memory` is missing an INSERT trigger into `memory_fts`,
  and `procedural_memory` / `relationship_memory` are missing UPDATE
  triggers, so the current LIKE blocks are masking real discoverability
  holes.

---

## Milestone 4: Turn Procedural Memory Into Workflow Memory

**Status**: Acceptance met (follow-ons closed)

### Goal

Evolve procedural memory from tool counters into reusable workflows, SOPs,
playbooks, and approved action sequences.

### File Targets

- [internal/agent/memory/retrieval_tiers.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval_tiers.go:53)
- [internal/agent/learning.go](/Users/jmachen/code/roboticus/internal/agent/learning.go:156)
- [internal/pipeline/post_turn.go](/Users/jmachen/code/roboticus/internal/pipeline/post_turn.go:224)
- [internal/db/migrations/040_procedural_upgrade.sql](/Users/jmachen/code/roboticus/internal/db/migrations/040_procedural_upgrade.sql:1)
- [internal/agent/memory/consolidation_phases.go](/Users/jmachen/code/roboticus/internal/agent/memory/consolidation_phases.go:773)

### Work

- Normalize procedural records around:
  - workflow name
  - ordered steps
  - preconditions
  - failure modes
  - context tags
  - version
  - success/failure evidence
- Make procedural retrieval query-sensitive.
- Promote successful repeated tool chains into procedural memory with richer
  metadata than a learned skill name alone.
- Resolve schema/runtime drift so consolidation and retrieval agree on which
  procedural fields exist.

### Acceptance Criteria

- A procedural query returns relevant workflows, not just top tool stats
- Learned procedures include steps and contextual metadata
- Consolidation no longer silently skips procedural confidence/state sync due
  to missing columns
- Tests cover retrieval of the right workflow for a matching procedural query

### Progress

- Migration `045_workflow_memory.sql` adds the missing columns the runtime
  has been expecting (`confidence`, `memory_state`, `state_reason`,
  `version`, `category`, `success_evidence`, `failure_evidence`) plus
  supporting indexes on `category` and `last_used_at`. Consolidation's
  confidence sync now lands instead of silently skipping because the
  column "may not exist".
- `internal/agent/memory/workflow.go` introduces a `Workflow` type and
  Manager methods `RecordWorkflow`, `RecordWorkflowSuccess`,
  `RecordWorkflowFailure`, `GetWorkflow`, and `FindWorkflows`. Updates to
  an existing workflow bump `version` and preserve success/failure
  counters + evidence so the track record survives revisions.
- `retrieveProceduralMemory` now surfaces workflows first (with steps,
  preconditions, tags, success rate) and falls back to tool statistics
  only when no workflow matches the query.
- Post-turn procedure detection now promotes a detected tool chain into
  a workflow entry once its count hits 3, tagging it
  `auto_promoted,tool_chain,intent:*,complexity:*` and recording the
  session ID as success evidence so operators can audit why the promotion
  fired.
- Auto-extraction during promotion now populates `error_modes` from any
  failing tool-result messages in the session (first line per step,
  deduplicated, capped) and seeds `preconditions` from the session's
  task intent, complexity, and subgoals so the workflow record carries
  the context that made it successful.
- Agent-facing `find_workflow` tool now exposes both `find` and `get`
  operations to the model. `find` over-fetches from `FindWorkflows`,
  then re-ranks with a Laplace-smoothed blend: success rate with
  add-one smoothing, failure penalty capped at -0.30, query-token
  overlap across name/steps/tags/preconditions, tag fit weighted
  above query fit (tags are explicit semantic signal), 30-day
  half-life recency decay, and a confidence multiplier clamped to
  [0.1, 1.0]. A ranking floor drops candidates below 0.15 so the
  tool never surfaces workflows the model should not trust.

---

## Milestone 5: Replace Relationship Memory With Persisted Relational Memory

**Status**: Acceptance met (follow-ons closed)

### Goal

Move from lightweight entity trust tracking to a persisted relational layer
capable of representing dependencies, ownership, chronology, and causality.

### File Targets

- [internal/db/schema.go](/Users/jmachen/code/roboticus/internal/db/schema.go:152)
- [internal/agent/knowledge.go](/Users/jmachen/code/roboticus/internal/agent/knowledge.go:19)
- [internal/agent/memory/retrieval_tiers.go](/Users/jmachen/code/roboticus/internal/agent/memory/retrieval_tiers.go:99)
- [internal/agent/memory/manager_entities.go](/Users/jmachen/code/roboticus/internal/agent/memory/manager_entities.go:1)
- [internal/agent/memory/consolidation.go](/Users/jmachen/code/roboticus/internal/agent/memory/consolidation.go:141)

### Work

- Add persisted graph-like tables for:
  - entities
  - relations
  - relation types
  - timestamps/version ranges
  - provenance
- Keep the simple relationship table only as a transitional compatibility layer.
- Add retrieval/traversal for:
  - ownership
  - dependency chains
  - impacted components
  - parent/child relationships
  - causality and timelines
- Update consolidation to promote discovered recurring relations into the
  relational store.

### Acceptance Criteria

- Graph/relational data survives restart
- Debugging/planning queries can traverse dependencies rather than only listing
  frequent entities
- At least one production path uses relational traversal in retrieval
- The in-memory `KnowledgeGraph` is either retired or reduced to a cache over
  persisted data

### Progress

- `knowledge_facts` now exists as a persisted store with provenance,
  confidence, and freshness metadata.
- Semantic ingestion extracts typed facts like `depends_on`, `owned_by`,
  `uses`, `blocks`, `causes`, and `version_of` into that store.
- Retrieval, search, recall, indexing, and stats tooling now treat
  `knowledge_facts` as a first-class store.
- Relationship routing now uses graph-aware retrieval with one-hop expansion
  from matched entities.
- Graph retrieval can now synthesize explicit path and reverse-impact chain
  evidence over persisted facts.
- `internal/agent/memory/graph.go` now exposes a reusable `KnowledgeGraph`
  API with forward + reverse adjacency, configurable-depth `ShortestPath`,
  `Impact` (multi-hop reverse traversal), `Dependencies` (multi-hop forward
  traversal), and `LoadKnowledgeGraph` / `LoadKnowledgeGraphWithLimit`
  helpers that read straight from `knowledge_facts`. The API supports
  arbitrary depth (not the previous hard-coded 2 hops) and is exported so
  tools and tests can traverse the persisted graph without rebuilding BFS
  each call.
- The retrieval tier now consumes the `KnowledgeGraph` API via type aliases
  (`graphFactRow = GraphFactRow`, `graphEdge = GraphEdge`). Path queries
  delegate to `ShortestPath` over the canonical relation set only — the
  historical permissive fallback is retired because the ingestion path
  writes only canonical relations by construction. Impact / dependency
  queries delegate to `Impact` and `Dependencies` with the same depth-2
  cap the retrieval tier had before.
- Multi-hop graph queries are now exposed as an agent tool
  (`query_knowledge_graph`) registered from the daemon. The tool supports
  three operations — `path`, `impact`, `dependencies` — and caps both
  the working-set size (500 facts) and max traversal depth (8 hops) so
  large graphs stay responsive. Output is JSON with a `summary`, the
  discovered hops or nodes, and graph stats for auditability.
- Canonical relations are centralised in `db.CanonicalGraphRelations` with
  `db.IsCanonicalGraphRelation` as the single source of truth:
  `IsTraversableRelation` delegates to it, `StoreKnowledgeFact` enforces
  it at write time, and a parity regression asserts the extractor's
  production patterns and the canonical list stay aligned.

---

## Milestone 6: Add A Real Verifier / Critic Stage

**Status**: Acceptance met (follow-ons closed)

### Goal

Introduce an evidence-aware verification pass that checks completeness,
support, contradictions, and freshness before final answer or action.

### File Targets

- [internal/pipeline/guards_truthfulness.go](/Users/jmachen/code/roboticus/internal/pipeline/guards_truthfulness.go:1)
- [internal/pipeline/guards_financial_verification.go](/Users/jmachen/code/roboticus/internal/pipeline/guards_financial_verification.go:1)
- [internal/pipeline/guard_registry.go](/Users/jmachen/code/roboticus/internal/pipeline/guard_registry.go:13)
- [internal/agent/memory/context_assembly.go](/Users/jmachen/code/roboticus/internal/agent/memory/context_assembly.go:56)
- new verifier module under `internal/pipeline/` or `internal/agent/`

### Work

- Add a verifier artifact that checks:
  - every subgoal answered or explicitly unresolved
  - every material claim supported by retrieved evidence
  - authoritative vs anecdotal source balance
  - freshness requirements
  - contradictions not ignored
  - procedure/policy consistency for actions
- Keep the current guard chain for safety and style, but treat verifier output
  as a separate stage with its own trace metadata.
- Support retry, abstain, or "need more evidence" outcomes.

### Acceptance Criteria

- A run can fail verification without failing guard checks
- Verification metadata appears in pipeline traces
- High-risk answers can be downgraded to uncertainty when support is weak
- Tests cover:
  - unsupported leap
  - contradiction left unresolved
  - stale source beats current source

### Progress

- A verifier stage now runs before final output persistence and can request a
  revision pass.
- The verifier already checks unsupported certainty, missed multi-part
  coverage, contradiction handling, policy anchoring, remediation/next-step
  coverage, and stale "latest/current" claims.
- The verifier now parses `[Retrieved Evidence]` items from the assembled
  context and checks answered subgoals for explicit support before allowing
  them to stand as resolved.
- The verifier now parses each response into structured claims with certainty
  levels (hedged, moderate, high, absolute) and canonical-anchor metadata,
  and runs three additional claim-level checks:
  - `unresolved_contradicted_claim` when an absolute claim echoes contested
    evidence without reconciliation.
  - `weak_provenance_coverage` when fewer than half the absolute claims on a
    high-risk query trace back to evidence or a canonical anchor.
  - `unsupported_absolute_claim` when a single absolute claim on a high-risk
    query has no evidence support and no canonical anchor.
- The verifier also consumes executive state from the `[Working State]`
  section and rejects responses that abandon unresolved questions that the
  current prompt is related to or claim task completion without satisfying
  the active stopping criteria.
- The verifier now produces a structured `ClaimAudit` record for every claim
  it parses (certainty, supported, anchored, reconciled, keyword hits, issue
  code) and emits them onto the pipeline trace via `AnnotateVerifierTrace`
  under the `verifier.*` namespace, including a compact summary (counts,
  coverage ratio) and a JSON claim map operators can audit per turn.
- Financial, compliance, security, and explicit policy-sensitive queries now
  enforce a per-claim proof obligation: every absolute claim must be anchored
  to a canonical source either via explicit in-response attribution or via
  evidence that itself carries a canonical marker (`canonical`, `policy`,
  `documentation`, `runbook`, `standard`, `authoritative`, etc.). Violations
  fail with `proof_obligation_unmet` and are surfaced in the claim trace map.
- The verifier now ships an embedding-backed semantic certainty
  classifier (`internal/pipeline/verifier_classifier.go`). The lexical
  marker pass runs first and is authoritative for known phrases; any
  sentence still tagged `CertaintyModerate` after that pass flows
  through the classifier, which is built from a small adversarial
  exemplar corpus shaped around the verifier failure modes we care
  about (absolute-without-evidence, pseudo-cautious resolution,
  policy / currentness overclaim, remediation-as-fact, softened
  hallucinations). The `SemanticClassifier` uses the configured
  embedder when available and the n-gram fallback otherwise; a
  conservative abstain policy (MinScore 0.30, MinGap 0.10) keeps
  ambiguous embeddings from inventing certainty. Upgraded claims
  carry a `certainty_upgraded` flag onto the trace claim map so
  operators can audit when the embedding-backed pass added value
  beyond lexical matching, and use those audits to evolve the corpus
  from observed misses (regression-asset discipline).

---

## Milestone 7: Deepen Working Memory Into Executive State

**Status**: Acceptance met (follow-ons closed)

### Goal

Build on the existing persistence/vetting success by making working memory the
short-term executive state described in the reference architecture.

### File Targets

- [internal/agent/memory/working_persistence.go](/Users/jmachen/code/roboticus/internal/agent/memory/working_persistence.go:25)
- [internal/db/schema.go](/Users/jmachen/code/roboticus/internal/db/schema.go:97)
- [internal/pipeline/task_synthesis.go](/Users/jmachen/code/roboticus/internal/pipeline/task_synthesis.go:12)
- [internal/pipeline/decomposition.go](/Users/jmachen/code/roboticus/internal/pipeline/decomposition.go:31)
- [internal/agent/memory/context_assembly.go](/Users/jmachen/code/roboticus/internal/agent/memory/context_assembly.go:67)

### Work

- Expand working-memory entry types or add structured payload support for:
  - current plan
  - assumptions
  - unresolved questions
  - verified conclusions
  - decision checkpoints
  - stopping criteria
- Persist a task graph or task-state object per turn/session.
- Update startup vetting to preserve:
  - active goals
  - active decisions
  - verified conclusions
  - unresolved blockers
  while discarding low-value transient chatter.

### Acceptance Criteria

- Working memory contains explicit task-state artifacts, not just turn summaries
- Startup vetting retains active executive state across restarts
- Long multi-step tasks resume coherently after restart
- Tests cover restore/resume of an unfinished multi-step task

### Progress

- Migration `044_working_memory_executive_state.sql` expands the
  `working_memory` table with `task_id` and JSON `payload` columns and
  extends the `entry_type` CHECK to include the executive-state types.
- `internal/agent/memory/executive.go` introduces the executive-state types
  (`plan`, `assumption`, `unresolved_question`, `verified_conclusion`,
  `decision_checkpoint`, `stopping_criteria`) and `RecordPlan`,
  `RecordAssumption`, `RecordUnresolvedQuestion`, `RecordVerifiedConclusion`,
  `RecordDecisionCheckpoint`, `RecordStoppingCriteria`, `ResolveQuestion`,
  `LoadExecutiveState`, and `LoadAllExecutiveState` methods on the Manager.
- `DefaultVetConfig` now retains every executive-state type across startup
  vetting by default, and executive entries get a longer `ExecutiveMaxAge`
  cutoff (7 days default) so multi-day tasks resume coherently after restart.
- `AssembleContext` now loads the latest executive state for the session and
  renders it at the top of the `[Working State]` section so the model and the
  verifier both see the current plan, assumptions, unresolved questions,
  verified conclusions, decision checkpoints, and stopping criteria.
- Task synthesis now records the synthesized plan as a structured plan entry
  in working memory on every turn it fires.
- Post-turn reflection now grows executive state automatically: verified
  conclusions are recorded for subgoals that are both covered in the
  response and supported by retrieved evidence, unresolved questions are
  opened for subgoals the turn could not close, and prior unresolved
  questions whose keywords appear in a confident response are resolved.
- Growth is idempotent across repeated runs so the executive layer does not
  churn on every turn.
- A cross-turn restart regression test now proves that a multi-step task
  resumes after a simulated shutdown/startup cycle with the same plan,
  unresolved question, assumption, and stopping criterion, and that the
  assembled-context block for the next turn renders exactly those items.
- Decision checkpoints are now recorded automatically whenever task synthesis
  produces a subgoal set that differs from the prior plan for the same task.
  The checkpoint payload carries the chosen subgoals, the prior subgoals as
  "considered", and a rationale describing the added/removed diff.
- Executive-state writes are now observable: task-synthesis plan writes are
  annotated onto the pipeline trace under the `executive.*` namespace
  (plan_recorded, subgoals, subgoals_added, subgoals_removed,
  checkpoint_recorded, task_id), and post-turn growth emits structured log
  events with an `executive_write` / `executive_growth` category that carry
  the session, task, and subgoal for every write. `growExecutiveState` now
  returns an `ExecutiveGrowthResult` with counts for tests and telemetry.
- Post-turn growth now also extracts assumptions the agent named explicitly
  in the response (markers like "assuming that", "I'll assume",
  "presumably", etc.) and records each as an `assumption` executive entry
  with source `response` so the agent's own stated assumptions survive into
  the next turn's context and into startup vetting. The extractor is
  word-boundary aware (so "reassuming" is not a match) and deduplicates
  equivalent clauses within a single turn.
- Tool-output assumption extraction now lands as a narrow allowlist
  harvester (`internal/pipeline/tool_facts.go`) covering `recall_memory`,
  `search_memories`, `read_file`, `query_knowledge_graph`, and
  `find_workflow`. Per-source confidence policy: memory recalls inherit
  (capped at 0.9), file/graph/named-workflow gets land at 0.75, search
  inventories and find-workflow inventories at 0.65. A reference gate
  (`FilterFactsReferencedByResponse`) keeps a fact only when enough of
  its keywords appear in the final assistant response, so observed-but-
  unused facts never reach working memory.

---

## Milestone 8: Improve Reflection And Consolidation Quality

**Status**: In progress

### Goal

Turn post-turn learning from heuristic logging into reusable memory shaping.

### File Targets

- [internal/pipeline/post_turn.go](/Users/jmachen/code/roboticus/internal/pipeline/post_turn.go:165)
- [internal/agent/memory/reflection.go](/Users/jmachen/code/roboticus/internal/agent/memory/reflection.go:26)
- [internal/agent/memory/consolidation.go](/Users/jmachen/code/roboticus/internal/agent/memory/consolidation.go:141)
- [internal/agent/memory/consolidation_phases.go](/Users/jmachen/code/roboticus/internal/agent/memory/consolidation_phases.go:279)

### Work

- Enrich reflection with:
  - evidence that mattered
  - failed hypotheses
  - successful fix patterns
  - actual durations and result quality
- Promote:
  - stable facts into semantic memory
  - repeatable tool chains into procedural memory
  - newly discovered dependencies into relational memory
- Add stricter promotion thresholds to prevent anecdote hijacking.

### Acceptance Criteria

- Reflection summaries include more than goal/actions/outcome
- Consolidation can promote into semantic, procedural, and relational stores
- Repeat success patterns become reusable memory, not just archived episodes
- Tests cover "episodic repeated success promotes to procedural/semantic"

### Progress

- `EpisodeSummary` now carries `EvidenceRefs`, `FailedHypotheses`,
  `FixPatterns`, `ErrorsSeen`, `VerifierPassed`, and a blended
  `ResultQuality` score in addition to the original goal/actions/outcome.
- New `AnalyzeEpisode(EpisodeInput)` entry point is the enriched
  reflection call path; the original `Reflect()` remains as a shim for
  callers without evidence/verifier data.
- Post-turn reflection now feeds evidence items, verifier outcome, and
  captured tool error messages into `AnalyzeEpisode`, so the stored
  episode summary records the information consolidation needs to
  promote reusable patterns later.
- A dedicated consolidation phase `phaseEpisodeDistillation` now parses
  enriched episode summaries, counts recurring fix patterns and
  evidence references across successful episodes, and promotes them
  into `semantic_memory` under the `fix_pattern` and `learned_fact`
  categories. Thresholds are deliberately strict (3+ episodes for
  evidence, 2+ for fix patterns) to prevent anecdote hijacking. The
  phase is idempotent via UPSERT on `(category, key)` and the
  `Distilled` counter appears in the consolidation report.
- Remaining work to close the milestone against its original acceptance
  criteria:
  - promote repeated entity-relation pairs observed in enriched summaries
    into `knowledge_facts`, so consolidation genuinely promotes into the
    relational store as well as semantic/procedural stores
  - add a dashboard surface for distilled patterns so operators can see
    what the agent has generalized

---

## Cross-Cutting Trace And Test Work

Every milestone should update traces and tests as part of the same change.

### Trace Targets

- [internal/pipeline/trace.go](/Users/jmachen/code/roboticus/internal/pipeline/trace.go:1)
- [internal/pipeline/pipeline_run_stages.go](/Users/jmachen/code/roboticus/internal/pipeline/pipeline_run_stages.go:269)

### Test Targets

- `internal/agent/memory/*_test.go`
- `internal/pipeline/*_test.go`
- [smoke_test.go](/Users/jmachen/code/roboticus/smoke_test.go:223)
- [docs/regression-test-matrix.md](/Users/jmachen/code/roboticus/docs/regression-test-matrix.md:1)

### Required Additions Per Milestone

- At least one regression test for the bug/risk being addressed
- At least one trace assertion for the new decision point
- Update the regression matrix when a new failure mode is covered

---

## Recommended Release Grouping

| Release | Milestones | Theme |
|---------|------------|-------|
| v1.0.6 | 1 + 2 | Make retrieval path authoritative and intent-driven |
| v1.0.7 | 3 + 4 | Canonical semantic memory + real procedural workflows |
| v1.0.8 | 5 + 6 | Relational memory + verifier/critic |
| v1.0.9 | 7 + 8 | Executive working memory + stronger learning loop |

This ordering keeps each release focused and avoids introducing graph,
verification, and richer learning before the read path can be trusted.

---

## Exit Criteria

The roadmap should be considered complete when all of the following are true:

- One production retrieval path owns decomposition, routing, fusion, reranking,
  and context assembly
- Intent/perception emits a concrete routing decision artifact used in runtime
- Semantic memory can distinguish canonical current truth from stale but
  similar content
- Procedural memory returns workflows, not just historical tool stats
- Relational memory persists explicit dependencies and is used in retrieval
- Verification can block or downgrade unsupported answers before final output
- Working memory resumes active task state across restart while discarding waste
- Reflection and consolidation convert successful experience into reusable
  long-term knowledge

---

## Immediate Next Step

All eight core milestones (M1–M8) are now acceptance-met. The roadmap's
main arc is complete. Remaining work is quality follow-ons listed in
the **Current Critical Path** section above, followed by Appendices A,
B, and C once the follow-ons close.

The highest-leverage follow-on to tackle first is the **semantic
classifier** upgrade for the verifier (M6 follow-on). Lexical marker
matching lets paraphrased claims slip through the current checks, and
embedding-backed classification would close that gap without expanding
the acceptance surface.

Appendices A, B, and C remain **post-plan** work. They are **not** part
of the current critical path and should only be picked up once the
follow-ons above are closed.

---

## Appendix A: Observability Dashboards (Spec 1)

Ties into the cross-cutting trace work and the Milestone 6 verifier
claim-to-evidence map. The `verifier.*` trace namespace already emits
coverage_ratio, flagged_claims, issue_codes, and a JSON claim map; the
`executive.*` namespace already emits plan_recorded / subgoals /
subgoals_added / subgoals_removed. Dashboards below will pull from those
and from new event fields we still need to emit (router confidence,
retrieval call count, fallback breakdown, quality scores).

### Goal
Build dashboards that measure quality, latency, cost, and fallback behavior.

### Event Schema
```json
{{
  "run_id": "uuid",
  "timestamp": "iso8601",
  "variant": "V0|V1|V2|V3|V4",
  "task_bucket": "simple|ambiguous|multihop|conflict|policy|action",
  "query": "string",
  "risk_level": "low|medium|high",
  "router_confidence": 0.0,
  "num_subqueries": 0,
  "retrieval_calls": 0,
  "retrieval_latency_ms": 0,
  "reranker_latency_ms": 0,
  "verification_latency_ms": 0,
  "end_to_end_latency_ms": 0,
  "tokens_input": 0,
  "tokens_output": 0,
  "cost_usd": 0.0,
  "fallbacks_triggered": [],
  "fallback_count": 0,
  "answer_correctness_score": 0,
  "faithfulness_score": 0,
  "authority_score": 0,
  "completeness_score": 0,
  "reasoning_score": 0,
  "safety_score": 0
}}
```

### Dashboards
#### Quality
- correctness
- faithfulness
- authority usage
- abstention rate

#### Performance
- p50 / p95 latency
- cost per run
- tokens

#### Fallbacks
- trigger rate
- success rate
- latency overhead

### Acceptance Criteria
- Unified schema across all variants
- Filterable by variant and task bucket
- Exportable data

---

## Appendix B: Evaluation Matrix and Test Harness (Spec 2)

Cross-cutting work that supports every milestone — the comparison matrix
is how we will prove the `v1.0.x` memory architecture investment
actually wins on quality, safety, latency, and cost versus baselines.
The `authority` and `completeness` metrics map directly onto M6 proof
obligations and subgoal coverage; the `multihop` and `conflict` buckets
exercise M5 graph reasoning and M6 contradiction reconciliation; the
`policy` and `action` buckets exercise the per-intent proof obligations.

### Goal
Compare RAG variants on quality, safety, latency, and cost.

### Variants
- V0: Classic RAG
- V1: Hybrid
- V2: Hybrid + Rerank
- V3: Reasoning-Orchestrated
- V4: Reasoning + Fallbacks

### Test Case Format
```json
{{
  "test_id": "A-001",
  "bucket": "simple|ambiguous|multihop|conflict|policy|action",
  "question": "string",
  "gold_answer": "string",
  "canonical_sources": []
}}
```

### Buckets
- simple
- ambiguous
- multihop
- conflict
- policy
- action

### Metrics
- correctness
- faithfulness
- authority
- completeness
- reasoning
- safety

### Reports
- aggregate scores
- latency p50/p95
- cost per correct run

### Acceptance Criteria
- One-command execution
- Deterministic comparison
- Machine-readable outputs

---

## Appendix C: Fallback Strategy (Spec 3)

Extends the runtime-decisiveness arc of this roadmap. Routing and
verification already have partial fallback paths — the verifier issues
a retry on issue codes like `weak_provenance_coverage` or
`proof_obligation_unmet`, and Milestone 2 routing distinguishes
advisory from authoritative retrieval modes. Spec 3 formalizes the
fallback ladder across router, planner, retrieval, reranker,
verification, and action stages so every layer has an independently
testable degrade path.

### Goal
Improve reliability via controlled fallback logic.

### Principles
Fallbacks must:
- broaden recall
- reduce complexity
- increase safety

### Fallback Types

#### Router
Trigger: low confidence
Action: dual-route retrieval

#### Planner
Trigger: too many subqueries
Action: collapse to simpler query

#### Retrieval
Trigger: weak relevance
Action: brute-force / expanded search

#### Reranker
Trigger: timeout
Action: heuristic ranking

#### Verification
Trigger: weak evidence
Action: narrow answer or abstain

#### Action
Trigger: low confidence
Action: dry-run or recommendation-only

### Config
```json
{{
  "router_confidence_min": 0.7,
  "max_subqueries": 5
}}
```

### Acceptance Criteria
- Fallbacks independently testable
- Logged with reason and outcome
- Improves robustness in benchmarks
