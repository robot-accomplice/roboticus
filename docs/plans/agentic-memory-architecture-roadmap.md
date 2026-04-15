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
| 1 | Unify The Production Retrieval Path | In progress | Pipeline-prepared memory/index now preferred by runtime context assembly |
| 2 | Make Intent And Retrieval Routing Real Decision Inputs | In progress | Intent signals now reach production retrieval; router modes now affect tier behavior |
| 3 | Upgrade Semantic Memory Into A Canonical Knowledge Layer | In progress | Semantic provenance, canonical flags, authority scoring, and freshness cues now survive retrieval/assembly |
| 4 | Turn Procedural Memory Into Workflow Memory | Not started | Still too stats-heavy; no true workflow records yet |
| 5 | Replace Relationship Memory With Persisted Relational Memory | In progress | Persisted `knowledge_facts` store, graph-aware retrieval, and first traversal semantics now shipped |
| 6 | Add A Real Verifier / Critic Stage | Acceptance met | Claim-level certainty classification, provenance coverage, contradiction reconciliation, per-intent proof obligations, and a structured claim-to-evidence trace map are all in place; semantic-classifier upgrade remains as quality work |
| 7 | Deepen Working Memory Into Executive State | Acceptance met | Executive state is persisted, surfaced in context assembly, grows automatically in post-turn, survives restart with a cross-turn regression test, and emits operator-auditable trace/log writes; richer tool-output assumption extraction remains as follow-on work |
| 8 | Improve Reflection And Consolidation Quality | Not started | Reflection/consolidation still heuristic despite working scaffolding |

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
- Milestone 5 now ships a reusable `KnowledgeGraph` API
  (`internal/agent/memory/graph.go`) with multi-hop `ShortestPath`,
  `Impact`, and `Dependencies` traversals over persisted `knowledge_facts`.

### Current Critical Path

1. Milestone 6 acceptance criteria are met. Remaining quality work is a
   semantic claim classifier (LLM or embedding backed) so certainty and
   provenance survive paraphrases rather than relying only on lexical
   markers.
2. Milestone 7 acceptance criteria are met. Remaining quality work is
   extracting assumptions from tool outputs (not just the agent's own
   response) and upgrading assumption confidence scoring based on evidence.
3. Finish Milestone 5 by moving from first traversal semantics to richer
   persisted adjacency/path reasoning and multi-hop impact analysis.
4. Start Milestone 4 (turn procedural memory into workflow memory) so the
   reusable-workflow gap is closed before Milestone 8 attempts to promote
   episodic patterns into procedures.

---

## Current Assessment

### Strong Today

- Tool execution guardrails and policy enforcement
- Episodic memory storage and hybrid retrieval
- Working memory persistence and startup vetting
- Reflection and consolidation scaffolding
- Pipeline traces and guard-chain observability

### Partial Today

- Intent/perception exists, but not as a unified "intent + risk + source of
  truth + required memory types" decision artifact
- Planner/decomposer exists, but is still heuristic and shallow
- Router exists, but retrieval modes are not fully honored by the runtime
- Context assembly exists, but evidence structure is still fairly thin
- Verification exists mostly as output guards, not as a real evidence critic

### Weak Today

- Semantic memory as a canonical fact/policy system
- Procedural memory as reusable workflows/SOPs rather than mostly tool stats
- Relational memory as a persisted dependency/causality graph
- End-to-end use of provenance, authority, recency, and contradiction signals

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

**Status**: In progress

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
- Full retrieval parity between all runtime paths still needs to be proven more
  explicitly in traces and stream/non-stream comparisons.

---

## Milestone 2: Make Intent And Retrieval Routing Real Decision Inputs

**Status**: In progress

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
- Risk level and explicit source-of-truth classification are still not emitted
  as a unified perception artifact.

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
- Semantic retrieval still needs a stronger source model around versioning,
  effective dates, and supersession, and it still falls back to SQL matching
  more often than the final architecture should allow.

---

## Milestone 4: Turn Procedural Memory Into Workflow Memory

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

---

## Milestone 5: Replace Relationship Memory With Persisted Relational Memory

**Status**: In progress

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
- What still remains is migrating the retrieval tier to consume the new
  API (currently it keeps a private walker), and surfacing multi-hop graph
  queries via an agent tool so the model can call them directly.

---

## Milestone 6: Add A Real Verifier / Critic Stage

**Status**: Acceptance met (quality follow-on open)

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
- The remaining gap is semantic depth: claim classification is still lexical,
  so paraphrase-heavy responses can still slip through the certainty and
  support checks.

---

## Milestone 7: Deepen Working Memory Into Executive State

**Status**: Acceptance met (tool-output assumption extraction open)

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
- What still remains is richer assumption extraction from tool outputs — the
  current extractor only picks up assumptions the agent names explicitly.

---

## Milestone 8: Improve Reflection And Consolidation Quality

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

Milestones 6 and 7 acceptance criteria are now met. Shift the critical
path to Milestones 5 and 4:

- **Milestone 5 (relational memory)** — finish persisted adjacency/path
  reasoning by promoting the current retrieval-time graph traversal into a
  first-class persisted graph API so multi-hop impact analysis and
  dependency-chain queries do not rebuild on every retrieval.
- **Milestone 4 (workflow memory)** — introduce structured workflow records
  (name, steps, preconditions, failure modes, context tags, version,
  success/failure evidence) so procedural retrieval returns reusable
  workflows instead of tool statistics.
- **Milestone 6 quality follow-on** — replace the lexical claim extractor
  with an embedding-backed semantic classifier so paraphrased absolute
  claims are still flagged by the verifier.
- **Milestone 7 quality follow-on** — extract assumptions from tool outputs
  (not only the agent's own response) and upgrade assumption confidence
  based on evidence support.
