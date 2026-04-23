# System 03: Memory Retrieval, Compaction, and Injection

## Status

- Owner: parity-forensics program
- Audit status: `validated`
- Last updated: 2026-04-19
- Related release: v1.0.6

## Why This System Matters

This system decides what past state the agent sees, how that state is filtered,
compacted, and formatted, and how much of it reaches the prompt as active
context versus on-demand recall handles.

It is one of the most migration-sensitive seams because subtle ownership drift
causes the exact class of failures we have already been chasing:

- the wrong memory survives under pressure
- a helper exists but the live path still uses a weaker fallback
- structured evidence is rendered down to text and later reparsed
- the pipeline and daemon disagree about which layer owns memory preparation

## Scope

In scope:

- retrieval strategy selection and subgoal decomposition
- memory-tier routing and evidence retrieval
- working-memory and ambient direct injection
- reranking and structured context assembly
- memory context and memory index population on the session
- typed evidence handoff to later stages
- compaction of retrieved memory before prompt assembly

Out of scope:

- tool-pruning behavior
- model routing itself
- long-term consolidation/promotion after the turn completes

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Retrieval output handed into request builder | `crates/roboticus-pipeline/src/core/context_builder.rs:96`, `314-325`, `517-527` |
| Memory text compaction | `crates/roboticus-agent/src/compaction.rs:78-154`, `293-348` |
| Context assembly with budget | `crates/roboticus-agent/src/context.rs:277-288` |
| Retrieval metrics propagation into inference context | `crates/roboticus-pipeline/src/context/inference.rs:282-373` |
| Memory index / recall pattern | `crates/roboticus-agent/src/tools/introspection.rs:403-465` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Pipeline memory stage | `internal/pipeline/pipeline_run_stages.go:437-500` |
| Session memory carriers | `internal/session/session.go:69-81` |
| Retriever orchestration | `internal/agent/memory/retrieval.go:148-314` |
| Structured context assembly | `internal/agent/memory/context_assembly.go:27-260` |
| Memory index builder / recall tools | `internal/agent/tools/memory_recall.go` |
| Hippocampus compact summary repository API | `internal/db/hippocampus_repo.go:207-260` |
| Retrieval parity tests | `internal/pipeline/retrieval_parity_test.go` |

## Live Go Path

Current observed state on 2026-04-16:

1. Pipeline Stage 8.5 is intended to be the single authority for memory
   preparation.
2. That stage now always populates a memory index and conditionally populates
   `MemoryContext` when retrieval strategy is not `none`.
3. The `Retriever` handles decomposition, routing, tier retrieval, reranking,
   and structured assembly.
4. The retriever now also emits a typed evidence artifact through an evidence
   sink attached to the request context.
5. `buildAgentContext` reads pipeline-prepared `MemoryContext` and
   `MemoryIndex` from the session rather than rebuilding them inline.

This system is in better shape than earlier iterations, but it still contains a
few critical parity questions:

- whether the live compaction behavior is equivalent to Rust
- whether structured evidence fully replaced rendered-text reparsing
- whether all memory ownership really flows through the pipeline now

## Artifact Boundary

The artifact boundaries for this system are:

- session `MemoryContext`
- session `MemoryIndex`
- typed verification evidence attached to the session
- retrieval metrics and tier counts used by downstream stages

Parity is not satisfied unless these artifacts are produced once on the live
path and consumed without rebuilding weaker alternatives elsewhere.

## Success Criteria

- Closure artifact(s):
  - session `MemoryContext`
  - session `MemoryIndex`
  - session `VerificationEvidence`
  - retrieval metrics attached to the live turn
- Live-path proof:
  - runtime-facing tests prove Stage 8.5 is the only production owner of these
    artifacts
  - downstream consumers are shown to use typed evidence when present
  - final request-path tests prove memory survives compaction by value, not by
    naive truncation
- Blocking conditions:
  - any live downstream consumer still requires rendered `MemoryContext`
    section parsing when typed evidence is available
  - any fallback rebuild path reappears outside the pipeline
  - memory compaction only exists in helpers while the request path still uses
    raw truncation
- Accepted deviations:
  - richer recall/search behavior may remain if its ranking and budget effects
    are explicitly classified and documented

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-03-001 | P1 | Memory compaction parity still incomplete on live request path | Rust compacts memory text before context assembly with dedup, formatting compression, and budgeted retention | The live request path now compacts injected memory through the Rust-inspired compaction port instead of naive truncation; the remaining retrieval-quality work is fusion/reranking/read-path cleanup, not this compaction seam | Degradation remediated | Closed | Rust: `compaction.rs:78-154`, `293-348`; Go: `internal/agent/context.go`, `internal/agent/memory/compaction.go`, `internal/agent/memory/retrieval.go` |
| SYS-03-002 | P1 | Structured evidence still had a rendered-text fallback downstream | Rust pipeline carries structured prepared context forward | Go now emits typed evidence, and the verifier consumes typed artifacts only. Compatibility callers that set only `MemoryContext` are normalized at the session boundary into `VerificationEvidence`, so downstream stages no longer parse rendered memory text directly | Improved | Closed, retain as invariant | `internal/agent/memory/context_assembly.go:162-225`, `internal/session/verification_evidence.go`, `internal/pipeline/verifier.go` |
| SYS-03-003 | P1 | Pipeline single-authority claim must be continuously re-proven | Rust request builder consumes prepared retrieval output in one path | Go comments and tests say Stage 8.5 is sole authority; must keep proving no fallback rebuild path reappears | Improvement | Closed for current tree, retain as invariant | `internal/pipeline/pipeline_run_stages.go:442-460`, `internal/daemon/daemon_adapters.go:174-212`, `internal/pipeline/retrieval_parity_test.go` |
| SYS-03-004 | P2 | Hippocampus summary is split between retrieval-adjacent repo API and prompt assembly ownership | Rust injects compact summary in context builder | Go has repository support and related introspection use, but live prompt-injection ownership still belongs to System 01 remediation | Missing Functionality | Tracked in System 01 | `internal/db/hippocampus_repo.go:207-260` plus absence from current committed request path |
| SYS-03-005 | P2 | `search_memories` exists in Go but not in the Rust baseline | Rust exposes memory index plus `recall_memory(id)`; no search-by-topic companion tool is present in the audited baseline | Go adds `search_memories(query)` with FTS + fallback search to recover topic-based memories not surfaced in the injected index | Improvement | Classified, retain | Go: `internal/agent/tools/memory_recall.go:158-260`; Rust: `crates/roboticus-agent/src/tools/introspection.rs:403-465` |
| SYS-03-006 | P2 | `recall_memory` lookup semantics diverged from Rust | Rust `recall_memory` resolves through the memory index and recalls indexed content by source tier | Go accepts optional `source_table`, falls back to scanning source tables directly if the index misses, and reinforces index confidence on successful recall | Idiomatic shift leaning improvement | Classified, retain | Go: `internal/agent/tools/memory_recall.go:44-156`; Rust: `crates/roboticus-agent/src/tools/introspection.rs:403-465` |
| SYS-03-007 | P1 | Typed evidence replaced most rendered-text reparsing, but compatibility callers still need a bridge | Rust carries prepared context forward in one path | Go moved the format-sensitive parse to the session boundary: `SetMemoryContext` derives a compatibility `VerificationEvidence` artifact when no explicit typed artifact exists, and `BuildVerificationContext` consumes typed evidence only. The remaining seam is compatibility normalization ownership, not verifier/parser coupling | Idiomatic shift | Closed, retain | `internal/session/verification_evidence.go`, `internal/session/session.go`, `internal/pipeline/verifier.go` |

## Intentional Deviations

Classified improvements / shifts already visible:

- `search_memories` is a real beyond-parity capability, not a missing-Rust
  regression.
- `recall_memory` is more forgiving than Rust's index-only recall path because
  it can recover directly from source tables and optionally accept a known
  `source_table`.
- confidence reinforcement on successful recall is another intentional Go
  deviation; it should be treated as retrieval-behavior drift only if it causes
  downstream ranking pathologies.

Still not fully accepted as a clean improvement:

- session-boundary derivation of typed verification evidence from rendered
  memory context for compatibility callers. This is acceptable only because the
  verifier and other downstream consumers now stay on typed artifacts.

## Remediation Notes

This system is partially blocked on System 01 because the final request still
applies a weaker memory-cap path after retrieval/assembly.

Acceptance bar for closure:

- memory compaction parity is proven on the live request artifact
- no downstream stage needs to parse `MemoryContext` text if typed evidence is
  available
- Stage 8.5 remains the only production owner for session memory artifacts
- memory index / recall behavior is line-by-line classified against Rust
- deliberate beyond-parity features (`search_memories`, richer recall lookup)
  are documented so they are not reopened as false-positive parity debt

## Downstream Systems Affected

- System 01: Request construction and context assembly
- System 04: Verification, guards, and post-processing
- System 06: Session continuity, persistence, and learning

## Final Disposition

System 03 is closed for v1.0.6.

- Stage 8.5 is the production owner for memory context, memory index, and
  typed verification evidence.
- The verifier no longer parses rendered memory text on the live path.
- The only remaining format-sensitive logic is the session-boundary
  compatibility normalization for older callers that still set only
  `MemoryContext`. That is accepted as an intentional compatibility layer, not
  a downstream degradation.
- `search_memories` and the richer `recall_memory` lookup remain accepted
  Go-native improvements.

## v1.0.7 Reopening

System 03 was reopened for the remaining retrieval-quality seam that still
mattered to parity and agent efficacy:

- `PAR-014` — semantic read-path cleanup (now closed)

`PAR-012` is now closed. The architecture rule is explicit and implemented:

- tier retrieval may compute raw relevance and provenance in parallel
- **fusion** is the first central stage allowed to combine route weight,
  provenance, freshness, authority, and corroboration into one retrieval
  quality score
- reranking follows fusion and owns narrowing / collapse protection, not the
  fusion semantics themselves
- LLM-based reranking, when enabled, is a second-stage semantic scorer owned by
  the retriever itself. It must consume fused evidence, emit explicit RCA
  counters, and degrade cleanly to deterministic score-based reranking when
  policy, provider health, or structured output quality does not justify the
  extra pass
- semantic retrieval is no longer allowed to depend on a residual `LIKE`
  safety net just to recover semantic `key` / `category` lookups. The clean
  fix is to make the semantic FTS corpus rich enough to preserve that behavior
  on the primary semantic read path

That distinction matters for both RCA and future ML work. Fusion needs a
single owned stage so operators can understand why evidence surfaced and later
training data can observe the same decision boundary.

## Progress Log

- 2026-04-16: Initialized System 03 document.
- 2026-04-16: Recorded that Stage 8.5 memory ownership is currently much
  healthier than earlier iterations and should be preserved as an invariant.
- 2026-04-16: Flagged the remaining key seam: structured evidence exists, but
  some downstream logic still falls back to reparsing rendered memory text.
- 2026-04-16: Classified `search_memories` as a likely beyond-parity
  improvement rather than a regression.
- 2026-04-16: Classified Go's richer `recall_memory` lookup semantics as an
  idiomatic shift leaning improvement, while keeping the typed-evidence
  fallback seam as the main remaining degradation in this system.
- 2026-04-16: v1.0.6 P1 closed the SYS-03-001 compaction seam on the
  live request path. `internal/agent/memory/compaction.go` ports Rust's
  `compaction.rs` with both the structured `Compact` entry point and
  the text-level `CompactText`; `internal/agent/context.go` now calls
  `memory.CompactText(cb.memory, memCap)` at the memory-injection site
  instead of the old `cb.memory[:maxChars] + "[truncated]"` char cut.
  Token estimation continues to use Go's script-aware
  `llm.EstimateTokens` — recorded as Idiomatic Shift in the port's
  header comment, so the audit re-pass should verify no downstream
  consumer depended on Rust's naïve `len/4` estimator.
  SYS-03-002 / SYS-03-007 (typed-evidence fallback) remain open; the
  P1 commit does not touch the verifier code path.
- 2026-04-17: Closed the live downstream typed-evidence seam. The verifier no
  longer reparses rendered `MemoryContext` sections at all; compatibility
  callers that only set text now derive a `VerificationEvidence` artifact at
  `Session.SetMemoryContext(...)`. This keeps format-sensitive parsing out of
  downstream consumers while preserving backward compatibility for tests and
  ad-hoc harnesses.
- 2026-04-20: Reopened `PAR-012` as an architecture-first retrieval closure.
  The current pipeline still routes and retrieves in parallel, but fusion is
  mostly implicit across router weights and reranker heuristics. v1.0.7 now
  treats fusion as its own stage between routed tier retrieval and reranking.
- 2026-04-20: Closed `PAR-012`. `internal/agent/memory/fusion.go` now owns the
  route-weight / provenance / freshness / authority / corroboration merge,
  retrieval traces record fusion-stage counters, and the full memory package
  stayed green after the change.
- 2026-04-20: Reopened `PAR-013` as an architecture-first ranking closure. The
  live path now has an explicit fusion stage, so any LLM-based reranking must
  run after fusion, remain optional and centrally configured, and preserve the
  existing deterministic reranker as the fallback rather than replacing it.
- 2026-04-20: Closed `PAR-013`. The retriever now owns an optional LLM-backed
  reranking stage after fusion and before context assembly. It uses the shared
  LLM service with `NoEscalate`, emits explicit `retrieval.rerank.llm.*`
  annotations for RCA/ML work, and falls back cleanly to deterministic
  score-based reranking on disablement, parse failure, or provider failure.
- 2026-04-20: Reopened `PAR-014` as a semantic-tier cleanup, not a blind
  fallback deletion. The remaining issue is that semantic retrieval still uses
  a tier-local `LIKE` safety net to recover `key` lookups because the semantic
  FTS corpus only indexes `value`. v1.0.7 must enrich the semantic FTS corpus
  so `key` / `category` retrieval survives without heuristic SQL.
