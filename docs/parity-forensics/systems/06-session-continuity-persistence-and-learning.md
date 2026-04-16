# System 06: Session Continuity, Persistence, and Learning

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-16
- Related release: v1.0.6

## Why This System Matters

This system determines whether the agent keeps the right things across restarts,
whether it reflects on completed turns, and whether repeated patterns become
longer-lived knowledge. It is the memory-system equivalent of the request path:
if ownership drifts here, the agent may appear to "remember" while actually
preserving the wrong state or learning the wrong lessons.

It is also one of the most migration-sensitive families because the surface area
is large:

- working-memory shutdown/startup continuity
- executive-state growth
- context checkpoints
- reflection into episodic summaries
- consolidation/distillation into semantic and graph stores

## Scope

In scope:

- working-memory persistence on shutdown
- startup vetting / restoration of working memory
- checkpoint save/load behavior
- post-turn reflection into episodic summaries
- executive-state growth
- consolidation/distillation and promotion rules

Out of scope:

- raw request assembly
- routing/model selection
- install/update/service lifecycle

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Context checkpoints | `crates/roboticus-db/src/checkpoint.rs` |
| Session/governor cleanup around checkpoints | `crates/roboticus-agent/src/governor.rs` |
| Consolidation pipeline | `crates/roboticus-agent/src/consolidation.rs` |
| Retrieval metrics persistence into snapshots | `crates/roboticus-db/src/sessions.rs` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Working memory persistence/vetting | `internal/agent/memory/working_persistence.go:1-184` |
| Post-turn reflection | `internal/pipeline/post_turn.go:171-244` |
| Executive-state growth | `internal/pipeline/post_turn.go:246+` |
| Consolidation distillation | `internal/agent/memory/consolidation_distillation.go:1-320` |
| Checkpoint restore in request prep | Rust parity tracked in request system; Go runtime restore path audit still pending deeper trace |

## Live Go Path

Current observed state on 2026-04-16:

1. Working memory is persisted on shutdown by marking entries with
   `persisted_at`.
2. On startup, persisted entries are vetted by age, importance, and entry type,
   and surviving rows are restored to active working memory.
3. Post-turn logic reflects on the turn and stores an `episode_summary` in
   episodic memory.
4. Executive-state growth converts verified outcomes into active structured
   task-state entries.
5. Consolidation distills repeated high-quality episode patterns into semantic
   memory and `knowledge_facts`.

This family looks materially stronger than earlier in the migration, but it
still needs deeper parity classification around:

- checkpoint behavior
- reflection completeness and fidelity
- promotion thresholds and categories
- which differences are true improvements versus silent drift

## Artifact Boundary

The artifact boundaries for this system are:

- persisted `working_memory` rows before/after restart vetting
- episodic `episode_summary` rows written post-turn
- executive-state entries written after a turn
- semantic and `knowledge_facts` rows produced by consolidation

Parity is not satisfied unless those persisted artifacts match the intended
ownership and promotion rules.

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-06-001 | P1 | Working-memory persistence/vetting must remain treated as a core success, not an open gap | Rust baseline preserves active task-relevant state across continuity boundaries | Go has a real persisted/vetted working-memory path and should preserve it as an invariant | Improvement | Closed / retain as evidence | `internal/agent/memory/working_persistence.go:1-184` |
| SYS-06-002 | P1 | Reflection remains heuristic and may under-capture turn quality/timing | Rust continuity/learning path includes richer session/governor/checkpoint context | Go reflection stores structured `episode_summary`, but turn duration is still a TODO proxy and tool-event extraction is heuristic | Degradation | Open | `internal/pipeline/post_turn.go:205-227` |
| SYS-06-003 | P1 | Consolidation behavior must be classified, not assumed parity | Rust has an explicit consolidation pipeline | Go distillation now promotes recurring patterns into semantic memory and `knowledge_facts`, which may be improvement or drift depending on exact parity target | Improvement candidate | Open | `internal/agent/memory/consolidation_distillation.go:1-320`, Rust `consolidation.rs` |
| SYS-06-004 | P1 | Checkpoint ownership is split between a lightweight live save path and a separate repository abstraction | Rust has explicit checkpoint persistence and pruning APIs with clearer lifecycle ownership | Go saves periodic checkpoints through `maybeCheckpoint(...)` in `pipeline_gaps.go`, while a `CheckpointRepository` also exists with save/load/delete APIs; the live save path currently stores a truncated system-message summary and digest rather than a richer typed snapshot | Degradation seam | Open | `internal/pipeline/pipeline_gaps.go:357-406`, `internal/db/checkpoint_repo.go:10-54` |
| SYS-06-005 | P2 | Executive-state growth is stronger than earlier versions, but needs classification against Rust task-state ownership | Rust threads task state through planning/inference/guards | Go grows executive state post-turn from verification results; this looks like a real improvement, but needs explicit parity classification so it is protected rather than flattened away | Improvement candidate | Open | `internal/pipeline/post_turn.go:246+` |
| SYS-06-006 | P1 | Reflection remains heuristic and under-specified relative to the richer memory architecture now in place | Rust continuity/learning path ties more directly into checkpoint/session lifecycle | Go reflection stores structured `episode_summary`, but turn duration is still a TODO proxy and tool-event extraction is inferred from message adjacency patterns | Degradation | Open | `internal/pipeline/post_turn.go:171-227` |
| SYS-06-007 | P1 | Checkpoint repository abstraction is currently test-only and not the authoritative live lifecycle | Checkpoint persistence APIs should either own the live save/load/delete lifecycle or be explicitly demoted as helper/test scaffolding | Go's `CheckpointRepository` is exercised in tests, but the current tree has no live call sites for `SaveCheckpoint`, `LoadCheckpoint`, or `DeleteOld`; production checkpoint writes still go through `maybeCheckpoint(...)` directly | Degradation seam | Open | `internal/db/checkpoint_repo.go`, `internal/db/coverage_boost_test.go`, `internal/pipeline/pipeline_gaps.go:362-406` |
| SYS-06-008 | P2 | Tool-fact harvesting into executive-state assumptions is a novel extension that needs explicit protection | Baseline continuity systems preserve task-relevant state; memory growth should be deliberate rather than accidental | Go now extracts a narrow allowlist of referenced tool-derived facts and records them as executive assumptions post-turn; this is beyond simple Rust parity and should be classified as a deliberate synthesis of recall discipline plus working-memory continuity | Improvement candidate | Open | `internal/pipeline/post_turn.go:378-407` |

## Intentional Deviations

Possible likely improvements that still need explicit classification:

- restart vetting rules for working memory
- relational distillation into `knowledge_facts`
- executive-state growth from verifier output
- narrow allowlist harvesting of referenced tool facts into executive-state
  assumptions
- the combination of retrieval discipline with persisted/vetted working memory,
  which is part of the novel memory architecture and must not be simplified
  away in the name of Rust parity

None are accepted yet until the full Rust/Go comparison is completed.

## Remediation Notes

This system should be approached in two passes:

1. preserve and document already-good invariants
2. classify reflection/checkpoint/consolidation differences carefully before
   deciding whether they are degradations, improvements, or idiomatic shifts

Protected invariants for this system:

- shutdown/startup working-memory persistence with vetting is a core success
  and should be treated as non-negotiable
- executive-state entry kinds are part of the novel memory model, not audit
  noise
- post-turn growth of task state from verified evidence and referenced tool
  facts is part of the richer Go memory architecture and should be evaluated as
  synthesis, not dismissed as automatic drift
- consolidation into `knowledge_facts` is not automatically suspect just
  because Rust was simpler; the question is whether the combined design is
  stronger and still disciplined

## Downstream Systems Affected

- System 03: Memory retrieval, compaction, and injection
- System 04: Verification, guards, and post-processing
- System 09: Admin, dashboard, and observability surfaces

## Open Questions

- Where exactly is the full Go checkpoint save/load/prune lifecycle, and how
  close is it to Rust?
- Is `CheckpointRepository` intended to become the authoritative live path, or
  is it now effectively test/support scaffolding around a direct SQL path?
- Should post-turn reflection remain heuristic, or is there a more structured
  source of turn timing and tool outcomes available?
- Which consolidation behaviors are intentionally beyond parity and which are
  silent semantic changes?
- Should the lightweight `maybeCheckpoint(...)` path be considered a temporary
  compatibility save path, or is it intended to remain the authoritative live
  checkpoint implementation?
- Which parts of the new memory model are now protected architecture, meaning
  future parity work must integrate them rather than roll them back?

## Progress Log

- 2026-04-16: Initialized System 06 document.
- 2026-04-16: Recorded working-memory persistence/vetting as a real success
  that should not be reopened accidentally.
- 2026-04-16: Flagged reflection completeness, checkpoint parity, and
  consolidation classification as the main remaining audit seams.
- 2026-04-16: Deepened the checkpoint seam: Go has both a live lightweight
  checkpoint save path and a separate checkpoint repository abstraction, which
  creates split ownership that needs explicit classification.
- 2026-04-16: Marked the shutdown/startup continuity path and executive-state
  model as protected elements of the novel memory architecture, not optional
  parity debt.
- 2026-04-16: Confirmed that the dedicated `CheckpointRepository` is still
  test-only in the current tree, while the live post-turn path writes
  lightweight checkpoint rows directly.
- 2026-04-16: Added the newer tool-fact harvesting path to the audit so this
  recall-plus-working-memory synthesis is classified deliberately instead of
  being flattened away by future parity cleanup.
