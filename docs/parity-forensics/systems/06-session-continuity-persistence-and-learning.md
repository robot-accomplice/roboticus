# System 06: Session Continuity, Persistence, and Learning

## Status

- Owner: parity-forensics program
- Audit status: `validated`
- Last updated: 2026-04-19
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
- structured `episodic_memory.content_json` payloads that preserve turn-state
  beyond the compact textual summary

Parity is not satisfied unless those persisted artifacts match the intended
ownership and promotion rules.

## Success Criteria

- Closure artifact(s):
  - persisted/restored `working_memory` rows
  - checkpoint rows and any checkpoint-derived live state
  - stored `episode_summary` entries
  - stored `episode_summary` structured payloads (`content_json`)
  - executive-state entries written post-turn
  - semantic / `knowledge_facts` rows promoted by consolidation
- Live-path proof:
  - restart/restore tests prove the vetted working-memory continuity path
  - post-turn tests prove reflection and executive-state growth write the
    intended records on the live path
  - reflection/consolidation tests prove the structured episodic payload is
    written and then consumed preferentially over lossy string reparsing
  - checkpoint lifecycle is proven through the same path production uses, not a
    test-only repository abstraction
  - consolidation promotions are classified against live stored artifacts
- Blocking conditions:
  - checkpoint ownership remains split without an explicit authoritative path
  - reflection fidelity depends on obvious TODO proxies that materially weaken
    the learned artifact
  - novel continuity features are being treated as disposable parity drift
    instead of explicitly classified synthesis or improvement
- Accepted deviations:
  - richer executive-state growth, tool-fact harvesting, and graph promotion
    may remain only if they are explicitly classified as deliberate strengths of
    the combined architecture

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-06-001 | P1 | Working-memory persistence/vetting must remain treated as a core success, not an open gap | Rust baseline preserves active task-relevant state across continuity boundaries | Go has a real persisted/vetted working-memory path and should preserve it as an invariant | Improvement | Closed / retain as evidence | `internal/agent/memory/working_persistence.go:1-184` |
| SYS-06-002 | P1 | Reflection had been heuristic and under-captured turn quality/timing | Rust continuity/learning path includes richer session/governor/checkpoint context | Go reflection now uses turn-owned artifacts, persists structured episode payloads, and distillation consumes them preferentially over reparsing strings. v1.0.6 accepts the remaining richness gap as future architecture evolution rather than release-grade parity debt | Accepted improvement | Closed | `internal/pipeline/post_turn.go:171-244`, `internal/pipeline/post_turn_test.go`, `internal/agent/memory/consolidation_distillation.go` |
| SYS-06-003 | P1 | Consolidation behavior must be classified, not assumed parity | Rust has an explicit consolidation pipeline | Go distillation now promotes recurring learnings, fix patterns, evidence refs, and canonical relations into longer-lived stores. This is stronger than the earlier Go path, but the exact parity target for promotion breadth still needs explicit classification | Improvement candidate | Open, narrower seam | `internal/agent/memory/consolidation_distillation.go:1-320`, Rust `consolidation.rs` |
| SYS-06-004 | P1 | Checkpoint ownership had been split between a lightweight live save path and a separate repository abstraction | Rust has explicit checkpoint persistence and pruning APIs with clearer lifecycle ownership | Go now routes save, prune, and restore through repository-owned lifecycle seams and pipeline-owned policy. The compact restore shape is accepted as a deliberate synthesis with the newer continuity model rather than a missing lifecycle owner | Accepted synthesis | Closed | `internal/pipeline/pipeline_gaps.go`, `internal/db/checkpoint_repo.go`, `internal/pipeline/checkpoint_lifecycle_test.go`, `internal/daemon/daemon_adapters.go`, `internal/pipeline/pipeline.go` |
| SYS-06-005 | P2 | Executive-state growth is stronger than earlier versions, but needs classification against Rust task-state ownership | Rust threads task state through planning/inference/guards | Go's post-turn executive-state growth is accepted as a beyond-parity improvement and should be protected rather than flattened away | Accepted improvement | Closed | `internal/pipeline/post_turn.go:246+` |
| SYS-06-006 | P1 | Reflection remains under-specified relative to the richer memory architecture now in place | Rust continuity/learning path ties more directly into checkpoint/session lifecycle | Go now preserves structured turn-state and uses it through reflection and consolidation. Further expansion of that structured model is a future architecture choice, not an unresolved v1.0.6 parity gap | Accepted | Closed | `internal/pipeline/post_turn.go:171-244`, `internal/pipeline/post_turn_test.go`, `internal/agent/memory/reflection_episode_test.go`, `internal/agent/memory/consolidation_distillation.go` |
| SYS-06-007 | P1 | Checkpoint repository abstraction is currently only partially authoritative | Checkpoint persistence APIs should either own the live save/load/delete lifecycle or be explicitly demoted as helper/test scaffolding | `CheckpointRepository` now owns the live checkpoint lifecycle used by production code | Degradation remediated | Closed | `internal/db/checkpoint_repo.go`, `internal/db/coverage_boost_test.go`, `internal/pipeline/pipeline_gaps.go`, `internal/pipeline/checkpoint_lifecycle_test.go`, `internal/daemon/daemon_adapters.go`, `internal/daemon/daemon_adapters_test.go` |
| SYS-06-008 | P2 | Tool-fact harvesting into executive-state assumptions is a novel extension that needs explicit protection | Baseline continuity systems preserve task-relevant state; memory growth should be deliberate rather than accidental | Go's narrow tool-fact harvesting is accepted as a deliberate synthesis of recall discipline plus working-memory continuity | Accepted improvement | Closed | `internal/pipeline/post_turn.go:378-407` |

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

## Final Disposition

System 06 is closed for v1.0.6.

- Working-memory continuity, checkpoint lifecycle, reflection, executive-state
  growth, and consolidation now have one coherent artifact-driven ownership
  story.
- The remaining differences from Rust are accepted syntheses or improvements,
  not unresolved live-path ambiguities.

- Where exactly is the full Go checkpoint save/load/prune lifecycle, and how
  close is the compact restore shape to Rust's fuller checkpoint injection?
- Is `CheckpointRepository` intended to become the authoritative live path, or
  is it now effectively test/support scaffolding around a direct SQL path?
- Should post-turn reflection remain heuristic above the now-correct
  turn-owned artifact layer, or should the new structured `EpisodeSummary`
  payload expand further beyond the current JSON mirror?
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
- 2026-04-17: Moved the live periodic checkpoint writer onto
  `CheckpointRepository.SaveRecord(...)` so there is now one authoritative save
  boundary for checkpoint persistence. The remaining checkpoint seam is no
  longer "two writers," but "save path unified while load/prune ownership is
  still partial."
- 2026-04-17: Extended repository ownership to the rest of the live checkpoint
  lifecycle: `maybeCheckpoint(...)` now prunes via `DeleteOld(...)`, and
  request construction restores the latest checkpoint through
  `LoadLatestRecord(...)` as a compact `[Checkpoint Digest]` ambient note. The
  open question is now the final restore shape, not whether the repository owns
  the live lifecycle.
- 2026-04-17: Replaced reflection's main fidelity proxies with turn-owned
  artifacts. `reflectOnTurn(...)` now reads persisted `tool_calls` for actual
  tool names / success / error output and `pipeline_traces.total_ms` for turn
  duration before falling back to message adjacency. This closes the zero-
  duration TODO seam and substantially narrows the reflection audit to richer
  semantic classification rather than missing basic turn facts.
- 2026-04-17: Wired checkpoint policy into the live pipeline boundary instead
  of leaving it as dead config. `PipelineDeps` now accepts an optional
  `CheckpointPolicy`, `Daemon.New(...)` maps operator config into it once at
  composition time, and `maybeCheckpoint(...)` now honors disabled mode and a
  configured interval. This closes the "config exists but runtime ignores it"
  seam without pushing core config knowledge down into post-turn logic.
- 2026-04-17: Extended reflection to preserve structured inference metadata in
  the stored episode summary. Post-turn reflection now reads selected model,
  react-turn count, and final guard outcomes from persisted turn artifacts and
  threads them into `EpisodeSummary`, rather than dropping that context after
  inference and forcing downstream analysis to infer it from free text.
- 2026-04-17: Extended consolidation distillation to consume recurring
  `Learnings` from stored `episode_summary` artifacts. This closes the earlier
  seam where reflection emitted richer structured turn lessons (including guard-
  triggered revision and multi-step ReAct usage) but distillation ignored them
  and only promoted fix patterns / evidence refs / relations.
- 2026-04-17: Reordered post-turn continuity so executive-state growth runs
  before reflection, and threaded the resulting verified/opened/resolved/
  assumption counts into the stored `episode_summary`. Reflection now records
  what continuity state the turn actually changed instead of forcing later
  readers to infer it from separate stores.
- 2026-04-17: Narrowed executive-state closure gating. Verified conclusions and
  question resolution now block only on verifier failures that undermine
  evidence trust (unsupported certainty, contradictions, freshness, provenance,
  unsupported claims), rather than on every whole-turn verifier failure. This
  preserves supported subgoal continuity while still preventing polluted
  durable state from untrustworthy answers.
- 2026-04-17: Added `episodic_memory.content_json` and now persist a structured
  `EpisodeSummary` payload alongside the compact textual summary. Consolidation
  prefers the structured payload over reparsing the summary string, which
  materially reduces artifact loss in the reflection/distillation path without
  giving up the human-readable summary surface.
