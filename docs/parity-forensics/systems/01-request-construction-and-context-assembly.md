# System 01: Request Construction and Context Assembly

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-16
- Related release: v1.0.6

## Why This System Matters

This is the narrowest high-leverage system in the migration. It is where tool
exposure, memory injection, compaction, hippocampus summary, prompt
compression, and routing-relevant request shaping all converge into the final
request artifact sent to inference.

If this system drifts from Rust, the outward symptoms show up everywhere else:

- routing feels wrong
- verifier context is weaker than expected
- tool loops bloat or degrade
- memory quality drops under token pressure
- warm-up and baseline behavior become noisy

## Scope

In scope:

- final `llm.Request` assembly for agent turns
- system/user/history message shaping
- tool list injection/pruning
- memory block assembly and compaction on the live path
- hippocampus summary injection
- prompt compression entrypoint selection
- routing-relevant trace annotations derived from the assembled request

Out of scope for this system:

- low-level retrieval scoring internals
- post-response ingestion/consolidation
- tool execution semantics after the request is issued
- install/update/service lifecycle

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Tool pruning before request assembly | `crates/roboticus-pipeline/src/core/context_builder.rs:242-250` |
| Memory compaction before context assembly | `crates/roboticus-pipeline/src/core/context_builder.rs:311-325`, `crates/roboticus-agent/src/compaction.rs:78-154`, `crates/roboticus-agent/src/compaction.rs:293-348` |
| Hippocampus summary injection | `crates/roboticus-pipeline/src/core/context_builder.rs:356-369`, `crates/roboticus-db/src/hippocampus.rs` |
| Prompt compression gate | `crates/roboticus-pipeline/src/core/context_builder.rs:436-445`, `crates/roboticus-agent/src/context.rs:596-614` |
| Tool-search trace contract | `crates/roboticus-pipeline/src/trace_helpers.rs:57-104` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Context builder | `internal/agent/context.go:108-377` |
| Daemon request prep | `internal/daemon/daemon_adapters.go:91-185` |
| Tool registry | `internal/agent/tools/registry.go:12-139` |
| Newer tool search implementation | `internal/agent/tools/tool_search.go` |
| Older duplicate tool search implementation | `internal/agent/tool_search.go` |
| Prompt compressor | `internal/llm/compression.go:18-67` |
| Topic compression | `internal/llm/topic_compression.go:10-195` |
| Routing trace annotation | `internal/pipeline/pipeline_run_stages.go:683-716` |
| Runtime router selection | `internal/llm/service.go:214-218`, `internal/llm/service.go:422-425` |
| Hippocampus compact summary | `internal/db/hippocampus_repo.go:209-260` |

## Live Go Path

Current live path, as observed on 2026-04-16:

1. Daemon context preparation builds an `agent.ContextBuilder` in
   `internal/daemon/daemon_adapters.go`.
2. The builder is populated with system prompt, memory context, and tools.
3. The committed live code path still injects `tools.ToolDefs()` directly into
   the builder, which means the final request can still receive the full tool
   set even though pruning implementations exist elsewhere. A remediation is
   currently in progress in the worktree, but parity evidence must be based on
   the live owned path, not the presence of replacement code.
4. `ContextBuilder.BuildRequest` in `internal/agent/context.go` assembles the
   final messages and returns the request object used by inference.
5. Routing telemetry in the pipeline separately annotates a routing winner, but
   the current trace uses a synthetic user-only request rather than the same
   full request shape used by the runtime router.

This means there are still multiple plausible owners for request-shaping
behavior, and at least one of them is known to be bypassing the intended parity
implementation.

## Artifact Boundary

The parity artifact for this system is the final `llm.Request` actually passed
to model selection / inference.

Parity for this system is not satisfied unless tests can assert:

- which messages are present
- which tools are present
- which memory content survived compaction
- whether hippocampus summary is present
- whether compression gates altered the live request
- whether routing annotations reflect the same effective inputs

## Success Criteria

- Closure artifact(s):
  - the final live `llm.Request`
  - the final routing trace / audit artifact derived from that same request
- Live-path proof:
  - runtime-facing tests or traces assert the exact `llm.Request.Messages` and
    `llm.Request.Tools` used for inference
  - those tests prove the selected tool set is pruned, memory is compacted,
    empty compacted messages are absent, and any hippocampus/checkpoint/prompt
    compression injections are present or absent intentionally
  - if prompt compression is enabled on the live path, paired off-vs-on soak
    evidence shows no pass→fail regression on the managed isolated scenario set
  - routing annotations are generated from the same effective request shape
    used by runtime model selection
- Blocking conditions:
  - any path still bulk-injects tools or bypasses the authoritative request
    builder
  - multiple plausible compression/pruning owners remain live-capable
  - checkpoint restore / hippocampus summary / prompt compression remain
    "implemented nearby" but not proven on the final request artifact
- Accepted deviations:
  - any retained prompt-layer differences from Rust must be explicitly
    justified as Go-only or synthesis behavior and tied back to the final
    request artifact
  - prompt compression may intentionally apply to a narrower surface than
    Rust if that is required to preserve Go's richer memory/system-context
    fidelity; such narrowing must be documented and covered by request-artifact
    tests plus paired live-soak evidence

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-01-001 | P0 | Tool pruning bypassed on live path | Rust prunes tool definitions before request assembly in `context_builder.rs:242-250` | Committed live path injects `tools.ToolDefs()` wholesale via `buildAgentContext` | Missing Functionality | Active remediation | `internal/daemon/daemon_adapters.go:140-158`, `internal/agent/tools/registry.go:69-85` |
| SYS-01-002 | P1 | Duplicate tool-search implementations | One canonical pruning path (`tool_search.rs`) | Two plausible Go implementations with different defaults/ownership exist | Degradation | Open | `internal/agent/tool_search.go`, `internal/agent/tools/tool_search.go`; worktree currently deleting the older one |
| SYS-01-003 | P1 | Memory compaction downgraded to naive truncation | Rust compacts/dedups/scores by value before context assembly | Live path still truncates memory text by character count with a sentinel | Degradation | Active remediation | Rust: `context_builder.rs:311-325`, `compaction.rs:78-154`; Go: `internal/agent/context.go:141-158` |
| SYS-01-004 | P1 | Hippocampus summary not injected on live path | Rust injects compact summary as a system message in `context_builder.rs:356-369` | `CompactSummary()` exists, but no live context-builder call site was found | Missing Functionality | Active remediation | `internal/db/hippocampus_repo.go:209-260`; no call from `internal/agent/context.go` or `internal/daemon/daemon_adapters.go` |
| SYS-01-005 | P2 | Prompt compression gate not live | Rust reads config gate and compresses assembled messages in `context_builder.rs:436-445` | Config fields exist, but current live path does not read them in committed code | Missing Functionality | Active remediation | `internal/core/config.go:260-261`, `internal/core/config_defaults.go:83`, `internal/agent/context.go`, `internal/llm/compression.go` |
| SYS-01-006 | P1 | Routing telemetry does not reflect actual routing inputs | Routing/debug story should match the real assembled request | Pipeline trace uses a synthetic last-user-only request while runtime router selects on full request | Degradation | Open | `internal/pipeline/pipeline_run_stages.go:693-708`, `internal/llm/service.go:214-218`, `internal/llm/service.go:422-425` |
| SYS-01-007 | P2 | Compression ownership split across subsystems | One authoritative context-compression path | ContextBuilder compaction, `PromptCompressor`, and topic compression coexist as separate plausible owners | Degradation | Open | `internal/agent/context.go:179-217`, `internal/agent/context.go:345-377`, `internal/llm/compression.go:18-67`, `internal/llm/topic_compression.go:10-195` |
| SYS-01-008 | P1 | Empty compacted messages could escape the request builder | Dropped content should disappear from the final request | Go now drops history messages whose compacted content is empty and which do not carry tool payloads. `llm.Service` still defensively scrubs empties at the service boundary, but empty conversational messages no longer originate from `ContextBuilder.BuildRequest` | Improved | Closed 2026-04-17 | `internal/agent/context.go`, `internal/agent/context_user_message_invariant_test.go::TestBuildRequest_DropsEmptyCompactedHistoryMessages`, `internal/llm/service.go` |
| SYS-01-009 | P2 | Rust request assembly restores context checkpoints directly into the live request; Go currently uses a compact checkpoint digest restore instead of the full checkpoint blob | Rust loads the latest checkpoint and injects it as a system message during request construction | Go now loads the latest checkpoint through `CheckpointRepository.LoadLatestRecord(...)` and injects a compact `[Checkpoint Digest]` ambient note during `buildAgentContext(...)`; this closes the missing live restore seam but remains a deliberate synthesis rather than verbatim Rust replay | Synthesis / improvement candidate | Improved, not closed | Rust: `context_builder.rs:387-410`; Go: `internal/daemon/daemon_adapters.go`, `internal/db/checkpoint_repo.go`, `internal/daemon/daemon_adapters_test.go` |
| SYS-01-010 | P1 | Prompt-layer tool discoverability could drift from the structured tool list | Rust injects textual tool-use instructions derived from the pruned tool set into the system prompt, helping models without perfect native tool-calling priors | Go already had a textual tool roster block, but it was populated from the daemon boot's full registry instead of the selected per-request tool set. The live path now rewrites `PromptConfig.ToolNames` / `ToolDescs` from `selectedDefs` before building the prompt, including the authoritative zero-tools case, so the model sees one coherent tool surface across prompt and `llm.Request.Tools` | Improved | Closed 2026-04-18 | Rust: `context_builder.rs:214-269`; Go: `internal/daemon/daemon_adapters.go`, `internal/daemon/daemon_adapters_test.go::TestBuildAgentContext_PromptToolRosterUsesSelectedDefs`, `internal/daemon/daemon_adapters_test.go::TestBuildAgentContext_PromptToolRosterClearsWhenSelectedDefsEmpty` |
| SYS-01-011 | P2 | Prompt assembly tiering differs | Rust varies prompt blocks by complexity level (compact L0/L1 vs verbose L2/L3) within the same request builder | Go currently has one primary prompt-assembly path plus independent compaction rules, but no equivalent committed complexity-tiered prompt block assembly in `ContextBuilder` | Open | Open | Rust: `context_builder.rs:270-310`; Go: `internal/agent/context.go:98-343` |

## Intentional Deviations

Potential synthesis opportunities already visible:

- Go's stronger memory index / typed-evidence work should not erase Rust's
  disciplined checkpoint restore and prompt-gating behavior.
- Rust's tighter prompt-layer tool discoverability may still complement Go's
  richer structured tool surface.

Do not treat "Go is richer elsewhere" as a reason to skip these request-layer
comparisons.

## Remediation Notes

Work already known to be underway elsewhere:

- semantic tool pruning on the live request path
- memory compaction parity
- hippocampus summary injection
- prompt compression gate wiring

Additional request-layer questions now pinned:

- should checkpoint restore become part of the same final request artifact in Go
  the way it is in Rust, or is that deliberately being replaced by another
  continuity mechanism?
- should textual tool instructions remain a prompt-layer aid even when
  structured tool defs are present?
- should complexity-tiered prompt assembly be restored, replaced, or synthesized
  with Go's newer mechanisms?

This document should be updated after those changes land to distinguish:

- expected end state
- actual live ownership
- duplicate or dead paths that still remain after remediation

Worktree note as of 2026-04-16:

- active edits exist in `internal/daemon/daemon_adapters.go`,
  `internal/agent/tools/registry.go`, and new tool-pruning files under
  `internal/agent/tools/`
- the older `internal/agent/tool_search.go` path is currently staged for
  deletion

Those changes are not treated as closed until the resulting live request path
is re-audited.

## Downstream Systems Affected

- System 02: Tool exposure, pruning, and execution loop
- System 03: Memory retrieval, compaction, and injection
- System 04: Verification, guards, and post-processing
- System 05: Routing and model selection

## Open Questions

- Which current Go compression helper is intended to remain authoritative after
  parity remediation?
- Once tool pruning lands, will the older duplicate tool-search implementation
  be deleted or simply left unwired?
- Should routing telemetry be emitted from the final `llm.Request` boundary
  instead of the pipeline stage helper?
- Is checkpoint restore intentionally absent from the committed Go request path,
  or is it simply not yet reintegrated into the novel memory design?
- Does every prompt-layer tool hint now derive from the selected per-request
  tool surface, or do any callers still advertise full-registry capability?

## Progress Log

- 2026-04-16: Initialized System 01 document.
- 2026-04-16: Seeded live-path divergences already confirmed in the current Go
  tree, including active-remediation items and additional audit findings around
  routing telemetry, duplicate tool search, split compression ownership, and
  empty-message emission risk.
- 2026-04-16: Added deeper request-layer divergences around checkpoint restore,
  prompt-layer tool discoverability, and Rust's complexity-tiered prompt
  assembly so those seams do not get lost behind the more obvious pruning and
  compaction work.
- 2026-04-16: v1.0.6 P0 tool-pruning remediation landed (touches SYS-01-001
  and SYS-01-002 from this system; detailed in System 02 progress log).
  Summary relevant to System 01:
  - tool selection is no longer owned by `buildAgentContext`; the pipeline
    stage `stageToolPruning` writes the selected set onto the session before
    `stagePrepareInference` observes it, so the request assembled here is
    bounded and reproducible across turns
  - `buildAgentContext` now reads `sess.SelectedToolDefs()` primary with a
    defensive fallback that preserves behavior for non-pipeline callers
  - the older `internal/agent/tool_search.go` deletion is now part of a
    coherent remediation (previously uncommitted staged delete); the new
    authoritative owner is `internal/agent/tools/tool_search.go` invoked
    via `internal/agent/tools/prune.go::SelectToolDefs`
  - SYS-01-003 (compaction), SYS-01-004 (hippocampus summary), SYS-01-005
    (compression gate), SYS-01-006 (routing telemetry), SYS-01-007
    (compression ownership split), and SYS-01-008 (empty compacted
    messages) remain open and are the target of the next remediation
    passes after audit re-validation of SYS-01-001/002.
- 2026-04-17: Observed additional in-flight System 01 remediation in the
  worktree:
  - memory injection now compacts over-budget memory through
    `memory.CompactText(...)` instead of naive char truncation
  - prompt compression is wired onto the live `ContextBuilder.BuildRequest`
    path via `agent.CompressContextMessages(...)`
  - older `llm.PromptCompressor` / topic-compression wrapper owners are being
    deleted in favor of clearer ownership boundaries
  - `llm.Service.Complete` / `Stream` now scrub empty messages at the service
    boundary
  These are meaningful live-path improvements, but System 01 is still not
  closure-ready because the current evidence is mixed: hippocampus stage
  wiring and tool-pruning stage wiring have explicit tests, while prompt
  compression / empty-message handling still need stronger direct
  request-artifact proof before any status is upgraded.
- 2026-04-17: Added a dedicated paired compression soak harness at
  `scripts/run-prompt-compression-soak.py`. This does not close SYS-01-005 by
  itself; it defines the required quality gate for that item:
  prompt compression is only acceptable if the compression-enabled lane does
  not turn baseline-passing live scenarios into failures.
- 2026-04-17: Prompt compression was deliberately narrowed relative to the
  broader Rust-era behavior. The current Go owner compresses only older
  conversational history (`user` / `assistant`) and leaves system prompt,
  memory, memory index, hippocampus/system notes, and tool payload messages
  verbatim. This is classified as a synthesis improvement, not a parity miss:
  Go's system layer carries richer memory architecture and should not be fed
  through a lossy compressor just because Rust historically allowed it.
- 2026-04-16: v1.0.6 P1 memory-compaction + hippocampus-summary
  remediation landed (touches SYS-01-003 and SYS-01-004). Changes:
  - `internal/agent/memory/compaction.go` ports Rust's
    `crates/roboticus-agent/src/compaction.rs`: `Compact` for structured
    entries, `CompactText` for rendered text, preserving the Rust
    priority formula (0.4*relevance + 0.3*importance + 0.3*recency;
    recency has 1-hour half-life), the 0.8 dedup threshold over word
    trigrams, and the Rust section headers. Token estimation uses Go's
    script-aware `llm.EstimateTokens` (documented as Idiomatic Shift).
  - `internal/agent/context.go:141-158` naïve
    `cb.memory[:maxChars] + "[truncated]"` replaced with
    `memory.CompactText(cb.memory, memCap)`. Also guards against
    emitting an empty memory system message when the compacted block
    collapses to "" under a tight budget (analogue of SYS-01-008 for
    the memory injection site; the message-history analogue remains
    open).
  - `stageHippocampusSummary` (new pipeline stage) runs between
    `stageToolPruning` and `stagePrepareInference`. Calls
    `db.NewHippocampusRegistry(store).CompactSummary(ctx)`, writes the
    non-empty summary onto `session.Session.hippocampusSummary`,
    annotates the trace with `hippocampus.bytes`. Empty summaries are
    recorded with bytes=0 and the outcome stays `ok` so operators can
    see the stage ran and why the model didn't receive an ambient
    database note.
  - `ContextBuilder.AppendSystemNote` (new) queues pipeline-owned
    ambient system messages and emits them after memory index in
    `BuildRequest`, matching Rust's
    `context_builder.rs:356-369` injection position. Trim-space guard
    rejects empty notes at append time.
  - runtime-facing tests:
    `internal/pipeline/hippocampus_stage_test.go` asserts the non-empty
    + empty-registry paths; `internal/agent/memory/compaction_test.go`
    covers the Rust port's unit contract.
  - SYS-01-003 and SYS-01-004 now carry artifact-level closure
    evidence; re-audit is the next step.
- 2026-04-17: Restored checkpoint continuity on the live request path through
  the repository-owned lifecycle. `buildAgentContext(...)` now loads the
  latest checkpoint via `CheckpointRepository.LoadLatestRecord(...)` and injects
  a compact `[Checkpoint Digest]` ambient note rather than replaying the full
  checkpoint blob. This closes the missing live restore seam for SYS-01-009,
  but its final classification remains synthesis/improvement-candidate rather
  than pure Rust parity because the restore shape is intentionally narrower.
- 2026-04-17: Closed the prompt-layer tool discoverability seam for the live
  request path. `buildAgentContext(...)` now rewrites the prompt-layer
  `ToolNames` / `ToolDescs` from the same selected tool defs that populate
  `llm.Request.Tools`, instead of advertising the daemon boot's full registry
  while the structured request carried a pruned set.
- 2026-04-17: Closed the message-history empty-content seam in the request
  builder. `ContextBuilder.BuildRequest` now drops compacted history messages
  that collapse to empty content instead of emitting blank conversational
  messages and relying on `llm.Service` to scrub them later.
- 2026-04-17: Unified baseline/exercise onto the runtime request path.
  `/api/models/exercise` no longer bypasses into direct `llm.RunExercise(...)`;
  it now uses `llm.ExerciseModels(...)` with a pipeline-backed sender, the same
  truth surface as `roboticus models exercise`. Exercise calls now set both
  `NoCache` and `NoEscalate`, and `llm.Service` now actually honors
  `NoEscalate` by suppressing the configured fallback chain during those runs.
