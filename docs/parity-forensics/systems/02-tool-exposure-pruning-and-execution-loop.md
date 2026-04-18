# System 02: Tool Exposure, Pruning, and Execution Loop

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-16
- Related release: v1.0.6

## Why This System Matters

This system controls how many tools the model sees, which tools are pinned,
which remote/MCP tools are penalized, how tool-search telemetry is emitted, and
how the request-to-loop handoff preserves the same tool surface across turns.

This is one of the highest-value migration seams because it has:

- direct token-cost impact
- direct effect on warm-up and baseline behavior
- direct effect on loop quality and "unknown tool" / wrong-tool behavior
- a known history of parity-shaped code existing while the live request path
  still bulk-injected everything

## Scope

In scope:

- tool registry and descriptor lifecycle
- embedding at registration/startup time
- query-time tool search and pruning
- always-include / pinned-tool behavior
- MCP latency penalty behavior
- trace annotations under `tool_search.*`
- reuse of the selected tool set across a single user request / ReAct loop

Out of scope:

- tool implementation behavior after selection
- model/provider execution itself
- general memory retrieval unrelated to tool choice

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Tool search defaults and ranking | `crates/roboticus-agent/src/tool_search.rs:28-115` |
| Pipeline pruning ownership | `crates/roboticus-pipeline/src/core/tool_prune.rs:31-181` |
| Context-builder integration | `crates/roboticus-pipeline/src/core/context_builder.rs:242-250`, `467-470` |
| Tool-search trace contract | `crates/roboticus-pipeline/src/trace_helpers.rs:57-104` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Tool registry / descriptors | `internal/agent/tools/registry.go:12-139` |
| Current request-path pruning hook | `internal/daemon/daemon_adapters.go:140-158` |
| Duplicate older implementation | `internal/agent/tool_search.go` |
| Newer pruning implementation | `internal/agent/tools/tool_search.go` |
| Tool defs passthrough | `internal/agent/tools/registry.go:69-85` |

## Live Go Path

Current observed state on 2026-04-16:

1. `buildAgentContext` is the place where the final tool set reaches the
   `ContextBuilder`.
2. On the committed path, that builder historically received the entire tool
   set from `ToolDefs()`.
3. A remediation is currently in progress to move live ownership to semantic
   pruning using descriptor embeddings and a bounded budget.
4. The repo has carried more than one plausible pruning implementation, which
   is itself a migration risk until only one path remains authoritative.

## Artifact Boundary

The artifact boundary for this system is the final `llm.Request.Tools` slice
plus the emitted `tool_search.*` trace annotations.

Parity is not satisfied unless tests prove:

- which tools were considered
- which tools were selected
- which pinned tools survived
- how many tokens were saved
- whether the selected tool set, not just helper output, reached the request

## Success Criteria

- Closure artifact(s):
  - final `llm.Request.Tools`
  - `tool_search.*` trace annotations emitted for the same turn
- Live-path proof:
  - runtime-facing tests prove the final injected tool defs are the selected
    defs, not `Registry.ToolDefs()` wholesale
  - traces prove candidate count, selected count, pruned count, token savings,
    and pinned-tool survival for the actual request
  - loop/runtime reuse is demonstrated on the live path rather than inferred
    from helper comments
- Blocking conditions:
  - more than one plausible pruning owner remains
  - MCP / pinned / operational tools are classified only at the helper level
    rather than on the injected request
  - `tool_search.*` annotations can drift from the live selected set
- Accepted deviations:
  - any Go-native pinned-tool surface must be documented as a functional
    analogue or synthesis target, not hand-waved as "close enough"

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-02-001 | P0 | Live tool surface historically unbounded | Rust prunes before request assembly with top-k + token budget | Go committed path bulk-injected all tool defs until current remediation | Missing Functionality | Active remediation | Rust: `context_builder.rs:242-250`; Go: `internal/daemon/daemon_adapters.go` |
| SYS-02-002 | P1 | Duplicate pruning implementations | One canonical `tool_search.rs` | Go carried at least two plausible pruning implementations | Degradation | Active remediation | `internal/agent/tool_search.go`, `internal/agent/tools/tool_search.go` |
| SYS-02-003 | P1 | Trace contract must match Rust telemetry surface | Rust writes `tool_search.candidates_considered`, `selected`, `pruned`, `token_savings`, `top_scores`, `embedding_status` | Go must prove that the emitted trace reflects the actual selected tool set reaching the request | Missing Functionality | Open | Rust: `trace_helpers.rs:57-104`; Go runtime re-audit pending after remediation lands |
| SYS-02-004 | P1 | Loop-scoped reuse of selected tools | Rust request passes the selected tool set forward after pruning | Go docs claim per-user-request reuse; must be proven against the actual loop/runtime ownership | Missing Functionality | Open | Rust: `context_builder.rs:467-470`; Go: `internal/daemon/daemon_adapters.go` comments + runtime ownership path |
| SYS-02-005 | P2 | Always-include semantics are broader than a simple Rust-defaults comparison | Rust has both a crate-level `SearchConfig::default()` pin set (`memory_store`, `delegate`) and a richer runtime operational-tool pin set in pipeline pruning | Go is converging on a Go-native functional analogue that pins memory recall/search and introspection tools; the audit risk is confusing the Rust test-fixture default with the richer runtime baseline | Improvement candidate / classification seam | Open | Rust: `tool_search.rs:30-45`, `crates/roboticus-pipeline/src/core/tool_prune.rs`; Go: `internal/agent/tools/tool_search.go`, `internal/agent/tools/prune.go` |
| SYS-02-006 | P1 | Tool-surface divergence included prompt-vs-request split-brain | Desired behavior is not just "same top-k knobs," but a coherent tool surface across registry descriptors, prompt-layer discoverability, request injection, and runtime reuse | Go now has registry-time descriptors, descriptor embeddings, query-time `SelectToolDefs(...)`, and prompt-layer narrowing from `selectedDefs`. The remaining audit work is to prove loop/runtime reuse and any non-pipeline callers stay on that same authoritative surface | Improved, not closed | Open | `internal/agent/tools/registry.go`, `internal/agent/tools/prune.go`, `internal/daemon/daemon_adapters.go`, `internal/daemon/daemon_adapters_test.go::TestBuildAgentContext_PromptToolRosterUsesSelectedDefs` |
| SYS-02-007 | P1 | Go registry is missing 10 of Rust's 12 runtime operational pinned tools | Rust `always_include_operational_tools` pins `get_memory_stats`, `get_runtime_context`, `list-subagent-roster`, `list-available-skills`, `compose-subagent`, `compose-skill`, `memory_store`, `delegate-subagent`, `orchestrate-subagents`, `task-status`, `retry-task`, `list-open-tasks` | Go registry registers `get_memory_stats`, `get_runtime_context`, and the closest analogue `get_subagent_status`; the other nine (subagent composition, orchestration, delegation, skill management, task lifecycle, explicit memory write) do not exist as agent-callable tools | Missing Functionality | Open | Discovered during v1.0.6 P0 remediation. Rust: `crates/roboticus-pipeline/src/core/tool_prune.rs:184-199`. Go registration sites: `internal/daemon/daemon.go:287-325`. Deferred to a later tool-surface remediation phase because each missing tool needs its own impl + tests + subagent/task/skill subsystem integration. |
| SYS-02-008 | P1 | A third parallel pruning implementation existed as dead code | Rust has one authoritative pruning owner (`core/tool_prune.rs` calling `agent::tool_search`) | Go previously carried THREE parallel implementations: `internal/agent/tool_search.go` (older, staged for deletion), `internal/agent/tools/tool_search.go` (new authoritative owner), and `internal/pipeline/tool_prune.go` (had its own `ToolPruner` struct + `PruneByEmbedding` + private `cosineSimilarity` — test-only, zero production callers) | Degradation | Landed / closed | `internal/pipeline/tool_prune.go` + tests deleted in v1.0.6 P0 remediation along with `TestPruneByEmbedding`/`TestCosineSimilarity` in `wave8_test.go`. Finding was not in prior audit passes — it was surfaced only when the new `ToolPruner` interface collided with the dead struct's name. |
| SYS-02-009 | P1 | Registry-backed tool selection and request injection were still relying on Go map iteration order | Equal-score tools and emitted tool defs should remain deterministic across runs so the selected surface, prompt roster, and runtime loop do not drift on hash iteration noise | Go registry now preserves stable registration order across descriptors, names, and tool defs. Equal-score pruning remains stable because the ranker preserves descriptor order, and the registry now makes that order explicit instead of inheriting random map iteration. | Remediated | Closed | `internal/agent/tools/registry.go`, `internal/agent/tools/registry_test.go`, `internal/agent/tools/tool_search_test.go` |

## Intentional Deviations

Potential synthesis opportunities already visible:

- Go's descriptor lifecycle and operator-configurable tool-search knobs may be
  a genuine improvement, but they should still preserve Rust's stronger notion
  of operationally essential tools.
- The right target may be a synthesis: Rust's disciplined runtime pinning plus
  Go's richer memory/introspection tool surface.

Landed in v1.0.6 P0 (pending auditor classification confirmation):

- `[tool_search]` TOML section (`core.ToolSearchConfig`) — Rust has no
  equivalent config surface; Rust hard-codes its ranking knobs at runtime.
  Go exposes `top_k`, `token_budget`, `mcp_latency_penalty`, and
  `always_include` as operator-overridable. Classification candidate:
  Improvement, because on-machine context ceilings vary widely by model
  and operators need to tune pruning without a rebuild.
- `DefaultToolSearchConfig.AlwaysInclude` is a 5-tool Go-native functional
  analogue of Rust's 12-tool operational list:
  `["recall_memory", "search_memories", "get_memory_stats",
    "get_runtime_context", "get_subagent_status"]`. This is the maximal
  pin set achievable against Go's current registry without dead pins;
  the remaining surface gap is tracked as SYS-02-007.
- Trace namespace corrected from `toolsearch` to `tool_search` to match
  Rust's `ns::TOOL_SEARCH` constant. Similarly `taskstate` → `task_state`
  for the same reason. These were pre-existing parity bugs in Go caught
  by the fitness test's own self-contradiction (the test asserted the
  wrong values while its comment said "must match Rust exactly").

## Remediation Notes

Known in-flight work:

- move live request composition to semantic tool pruning
- embed descriptors at registration/startup
- emit `tool_search.*` trace annotations
- delete the older duplicate pruning path

Important review rule for this system:

- do not "restore parity" by blindly reverting to Rust's bare
  `SearchConfig::default()` pin set; the runtime baseline in Rust is richer than
  that, and Go's novel memory tooling may justify a stronger synthesis still

Acceptance bar for closure:

- `llm.Request.Tools` is asserted in runtime-facing tests
- the selected set is bounded by budget, not merely ranked in a helper
- trace annotations reflect the same selected set
- only one pruning implementation remains plausibly live

## Downstream Systems Affected

- System 01: Request construction and context assembly
- System 05: Routing and model selection
- System 08: MCP and external integrations

## Open Questions

- Which exact Go file/function will be the durable authoritative pruning owner
  after remediation lands?
- Will the selected tool set be cached per request/session turn, or recomputed
  in each loop iteration?
- Which pinned-tool defaults are now considered the approved migration target?
- Is the current SYS-02-005 pinned-tool/defaults row only part of a broader
  tool-surface divergence that still needs to be split into additional
  findings once the active remediation branch settles?
- After the active remediation lands, is there exactly one authoritative tool
  surface spanning registry descriptors, selected tool defs, trace output, and
  loop reuse?

## Progress Log

- 2026-04-16: Initialized System 02 document from current live-path audit.
- 2026-04-16: Seeded known divergences around ownership, duplicate
  implementations, telemetry parity, and pinned-tool semantics.
- 2026-04-16: Recorded pending feedback that the currently documented
  pinned-tool/defaults divergence may not capture the full tool-surface gap;
  revisit after active remediation and adjacent system reads complete.
- 2026-04-16: Deepened the pinned-tool/defaults finding to distinguish Rust's
  crate-level defaults from Rust's richer runtime operational-tool pinning, so
  future parity work does not regress Go toward the wrong baseline.
- 2026-04-16: v1.0.6 P0 tool-pruning remediation landed (pending auditor
  re-validation). Changes summary:
  - pipeline-owned `stageToolPruning` (new) runs between cache-check and
    prepare-inference; calls `ToolPruner.PruneTools`; annotates trace
    under `tool_search.*` per Rust parity; writes selected defs onto
    `session.Session.selectedToolDefs`
  - `internal/pipeline/trace_tool_search.go` + `AnnotateToolSearchTrace`
    emit the six Rust-parity keys plus Go's richer embedding-status
    diagnostics
  - `core.ToolSearchConfig` + TOML `[tool_search]` section declared;
    operator overrides resolved once at daemon assembly via
    `resolveToolSearchConfig`
  - `prunerAdapter` in `internal/daemon/daemon_adapters.go` implements
    `pipeline.ToolPruner` using `agenttools.SelectToolDefs`
  - `buildAgentContext` reads `sess.SelectedToolDefs()` as the primary
    path; defensive fallback to inline `SelectToolDefs` for non-pipeline
    callers (unchanged output shape)
  - prompt-layer tool discoverability now follows the same selected tool
    set instead of advertising the daemon boot's full registry while the
    structured request carried a pruned set
  - trace namespace constants `TraceNSToolSearch` and `TraceNSTaskState`
    corrected to match Rust (`tool_search`, `task_state`); fitness test
    updated in lockstep
  - `internal/pipeline/tool_prune.go` (third parallel implementation,
    test-only, zero production callers) deleted along with its tests
    and the `TestPruneByEmbedding`/`TestCosineSimilarity` cases in
    `wave8_test.go` (recorded as SYS-02-008)
  - `DefaultToolSearchConfig.AlwaysInclude` moved from Rust's
    test-fixture default (`memory_store`, `delegate` — both dead names
    in Go's registry) to a 5-tool Go-native functional analogue
    (recorded in Intentional Deviations; missing tools tracked as
    SYS-02-007)
  - runtime-facing pipeline tests in
    `internal/pipeline/tool_pruning_stage_test.go` assert the session
    side effect, the six trace annotation keys, and graceful
    degradation on pruner error.
