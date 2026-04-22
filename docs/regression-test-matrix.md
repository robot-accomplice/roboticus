# Roboticus Regression Test Matrix

This matrix defines the minimum regression coverage required to support the
feature-complete contract in `docs/feature-complete-checklist.md`.

Transition and release sequencing are governed by
`docs/migration-release-policy.md`.

## Test Layers

- `L0` Architecture fitness tests
- `L1` Unit tests
- `L2` Integration / route / subsystem tests
- `L3` Live smoke and operator workflow tests
- `L4` Behavior / efficacy / release-gate tests

## Release Gate Commands

Blocking commands for feature-complete releases:

- `go test ./...`
- `go test ./internal/api -run Architecture -count=1`
- `go test ./internal/llm ./internal/db ./internal/api -count=1`
- `go test ./internal/parity -count=1`
- `go test -v -run TestLiveSmokeTest .`

## Matrix

### R-ARCH: Architecture Integrity

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-ARCH-01 | Routes remain thin connectors, not business-logic owners | `internal/api/architecture_test.go` | L0 |
| R-ARCH-02 | Route handlers do not import `internal/agent` directly | `internal/api/architecture_test.go` | L0 |
| R-ARCH-03 | Connectors use `pipeline.RunPipeline()` instead of direct `p.Run()` | `internal/api/architecture_test.go` | L0 |
| R-ARCH-04 | Pipeline does not depend back on `internal/api` or `AppState`-style service bags | `internal/api/architecture_test.go` | L0 |

### R-PAR: Parity Governance

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-PAR-01 | Every reopened or deferred parity finding documented in system docs is represented in the authoritative v1.0.7 roadmap | `internal/parity/roadmap_consistency_test.go` | L0/L1 |
| R-PAR-02 | Architecture-led parity seams remain represented in the roadmap instead of being lost between architecture docs and system docs as work closes and scope changes | `internal/parity/roadmap_consistency_test.go` | L0/L1 |

### R-API: Contract Honesty

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-API-01 | Read paths must not hide DB/query failures behind `200` empty/default payloads | route tests across `internal/api/routes/*_test.go` | L2 |
| R-API-02 | Write paths must reject invalid persisted state instead of accepting it silently | route tests for config/theme/subagent/config-key flows | L2 |
| R-API-03 | Any intentionally unavailable surface must return explicit disabled/unavailable semantics | route tests + smoke | L2/L3 |
| R-API-04 | Stream and non-stream message surfaces preserve behavior parity where required | route/integration tests + smoke | L2/L3 |
| R-API-05 | Trace route families are self-describing at the API boundary: `/api/traces` emits `route_family=traces` with summary/search artifact markers, while `/api/observability/traces` emits `route_family=observability_traces` with observability-page / waterfall markers | `internal/api/routes/routes_test.go`, `internal/api/routes/audit_observability_test.go`, `internal/api/response_shape_test.go` | L1/L2 |
| R-API-06 | Agent roster and editable subagent list share the same authoritative enriched subagent projection, so operator-facing agent surfaces cannot drift on which subagents exist or what metadata they expose | `internal/api/routes/admin_test.go`, `internal/api/routes/runtime_workspace_test.go` | L1/L2 |
| R-API-07 | The Observability trace flow keeps visible macro/detail RCA controls and an explicit trace-only fallback when canonical diagnostics are missing, instead of silently collapsing back into the old stage dump | `internal/api/dashboard_modularity_test.go` | L1 |
| R-API-08 | The Observability trace expansion renders as one integrated, bounded left-to-right decision flow with visible macro/detail controls, a dense top status banner plus bottom conclusion banner, compact macro blocks that expose only one useful visible signal per node, floating hover/detail affordances, and explicit scrolling instead of parallel flow/RCA panels or viewport overflow | `internal/api/dashboard_modularity_test.go` | L1 |
| R-API-09 | Fresh live turns with canonical diagnostics are not mislabeled as trace-only fallback: `pipeline_traces.turn_id` and `turn_diagnostics.turn_id` must join on the same authoritative turn id | `internal/pipeline/coverage_boost_test.go`, `internal/api/routes/audit_observability_test.go` | L1/L2 |
| R-API-10 | Observability RCA preserves retry atomicity: repeated flow steps carry an explicit repeat marker, detail mode exposes per-attempt sequence plus guard/verifier retry cause and same-model vs fallback reuse, and stale trace-only fallback overlays are cleared when the active turn or session changes | `internal/api/dashboard_modularity_test.go` | L1 |
| R-API-11 | Observability RCA conclusion text is interpretive rather than tautological, macro flow blocks encode clean/concern/broken state through the same persisted evidence used for the narrative, the dense top banner uses the same thresholded severity semantics and aggregate-health meaning as the flow, every banner chip exposes an explanatory hover/focus tooltip for operator interpretation, and persisted `turn_diagnostics` summaries do not ship placeholder boilerplate as the conclusion source | `internal/api/dashboard_modularity_test.go`, `internal/pipeline/turn_diagnostics_test.go`, `internal/api/routes/traces_turndetail_test.go` | L1 |
| R-API-12 | Observability trace rows keep the authoritative turn id directly operator-usable through a copy affordance and full-id tooltip, and repeated routing passes distinguish normal post-tool follow-up from retry/fallback churn instead of collapsing all `routing ×N` into one concern state | `internal/api/dashboard_modularity_test.go` | L1 |
| R-API-13 | Theme textures stop at the shell layer: dense operator text surfaces such as data tables render on opaque surface tokens at rest and on hover, so readable rows never sit directly on top of body or surface textures | `internal/api/dashboard_modularity_test.go` | L1 |

### R-BEH: Behavior Hardening

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-BEH-01 | Same-route / no-progress churn is terminated by the framework before it expands into repeated identical-route completions with no new tool progress, and RCA preserves the route-reuse / termination cause explicitly | `internal/agent/loop_test.go`, focused live RCA proof | L1/L4 |
| R-BEH-02 | Tool-bearing turns write `tool_calls` audit rows and RCA `tool_call_count` from the same execution-owned events that actually ran, so successful live tool use cannot collapse to zero-count diagnostics | `internal/agent/loop_test.go`, persistence integration proof, focused live RCA proof | L1/L2/L4 |
| R-BEH-03 | Persistent-artifact authoring turns privilege artifact-writing tools over authority-write tools, and false “created note/file/document” claims are retried unless the turn carries matching artifact-writing evidence. Source-backed exact authoring stays on the authoring path rather than being upcast into the generic code/delegation envelope | `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/turn_policy_test.go`, `internal/pipeline/guards_truthfulness_test.go`, focused live authoring proof | L1/L4 |
| R-BEH-04 | Focused direct authoring turns, including bounded multi-artifact note/document/file creation, use a capability-scoped tool profile instead of inheriting the generic operational always-include set, so the selected request surface collapses to artifact-writing plus only the minimum runtime/retrieval context actually justified by the turn. Artifact names like `runbook` or `policy` do not by themselves trigger authority-mutation mode, and the selected tool surface remains authoritative for both prompt guidance and execution, so out-of-surface tools are neither advertised nor executed | `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/turn_policy_test.go`, `internal/agent/prompt_test.go`, `internal/agent/loop_test.go`, focused live authoring proof | L1/L4 |
| R-BEH-05 | Once a turn has already made substantive tool-backed execution progress, narrative-only guard/verifier findings no longer trigger another full inference attempt; retries remain allowed only for execution-critical defects such as false execution claims, incomplete work, or broken output contracts | `internal/pipeline/guard_retry_test.go`, `internal/pipeline/post_success_retry_policy_test.go` | L1/L4 |
| R-BEH-06 | Once a side-effecting tool call has already succeeded in a turn, duplicate replays of the same protected effect are suppressed from one central tool-semantics policy instead of being executed again and risking non-idempotent side effects; replay identity must follow the protected resource/effect, not just byte-identical raw arguments | `internal/agent/loop_test.go`, `internal/agent/tools/semantics_test.go` | L1/L4 |
| R-BEH-09 | Replay-protected side effects surface into canonical diagnostics and operator RCA with suppression count and causal explanation instead of staying hidden in loop internals | `internal/agent/loop_test.go`, `internal/pipeline/turn_diagnostics_test.go`, `internal/api/dashboard_modularity_test.go` | L1/L4 |
| R-BEH-07 | Procedurally uncertain execution turns can trigger applied-learning retrieval from procedural and episodic memory before exploration begins, while simple direct authoring turns remain exempt; when the pipeline has already injected current-turn retrieved evidence and a memory index, that evidence becomes the first memory authority and prompt guidance only permits follow-up `recall_memory` / `search_memories` when the injected evidence is insufficient for the task. Source-backed authoring retries must prefer authoritative `read_file`-style source reads over memory-search churn when verifier identifies `source_artifact_unread`, prompt-declared source inputs must be protected as read-only turn resources so artifact-writing tools cannot overwrite them during the same request, and source-backed focused turns must pin an authoritative file-read tool on the selected surface instead of relying on semantic ranking luck | `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/perception_test.go`, `internal/agent/prompt_test.go`, `internal/pipeline/verifier_test.go`, `internal/pipeline/turn_policy_test.go`, `internal/agent/tools/builtins_test.go`, `internal/daemon/daemon_adapters_test.go`, `internal/pipeline/post_success_retry_policy_test.go` | L1/L4 |
| R-BEH-08 | Episode summaries preserve reusable procedural outcome semantics, and consolidation promotes recurring success/failure/partial patterns into reusable procedural knowledge instead of dropping negative or mixed experience on the floor. Final turn status and verifier outcome remain authoritative, so degraded/verifier-failed turns are not captured as `success` purely because tool calls succeeded | `internal/agent/memory/reflection_episode_test.go`, `internal/pipeline/post_turn_test.go`, `internal/agent/memory/consolidation_distillation_test.go`, `internal/agent/memory/retrieval_path_test.go` | L1/L4 |
| R-BEH-10 | Direct execution turns that keep spending successful read-only exploration calls without crossing into artifact writes, execution, delegation, or other real progress are terminated as framework-owned exploratory churn, and RCA preserves the blocked tool plus streak count instead of leaving operators to infer the loop from repeated tool rows | `internal/agent/loop_test.go`, `internal/pipeline/turn_diagnostics_test.go`, focused live RCA proof | L1/L4 |
| R-BEH-11 | Explicit body-scoped `session_id` values must resolve to a durable session row or fail cleanly as not found; the pipeline is not allowed to continue on a phantom session shell and surface a storage-time foreign-key `500` later | `internal/pipeline/coverage_boost_test.go`, `internal/api/routes/agent_test.go`, focused live request proof | L1/L2/L4 |
| R-BEH-12 | Structured tool-call arguments and tool results pass through one central normalization seam before execution/observation; malformed-but-repairable arguments are normalized with explicit RCA evidence, malformed arguments without a qualified transformer are rejected before tool execution, and operator narratives preserve whether normalization was exact, repaired, or absent | `internal/agent/tools/normalization_test.go`, `internal/agent/loop_test.go`, `internal/pipeline/turn_diagnostics_test.go` | L1/L4 |
| R-BEH-13 | Provider-facing tool message serialization uses the same normalization authority as tool-call/result normalization: Ollama follow-up messages use documented `tool_name` + structured `arguments` payloads, raw provider request/response envelopes are captured in inference RCA, and provider-specific tool-format drift is no longer hidden behind the generic OpenAI-compatible path | `internal/llm/tool_message_normalization_test.go`, `internal/llm/inference_observer_test.go`, `internal/llm/client_formats_test.go` | L1/L2/L4 |
| R-BEH-14 | Applied-learning retrieval and reusable outcome capture are first-class RCA facts: the canonical summary and operator flow must expose whether prior experience was consulted, whether successes/failures/partials were in scope, and whether the turn merely captured or actually promoted reusable procedural knowledge | `internal/pipeline/turn_diagnostics_test.go`, `internal/api/dashboard_modularity_test.go` | L1/L4 |
| R-BEH-15 | Historical assistant `tool_calls` remain immutable when pending execution state is consumed, so follow-up provider requests replay the exact original tool-call plan instead of a mutated duplicate or partial subset | `internal/session/session_test.go` | L1/L4 |
| R-BEH-16 | Focused filesystem inspection turns emit structured inspection evidence, direct-execution turns do not terminate on promissory `let me check...` filler, and read-only churn only trips when inspection calls fail to narrow the task instead of treating every successful inspection call as equivalent exploration | `internal/agent/tools/builtins_test.go`, `internal/agent/tools/inspection_proof_test.go`, `internal/agent/loop_test.go`, focused live behavioral soak proof | L1/L4 |
| R-BEH-17 | `get_runtime_context` and prompt guidance expose the same effective sandbox truth: workspace anchoring, absolute-path allowlist constraints, and protected read-only source artifacts remain aligned between runtime tool output and prompt instructions | `internal/agent/tools/builtins_test.go`, `internal/agent/prompt_test.go` | L1/L4 |
| R-BEH-18 | Inspection-shaped questions such as `what's in the vault`, `what about the vault in your workspace`, `brief summary of the contents of /path`, or `list the projects in /path` reuse the same focused-inspection detector as imperative inspection turns, so task synthesis, retrieval gating, and envelope selection do not drift apart by phrasing | `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/turn_policy_test.go`, focused live inspection proof | L1/L4 |
| R-BEH-19 | Focused inspection turns resolve explicit paths and obvious allowlisted aliases such as a single Desktop vault or the operator's code folder through one shared target-resolution seam, and explicit path-clarification follow-ups stay on that same inspection path instead of dropping back to generic question handling | `internal/pipeline/inspection_turn_test.go`, `internal/agent/prompt_test.go`, focused live Desktop-vault and code-folder proof | L1/L4 |
| R-BEH-20 | Read-only inspection turns may report discovered filenames and paths from authoritative inspection evidence without tripping `artifact_set_overclaim`; authored-artifact claim checks remain scoped to output-contract or mutation turns | `internal/pipeline/verifier_test.go`, focused live code-folder inspection proof | L1/L4 |
| R-BEH-21 | Authoring turns resolve allowlisted non-workspace destinations such as the Desktop Obsidian vault through the same shared target-resolution seam, and prompt/tool contracts expose the real absolute-allowed write surface instead of falsely implying workspace-only writes | `internal/pipeline/filesystem_target_test.go`, `internal/agent/prompt_test.go`, `internal/agent/tools/builtins_test.go`, focused live Desktop-vault report proof | L1/L4 |
| R-BEH-22 | Inspection-backed report authoring over a concrete filesystem target classifies as direct task execution rather than `creative`, disables memory-first widening, and uses a bounded analysis+authoring tool surface whose central semantics map includes `search_files` and other authoritative inspection tools | `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/turn_policy_test.go`, `internal/agent/tools/semantics_test.go`, focused live Desktop-vault project-report proof | L1/L4 |
| R-BEH-23 | Large code-root project reports use a first-class project-inventory inspection tool to derive bounded project metadata instead of degenerating into denied shell choreography or extension-globbing heuristics | `internal/agent/tools/project_inventory_test.go`, `internal/agent/tools/semantics_test.go`, `internal/daemon/daemon_adapters_test.go`, focused live Desktop-vault project-report proof | L1/L4 |
| R-BEH-24 | Context compaction preserves assistant tool-call messages and their matching tool-result messages as one atomic exchange across both request-building compaction and the pipeline-owned pre-inference in-place compactor, so budget pressure cannot corrupt provider requests or verifier retries by dropping a required tool reply while keeping the `tool_call_id` | `internal/pipeline/compaction_test.go`, `internal/agent/context_user_message_invariant_test.go`, focused live scheduling-history proof | L1/L4 |
| R-BEH-25 | Artifact-authoring turns may finalize with a concise completion confirmation when authoritative write evidence exists; the verifier does not degrade the turn just because the chat reply omits internal report/document fields that belong inside the written artifact, but mixed-output turns still owe chat-level coverage for any non-artifact deliverables | `internal/pipeline/verifier_test.go`, focused live Desktop-vault project-report proof | L1/L4 |
| R-BEH-26 | Advisory watchdog events such as `stage_liveness_warning` remain visible in RCA but do not mark an otherwise successful turn as `degraded` | `internal/pipeline/turn_diagnostics_test.go`, focused live Desktop-vault project-report proof | L1/L4 |

### R-CORE: Entry Path Behavior

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-CORE-01 | Non-stream message path performs full pipeline/persistence flow | route/integration tests + smoke | L2/L3 |
| R-CORE-02 | Streaming path uses the same business pipeline and persistence semantics | integration + smoke | L2/L3 |
| R-CORE-03 | Health/logs/agent metadata remain live and truthful | route tests + smoke | L2/L3 |

### R-CH: Channel Reliability

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-CH-01 | Channel ingress uses the shared policy/inference path | integration tests per channel + smoke | L2/L3 |
| R-CH-02 | Retry queue persistence survives restart and supports dead-letter replay | queue/channel tests + smoke | L1/L2/L3 |
| R-CH-03 | Channel reply formatting does not leak orchestration metadata | guard/behavior tests | L2/L4 |
| R-CH-04 | Telegram and WhatsApp webhook ingress is adapter-owned: routes consume normalized `InboundMessage` batches, WhatsApp challenge verification uses the adapter verifier, and POST webhook bodies validate adapter-owned signatures before pipeline dispatch | `internal/api/routes/admin_webhooks_test.go`, `internal/channel/telegram_coverage_test.go`, `internal/channel/coverage_boost_test.go` | L1/L2 |
| R-CH-05 | Adapters preserve baseline normalized metadata for shared filters instead of forcing downstream inference: Telegram/Signal/Discord/WhatsApp emit explicit `is_group` context plus transport-specific identifiers where available, and Matrix preserves explicit `room_id` / `sender_mxid` identifiers on the live path | `internal/channel/telegram_coverage_test.go`, `internal/channel/signal_coverage_test.go`, `internal/channel/discord_coverage_test.go`, `internal/channel/coverage_boost_test.go`, `internal/channel/matrix_test.go` | L1/L2 |
| R-CH-06 | Matrix preserves authoritative `is_direct` when `m.direct` account data is present, and emits `is_group=false` only for those proven direct rooms without inventing a stronger group claim for unknown rooms | `internal/channel/matrix_test.go` | L1/L2 |

### R-SESS: Sessions And Scope

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-SESS-01 | Session scope separation and uniqueness invariants hold | DB/session tests | L1/L2 |
| R-SESS-02 | Session archive/delete/rotation preserve the documented lifecycle semantics | route tests + smoke | L2/L3 |
| R-SESS-03 | Session insights/turns/feedback surfaces remain accurate | route tests | L2 |

### R-MEM: Memory And Context

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-MEM-01 | Retrieval contributes to context assembly correctly | pipeline/agent tests | L1/L2 |
| R-MEM-02 | Post-turn memory ingestion persists and reads back correctly | integration tests | L2 |
| R-MEM-03 | Memory recall avoids self-echo / stale summary regressions | agent/retrieval tests | L1/L2 |
| R-MEM-04 | Memory analytics and introspection expose live values, not placeholders | route tests + smoke | L2/L3 |
| R-MEM-05 | Memory search and explorer endpoints remain aligned with persisted state | route tests | L2 |
| R-MEM-06 | `search_memories` tool finds topic-specific memories via FTS5 + LIKE fallback | `internal/agent/tools/memory_search_test.go` | L1 |
| R-MEM-07 | Memory index is query-aware — topic-matched entries surface in first 1/3 of slots | `internal/agent/tools/memory_search_test.go` | L1 |
| R-MEM-08 | Memory index excludes tool-output noise (bash, introspect, errors) | `internal/agent/tools/memory_search_test.go` | L1 |
| R-MEM-09 | Confidence reinforce uses incremental +0.1, not binary reset to 1.0 | `internal/agent/tools/memory_search_test.go` | L1 |
| R-MEM-10 | Two-stage injection: `RetrieveDirectOnly` returns only working + ambient, not all tiers | `internal/agent/memory/retrieval_direct_test.go` | L1 |
| R-MEM-11 | FTS5 union strategy finds old memories via MATCH despite recency bias | `internal/agent/memory/retrieval_direct_test.go` | L1 |
| R-MEM-12 | Routed memory tiers execute concurrently but merge deterministically in subgoal/router order so retrieval latency improves without making evidence order nondeterministic | `internal/agent/memory/retrieval_parallel_test.go` | L1 |
| R-MEM-13 | Request construction restores the latest checkpoint in the fuller Rust-aligned shape (memory summary first, then active tasks and conversation digest) instead of a digest-only ambient note | `internal/daemon/daemon_adapters_test.go::TestBuildAgentContext_AppendsCheckpointRestoreFromRepository` | L1/L2 |
| R-MEM-14 | Retrieval fusion is a distinct stage before reranking: fused ordering rewards corroborated authority/freshness signals deterministically, and retrieval traces surface fusion-stage counters instead of burying fusion inside reranker heuristics | `internal/agent/memory/fusion_test.go`, `internal/agent/memory/retrieval_test.go` | L1/L2 |
| R-MEM-15 | Optional LLM reranking runs only through the central retriever seam, emits `retrieval.rerank.llm.*` RCA counters, and falls back cleanly to deterministic reranking on malformed output or provider failure | `internal/agent/memory/llm_reranker_test.go`, `internal/agent/memory/architecture_test.go` | L1/L2 |
| R-MEM-16 | Simple investigative task turns still retrieve evidence when the operator is asking for diagnosis, root cause, summary, or analysis, even after imperative-task retrieval is narrowed for direct execution turns | `internal/pipeline/task_synthesis_test.go` | L1/L2 |

### R-RT: Routing, Breakers, And Metascores

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-RT-01 | Breaker-tripped providers are excluded from selection | router/unit tests | L1 |
| R-RT-02 | Fallback order is deterministic when primary is unavailable | router/service tests | L1/L2 |
| R-RT-03 | Metascore routing actually drives execution, not just selection | `internal/llm/metascore_routing_test.go` | L2 |
| R-RT-04 | Session-aware and contextual metascore behavior remain effective | metascore fitness tests | L2/L4 |
| R-RT-05 | User weighting / spider-graph weighting changes the winner predictably when advertised | routing-profile tests | L2/L4 |
| R-RT-06 | Metascore-weighted routing improves outcome metrics over baseline on a fixed corpus | efficacy tests | L4 |
| R-RT-15 | Baseline runs, exercise prompt rows, and turn diagnostics persist host-resource snapshots from the shared sampler so benchmark conclusions can distinguish model weakness from host saturation | `internal/db/exercise_repo_test.go`, `internal/api/routes/routing_admin_exercise_test.go`, `internal/api/routes/traces_turndetail_test.go`, `internal/llm/resource_snapshot_test.go` | L1/L2 |
| R-RT-21 | Baseline runs and exercise prompt rows persist provider/model runtime-state snapshots, so empty-response benchmarks can distinguish model weakness from missing, unloaded, or unreachable model state | `internal/modelstate/sampler_test.go`, `internal/db/exercise_repo_test.go`, `internal/api/routes/routing_admin_exercise_test.go`, `internal/llm/exercise_models_test.go` | L1/L2 |
| R-RT-07 | OpenAI-compatible tool_call_id serialization includes explicit `content` field on assistant tool-call messages | `internal/llm/client_formats_test.go` | L1 |
| R-RT-08 | Tool result messages serialize `tool_call_id`, `content`, and `name` fields | `internal/llm/client_formats_test.go` | L1 |
| R-RT-09 | IntentMemoryRecall scoring rewards tool use and penalizes confabulation | `internal/llm/exercise_memory_recall_test.go` | L1 |
| R-RT-10 | Every model in CommonIntentBaselines has a MEMORY_RECALL entry | `internal/llm/exercise_memory_recall_test.go` | L1 |
| R-RT-11 | Routing trace annotations are emitted from the actual `llm.Request` selection site, including real message/tool counts, not a synthetic user-only approximation | `internal/llm/routing_trace_test.go` | L1 |
| R-RT-12 | `/api/traces/search` uses exact `tool_calls.tool_name` matching and parsed guard JSON instead of SQL `LIKE` over serialized trace blobs, and applies the `guard_name` filter before the final result limit so matching guarded traces are not hidden behind newer non-matching rows | `internal/api/routes/routes_test.go` | L1/L2 |
| R-RT-13 | Model-selection events persist the actual routed request's winner and user excerpt when turn/session/channel context is present | `internal/llm/model_selection_event_test.go` | L1 |
| R-RT-14 | `/api/models/exercise` now exercises the same pipeline-owned request path as the CLI, with `NoCache` + `NoEscalate`, instead of bypassing into direct LLM scoring | `internal/api/routes/routing_admin_exercise_test.go`, `internal/llm/service_complete_test.go` | L1/L2 |
| R-RT-15 | Streaming `NoEscalate` requests skip cache replay just like non-streaming `Complete`, so benchmark/raw-capability paths are not contaminated by cached content | `internal/llm/coverage_boost_test.go::TestService_Stream_NoEscalateSkipsCache` | L1/L2 |
| R-RT-16 | Provider-qualified persisted model policy is normalized onto the canonical model identity, so disabled / benchmark-only state is enforced consistently across routing, diagnostics, and benchmark gates | `internal/llm/role_routing_test.go`, `internal/llm/service_test.go`, `internal/api/routes/routing_admin_exercise_test.go` | L1/L2 |
| R-RT-17 | Benchmark/run-start and single-model exercise paths reuse the shared model lifecycle policy seam; disabled models are rejected unless explicitly forced | `internal/api/routes/routing_admin_exercise_test.go` | L1/L2 |
| R-RT-18 | Selected-model routing traces carry lifecycle state, reasons, and eligibility facts from the central model-policy seam so operator flow/RCA surfaces explain policy decisions truthfully | `internal/llm/routing_trace_test.go` | L1 |
| R-RT-19 | Canonical turn diagnostics are retrievable as a self-describing trace-family artifact so operator flow views can render macro/detail RCA from persisted truth instead of reconstructing from logs | `internal/api/routes/traces_turndetail_test.go` | L1/L2 |
| R-RT-20 | Routing evidence distinguishes hard request exclusions (for example missing tool capability) from soft evidence gaps (for example not exercised for the current intent class), and emits recommendation-grade callouts for ignored-but-unproven models | `internal/llm/role_routing_test.go`, `internal/llm/routing_trace_test.go` | L1/L2 |
| R-RT-21 | Intent-capability evidence uses the same canonical model identity across seeded baselines, imported exercise observations, live routing observations, and recommendation callouts, including alias resolution between bare routed names, direct provider-qualified names, and nested execution-provider specs, so exercised models are not falsely labeled `unexercised` for TOOL_USE because of namespace drift | `internal/llm/profile_test.go`, `internal/llm/coverage_db_test.go`, `internal/llm/role_routing_test.go`, `internal/llm/routing_trace_test.go` | L1/L2 |
| R-RT-22 | Intent-scoped exercise remains matrix-owned: CLI/API selectors can request one canonical intent class, but the shared exercise factory performs the filtering, validates the intent label, and preserves the same prompt-count / persisted-intent truth as the full matrix path | `internal/llm/exercise_models_test.go`, `internal/api/routes/routing_admin_exercise_test.go`, `cmd/models/models_test.go` | L1/L2 |
| R-RT-23 | Provider/model execution spec is joined from one authoritative seam, so routed targets that carry nested downstream namespaces such as `openrouter/openai/gpt-4o-mini` execute on the selected outer provider instead of being reinterpreted later as direct provider calls | `internal/llm/coverage_boost_test.go`, `internal/llm/service_complete_test.go` | L1/L2 |

### R-BOT: Bot Commands

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-BOT-01 | All 11 bot commands match and return expected content | `internal/pipeline/bot_commands_test.go` | L1 |
| R-BOT-02 | /model set and /breaker reset require Creator authority | `internal/pipeline/bot_commands_test.go` | L1 |
| R-BOT-03 | @bot_name stripping works for Telegram-style mentions | `internal/pipeline/bot_commands_test.go` | L1 |
| R-BOT-04 | /retry replays last assistant message or reports no history | `internal/pipeline/bot_commands_test.go` | L1 |
| R-BOT-05 | /help lists all registered commands | `internal/pipeline/bot_commands_test.go` | L1 |

### R-TOOLS: Tools, Policy, Browser, Plugins, MCP

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-TOOLS-01 | Tool policy + approval loops remain enforceable end-to-end | integration tests | L2/L3 |
| R-TOOLS-02 | Browser admin/runtime actions fail safely and perform advertised actions | browser tests + smoke | L1/L2/L3 |
| R-TOOLS-03 | Plugin discovery/execute remains stable, including daemon-owned startup scan/init, install/enable-time hot syncing into the live registry and semantic tool surface, and the shared script execution contract across skills and manifest-backed plugins | plugin tests + daemon tests + route tests + core script tests | L1/L2 |
| R-TOOLS-04 | MCP management surfaces stay aligned across API/UI/CLI where advertised | MCP tests + smoke | L2/L3 |
| R-TOOLS-07 | Runtime MCP connect/discover/disconnect keeps the live tool surface aligned with the connection manager instead of requiring daemon restart for MCP tool registration/pruning truth | MCP route tests + agent tool tests | L1/L2 |
| R-TOOLS-08 | Maintenance cache eviction targets the live `semantic_cache` table and uses the same `expires_at` TTL contract as cache lookup/write paths | scheduler tests | L1/L2 |
| R-TOOLS-09 | Prompt-layer tool roster is derived from the same selected per-request tool surface as `llm.Request.Tools`, including the authoritative zero-tools case | daemon adapter tests | L1/L2 |
| R-TOOLS-09A | Selected tool defs are reused across later loop turns instead of drifting back to the registry surface during request rebuilds | `internal/daemon/daemon_adapters_test.go::TestBuildAgentContext_ReusesSelectedToolSurfaceAcrossLoopTurns` | L1/L2 |
| R-TOOLS-10 | Registry-backed tool surfaces are deterministic: names, descriptors, tool defs, and equal-score pruning all preserve stable registration order instead of drifting on Go map iteration | agent tool tests | L1/L2 |
| R-TOOLS-11 | Runtime MCP discover mutates the manager-owned live connection, so refreshed MCP tools reach `AllTools()`, route responses, and the synced semantic tool surface instead of updating only a copied snapshot | MCP manager tests | L1/L2 |
| R-TOOLS-12 | MCP connection statuses and aggregated tool lists are deterministic by server name instead of drifting on Go map iteration, so operator/admin surfaces stay reproducible across runs | MCP manager tests | L1/L2 |
| R-TOOLS-13 | Dead MCP transports stay visible for diagnostics but no longer masquerade as healthy: `Statuses()` reports `connected=false` with an error, `tool_count=0`, and `AllTools()` excludes stale tools from the live aggregated surface | MCP manager tests | L1/L2 |
| R-TOOLS-14 | Artifact-writing tools emit one typed artifact-proof payload, preserve it through session tool-result metadata, and expose exact-content evidence when safely representable instead of collapsing success to byte-count strings | `internal/agent/tools/builtins_test.go`, `internal/session/session_test.go` | L1 |
| R-TOOLS-15 | The live loop passes the authoritative database store into `ToolContext`, so DB-backed tools such as `cron` succeed or fail on real persistence behavior instead of a missing-store plumbing bug | `internal/agent/loop_test.go`, focused live cron proof | L1/L4 |
| R-TOOLS-05 | Config-protection and action-verification guards block forbidden or fabricated behavior | guard tests + behavior tests | L1/L2/L4 |
| R-TOOLS-06 | Per-call MCP timeout fails only the timed-out call: the transport stays open, late responses are dropped, and a follow-on call can still succeed on the same connection | `internal/mcp/client_test.go` | L1/L2 |

### R-AN: Analysis And Recommendations

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-AN-01 | Turn/session analysis returns real, non-placeholder output | route tests | L2 |
| R-AN-02 | Recommendations are generated from live data and not fake-complete shells | route tests + smoke | L2/L3 |
| R-AN-03 | Operator analytics fail honestly on query failure | route tests | L2 |

### R-VER: Verification And Proof

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-VER-01 | Direct authoring verification uses typed artifact proof as primary post-execution evidence, so successful exact-content artifact writes are not downgraded by stale pre-inference gaps/contradictions | `internal/pipeline/verifier_test.go`, `internal/pipeline/guards_truthfulness_test.go`, `internal/pipeline/guard_context_population_test.go` | L1/L2 |
| R-VER-02 | Explicit exact-content artifact specs are parsed once and reused across turn sizing, decomposition, verification, verifier-retry finalization, and exact artifact-set validation, including equivalent directive forms such as `containing exactly` and `with content`, while resolving relative artifact names against explicit container directories in the prompt, classifying source/input artifact references separately from expected outputs, and excluding trailing post-artifact follow-up directives from the artifact body, so bounded multi-file authoring stays focused, embedded artifact bodies do not masquerade as subtasks, exact-content mismatch remains execution-critical after progress, invented extra-file claims fail verification, unexpected extra writes fail verification, truthful source-artifact references do not fail verification, and a failed verifier retry cannot overclaim success | `internal/pipeline/artifact_expectations_test.go`, `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/decomposition_test.go`, `internal/pipeline/post_success_retry_policy_test.go`, `internal/pipeline/verifier_test.go`, `internal/pipeline/pipeline_run_test.go` | L1/L2 |

### R-SCHED: Scheduler And Background Work

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-SCHED-01 | Cron CRUD contract remains stable | route tests | L2 |
| R-SCHED-02 | Cron worker executes due jobs, leases safely, and records runs | scheduler tests + smoke | L1/L2/L3 |
| R-SCHED-03 | UI-created schedule kinds are executable by the worker | integration tests | L2/L3 |
| R-SCHED-04 | Background maintenance tasks do real work or are explicitly disabled | smoke + subsystem tests | L2/L3 |
| R-SCHED-05 | Manual "run now" execution reuses the durable cron worker lifecycle instead of bypassing lease/run-history/retry ownership | route + scheduler tests | L1/L2 |
| R-SCHED-06 | Cron run recording writes the authoritative `error_msg` / `timestamp` schema without legacy fallback branching in the live worker path | scheduler tests | L1/L2 |
| R-SCHED-07 | Daemon-owned memory consolidation uses the shared heartbeat runtime instead of a bespoke ticker, and heartbeat interval ownership follows config/fallback policy | daemon + scheduler tests | L1/L2 |
| R-SCHED-08 | Dormant heartbeat tasks still target the live schema they claim to maintain; `MetricSnapshotTask` writes `metric_snapshots(id, metrics_json, alerts_json)` instead of stale columns | scheduler tests | L1/L2 |
| R-SCHED-09 | Daemon-owned maintenance cleanup uses the shared heartbeat runtime, and maintenance cadence follows config/fallback policy instead of staying as dead helper code | daemon + scheduler tests | L1/L2 |
| R-SCHED-10 | Treasury refresh runs only on its dedicated low-frequency cadence and does not fall back onto the application-health heartbeat interval | daemon + scheduler tests | L1/L2 |
| R-SCHED-11 | Treasury-state writers and readers use the live schema consistently: `usdc_balance`/`native_balance`/`atoken_balance` are written by meaning, and `/status` reads `usdc_balance` instead of a phantom `total_balance` field | scheduler + pipeline tests | L1/L2 |
| R-SCHED-12 | Scheduler status readers use the authoritative `cron_runs.timestamp` schema instead of carrying dead `created_at` fallback probing after writer normalization | pipeline tests | L1/L2 |
| R-SCHED-13 | Maintenance cleanup uses the same cache expiry contract as live cache lookup/write paths by deleting `response_cache` rows on `expires_at` instead of a second age-based rule | scheduler tests | L1/L2 |

### R-WAL: Wallet, Treasury, Payments

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-WAL-01 | Wallet read surfaces remain honest under missing state and failing state | route tests | L2 |
| R-WAL-02 | Treasury cached-state path is used where advertised instead of repeated live calls | unit/integration tests | L1/L2 |
| R-WAL-03 | EIP-3009 signing/output remains deterministic and correct | wallet tests | L1 |
| R-WAL-04 | x402 / payment flow remains integrated where advertised | wallet/integration tests | L1/L2 |

### R-DISC: Discovery, Runtime, A2A

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-DISC-01 | A2A handshake and runtime-discovery surfaces remain functional if advertised | route tests + smoke | L2/L3 |
| R-DISC-02 | Discovery/device/runtime surfaces do not silently fake success when incomplete | route tests | L2 |

### R-WS: WebSocket Protocol

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-WS-01 | WS upgrade requires valid ticket (anti-CSRF, anti-replay) | `internal/api/routes/ws_protocol_test.go` | L1/L2 |
| R-WS-02 | Topic subscription delivers only subscribed events, not all events | `internal/api/routes/ws_topics_test.go` | L1/L2 |
| R-WS-03 | Pipeline lifecycle events propagate through EventBus to WS subscribers | integration tests + smoke | L2/L3 |
| R-WS-04 | WS layer contains no business logic (thin connector enforcement) | `internal/api/architecture_test.go` | L0 |
| R-WS-05 | Zero `setInterval` polling calls survive in dashboard JavaScript | dashboard audit / smoke | L3 |

### R-THEME: Theme And Rendering

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-THEME-01 | Theme variable serialization round-trips correctly for preview rendering | theme route tests | L1/L2 |
| R-THEME-02 | `parseThemeColors` is cached per frame and invalidated on theme change | unit tests | L1 |
| R-THEME-03 | `_catalogThemeVars` does not crash when theme variables are undefined | route tests | L1/L2 |
| R-THEME-04 | Catalog entries carry full theme metadata (variables, textures, fonts) | route tests | L2 |
| R-THEME-05 | Theme install downloads textures to `~/.roboticus/themes/<name>/` and serves locally | theme route tests + smoke | L2/L3 |
| R-THEME-06 | Theme uninstall switches to default theme if active, removes from dropdown | theme route tests + smoke | L2/L3 |
| R-THEME-07 | Theme card previews use theme's own colors/fonts/textures, not current theme | dashboard smoke | L3 |
| R-THEME-08 | Installed themes reload into dropdown on server restart | theme route tests | L2 |

### R-LAYOUT: Workspace And Layout

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-LAYOUT-01 | Workspace footer is pinned to bottom without `calc()` misfire | layout tests / smoke | L2/L3 |
| R-LAYOUT-02 | Workstation positioning is equidistant with dynamic edge clamping | layout tests | L1/L2 |
| R-LAYOUT-03 | Canvas sizing is delegated to `resize()` — no conflicting CSS dimensions | layout tests | L1/L2 |

### R-CFG: Config Schema

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-CFG-01 | `/api/config/schema` returns all Config struct fields via reflection | route tests | L2 |
| R-CFG-02 | Config defaults match `DefaultConfig()` output | unit tests | L1 |
| R-CFG-03 | Config validation enforces constraints (ranges, enums, required) | unit tests | L1 |
| R-CFG-04 | Settings UI derives from schema, not hardcoded TOML | smoke | L3 |
| R-CFG-05 | TOML struct tags match Rust snake_case conventions (407 fields) | `internal/core/config_test.go` | L1 |
| R-CFG-06 | `IsWorkspaceConfined()` resolves `filesystem.workspace_only` without contradiction | `internal/core/config_validation_test.go` | L1 |
| R-CFG-07 | No `APIKeyEnv`, `TokenEnv`, `PasswordEnv` fields exist in config — keystore only | `internal/core/config_test.go` | L1 |
| R-CFG-08 | Prompt compression stays disabled by default in the live config contract | `internal/core/config_test.go` | L1 |

### R-PIPE: Pipeline Stages (v1.0.4)

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-PIPE-01 | Pipeline `Run()` orchestrator delegates to 16 named stage methods | `internal/pipeline/pipeline_test.go` | L1/L2 |
| R-PIPE-02 | All 8 pipeline trace annotations are wired into stage methods | `internal/pipeline/trace_test.go` | L1 |
| R-PIPE-03 | `agentSkills` populated from `SkillMatcher.ListEnabled()`, not empty | `internal/pipeline/pipeline_test.go` | L1 |
| R-PIPE-04 | Cache rejects responses containing `"tool_call"` or `"function_call"` | `internal/pipeline/pipeline_cache_test.go` | L1 |
| R-PIPE-05 | Cache rejects parroting responses (>60% text overlap) | `internal/pipeline/pipeline_cache_test.go` | L1 |
| R-PIPE-06 | `FinancialActionTruthGuard` verifies financial claims against tool output | `internal/pipeline/guards_financial_truth_test.go` | L1/L2 |
| R-PIPE-07 | Pipeline cache honors TTL on reads and stamps explicit `created_at` / `expires_at` metadata on writes instead of creating timeless rows | `internal/pipeline/behavioral_fitness_test.go::TestFitness_CacheRejectsExpiredEntries` | L1/L2 |
| R-PIPE-08 | Pipeline cache fingerprint includes the shaped session scaffold, so the same user text does not replay across materially different memory/system/tool context | `internal/pipeline/behavioral_fitness_test.go::TestFitness_PipelineCacheKeyIncludesSessionScaffold` | L1/L2 |
| R-PIPE-09 | Pipeline-level `NoEscalate` turns bypass semantic cache replay and store just like lower-level LLM no-escalate paths | `internal/pipeline/behavioral_fitness_test.go::TestFitness_NoEscalateBypassesPipelineCache` | L1/L2 |
| R-PIPE-10 | Pre-inference compaction mutates the live session artifact before inference instead of only logging a smaller hypothetical slice | `internal/pipeline/prepare_inference_test.go::TestPrepareForInference_CompactsSessionMessagesInPlace`, `internal/pipeline/prepare_inference_test.go::TestRunStandardInference_CompactsSessionMessagesInPlace` | L1/L2 |
| R-PIPE-11 | Verbose single-step authoring requests stay `simple` and `execute_directly` instead of being upcast on word count alone, so the focused execution envelope remains reachable for note/document/file creation turns | `internal/pipeline/task_synthesis_test.go::TestSynthesizeTaskState_VerboseSingleStepAuthoringStaysSimple`, `internal/pipeline/turn_policy_test.go::TestDeriveTurnEnvelopePolicy_SimpleDirectTaskUsesFocusedEnvelope` | L1/L2 |

### R-SEC: Security Hardening (v1.0.4)

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-SEC-01 | `Store.DB()` does not exist — no raw `*sql.DB` access | `internal/api/architecture_test.go` | L0 |
| R-SEC-02 | Wallet passphrase resolved from keystore only — no env var fallback | `internal/wallet/wallet_test.go` | L1 |
| R-SEC-03 | Delivery queue `in_flight` rows recovered to `pending` on startup | `internal/daemon/daemon_test.go` | L1/L2 |
| R-SEC-04 | OAuth shutdown uses parent ctx, not `context.Background()` | `internal/core/oauth_test.go` | L1 |

### R-ESC: Session Escalation And Compression (v1.0.4)

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-ESC-01 | Session escalation triggers on 2+ consecutive failures | `internal/llm/session_escalation_test.go` | L1 |
| R-ESC-02 | Session escalation triggers on quality < 0.3 for 3+ turns | `internal/llm/session_escalation_test.go` | L1 |
| R-ESC-03 | Topic-aware compression preserves current topic, compresses off-topic | `internal/llm/compression_test.go` | L1 |
| R-ESC-04 | `EstimateTokens()` uses UTF-8 rune count, not `len/4` | `internal/llm/tokencount_test.go` | L1 |

### R-SOAK: Behavior Soak Tests (v1.0.4)

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-SOAK-01 | Soak test default timeout is 1800s (30 min), not 240s | `scripts/run-agent-behavior-soak.py` | L4 |
| R-SOAK-02 | Per-scenario `max_latency_s` override works for heavy scenarios | `scripts/run-agent-behavior-soak.py` | L4 |
| R-SOAK-03 | Managed live behavior soak supports `external`, `clone`, and `fresh` modes so copied-state and clean-state lanes can both be exercised without touching the operator's live config or database | `scripts/run-agent-behavior-soak.py` (audit) | A |
| R-SOAK-04 | Prompt compression quality is evaluated as a paired live soak (`off` vs `on`) on isolated configs, with pass→fail drift treated as a release-blocking regression | `scripts/run-prompt-compression-soak.py`, `scripts/prompt_compression_soak_test.go` | L1/L4 |
| R-SOAK-05 | Paired prompt-compression lanes fail decisively with structured `harness_error` output when an underlying lane times out before producing a report, instead of hanging or surfacing a secondary missing-file crash | `scripts/run-prompt-compression-soak.py`, `scripts/prompt_compression_soak_test.go` | L1/L4 |
| R-SOAK-06 | Paired prompt-compression lanes use isolated per-lane base URLs and wait for managed-server port teardown between lanes, so one lane cannot poison the next through port reuse | `scripts/run-prompt-compression-soak.py`, `scripts/prompt_compression_soak_test.go` | L1/L4 |
| R-SOAK-07 | Prompt-compression comparison defaults to a targeted, history-bearing quality subset instead of the full behavioral matrix, while still allowing explicit scenario override via `SOAK_SCENARIOS` | `scripts/run-prompt-compression-soak.py`, `scripts/run-agent-behavior-soak.py`, `scripts/prompt_compression_soak_test.go` | L1/L4 |
| R-SOAK-08 | Clone/fresh behavior-soak teardown owns the entire managed-server process group and closes its log handle, so `go run . serve` cannot leave a detached child listening on the lane port after the harness exits | `scripts/run-agent-behavior-soak.py`, `scripts/run-prompt-compression-soak.py` | L1/L4 |

### R-CMD: CLI Subpackages (v1.0.4)

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-CMD-01 | 12 cmd subpackages register all commands via `Commands()` | `cmd/*/commands_test.go` | L1 |
| R-CMD-02 | Zero behavioral change — all CLI commands keep exact names and flags | CLI smoke | L3 |

### R-UX: Dashboard, TUI, CLI, Docs

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-UX-01 | Dashboard critical APIs remain functional against a live runtime | smoke + route tests | L2/L3 |
| R-UX-02 | Markdown/rendering safety remains enforced | fuzz/integration tests | L2/L4 |
| R-UX-03 | CLI operator-critical flows remain functional against a live runtime | CLI smoke | L3 |
| R-UX-04 | CLI commands must not be placeholders | CLI unit/integration tests | L1/L2 |
| R-UX-05 | If TUI parity is claimed, dashboard-to-TUI feature mapping stays current | TUI/UI parity tests | L2/L3 |
| R-UX-06 | `roboticus update all` and `roboticus upgrade all` preserve the historical operator upgrade path | CLI/update integration tests + release smoke | L2/L3/L4 |
| R-UX-07 | Dashboard settings copy must mark prompt compression as benchmark-only and not recommended for live use | `internal/api/dashboard_test.go` | L1/L2 |

### R-REL: Release Confidence

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-REL-01 | Live smoke must cover every advertised subsystem | `smoke_test.go` | L3 |
| R-REL-02 | Parity audit must report no remaining required gaps versus frozen Roboticus baseline | parity-audit + tests | L3/L4 |
| R-REL-03 | Feature-complete checklist and docs stay aligned with shipped behavior | doc/release review gate | L4 |
| R-REL-04 | Release artifacts and `SHA256SUMS.txt` are complete and installer-compatible | release gate + artifact validation tests | L4 |
| R-REL-05 | `roboticus.ai` sync succeeds from the Go release source and publishes matching metadata | site-sync dry run + deploy gate | L4 |
| R-REL-06 | Public installer scripts install the Go-based runtime without changing the operator contract unexpectedly | installer smoke on Unix + Windows | L3/L4 |
| R-REL-07 | Release-shaped CI/release/local helper builds stamp the CLI version into `cmd/internal/cmdutil.Version` and the daemon banner into `internal/daemon.version`, so built binaries do not report `dev` after release packaging | static build-contract tests + release gate | L2/L4 |
| R-REL-08 | CI/release workflows use Node 24-capable GitHub Action majors, so release-branch jobs do not carry known Node 20 deprecation warnings | workflow review gate | L4 |

### R-AGENT: Agentic Retrieval Architecture (v1.0.5)

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-AGENT-01 | Router produces different plans for different intent signals | `internal/agent/memory/router_test.go` | L1 |
| R-AGENT-02 | Router never targets working memory (active state, not searched) | `internal/agent/memory/router_test.go` | L1 |
| R-AGENT-03 | Router tier budgets sum to ~1.0 for all routing plans | `internal/agent/memory/router_test.go` | L1 |
| R-AGENT-04 | Reranker discards evidence below MinScore threshold | `internal/agent/memory/reranker_test.go` | L1 |
| R-AGENT-05 | Reranker authority boost promotes canonical sources | `internal/agent/memory/reranker_test.go` | L1 |
| R-AGENT-06 | Reranker collapse detection caps results when spread < 0.05 | `internal/agent/memory/reranker_test.go` | L1 |
| R-AGENT-07 | Decomposer splits compound queries (multiple ?'s, semicolons, conjunctions) | `internal/agent/memory/decomposer_test.go` | L1 |
| R-AGENT-08 | Decomposer classifies subgoals to correct memory tiers | `internal/agent/memory/decomposer_test.go` | L1 |
| R-AGENT-09 | Context assembly produces [Working State], [Evidence], [Gaps], [Contradictions] | `internal/agent/memory/context_assembly_test.go` | L1 |
| R-AGENT-10 | Context assembly detects gaps when tiers return no results | `internal/agent/memory/context_assembly_test.go` | L1 |
| R-AGENT-11 | Reflection generates structured episode summaries with outcome classification | `internal/agent/memory/reflection_test.go` | L1 |
| R-AGENT-12 | Reflection detects retry patterns and all-fail scenarios | `internal/agent/memory/reflection_test.go` | L1 |
| R-AGENT-13 | Working memory persisted on shutdown, vetted on startup | `internal/agent/memory/working_persistence_test.go` | L1 |
| R-AGENT-14 | Startup vet retains goals/decisions, discards stale/low-importance entries | `internal/agent/memory/working_persistence_test.go` | L1 |
| R-AGENT-15 | BM25 scoring in HybridSearch varies by term relevance | `internal/db/hybrid_search_test.go` | L1 |
| R-AGENT-16 | HybridSearch deduplicates across FTS and vector legs | `internal/db/hybrid_search_test.go` | L1 |
| R-AGENT-17 | Adaptive hybrid weight decreases monotonically with corpus size | `internal/agent/memory/adaptive_weight_test.go` | L1 |
| R-AGENT-18 | Partitioned index routes entries to correct partition by source table | `internal/db/vector_partitioned_test.go` | L1 |
| R-AGENT-19 | Collapse regression: ScoreSpread and adaptive weight match expectations at 100/1K scale | `internal/agent/memory/collapse_regression_test.go` | L1 |
| R-AGENT-20 | Post-turn procedure detection persists learned skills from tool sequences | `internal/pipeline/post_turn.go` | L2 |
| R-AGENT-21 | Post-turn reflection stores episode summaries as episodic_memory | `internal/pipeline/post_turn.go` | L2 |
| R-AGENT-22 | Semantic evidence preserves source identity, canonical flag, and authority metadata | `internal/agent/memory/retrieval_test.go` | L1 |
| R-AGENT-23 | Context assembly prints evidence provenance/authority instead of flattening all sources | `internal/agent/memory/context_assembly_test.go` | L1 |
| R-AGENT-24 | Verifier retries when responses ignore explicit evidence gaps or contradictions | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-25 | Standard pipeline path revises output when verifier rejects unsupported certainty | `internal/pipeline/pipeline_run_test.go` | L2 |
| R-AGENT-26 | Verifier prefers pipeline-computed task hints over prompt-only reconstruction | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-27 | Standard pipeline path revises output when remediation/next-step coverage is missing | `internal/pipeline/pipeline_run_test.go` | L2 |
| R-AGENT-28 | Relationship retrieval preserves source identity, dependency summary, and evidence age | `internal/agent/memory/retrieval_test.go` | L1 |
| R-AGENT-29 | Context assembly surfaces freshness risks for stale evidence | `internal/agent/memory/context_assembly_test.go` | L1 |
| R-AGENT-30 | Verifier rejects overconfident “latest/current” answers when evidence is stale | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-31 | Semantic ingestion extracts typed graph facts into persisted `knowledge_facts` rows | `internal/agent/memory/manager_test.go` | L1 |
| R-AGENT-32 | Relationship-tier retrieval can surface persisted graph facts with provenance | `internal/agent/memory/retrieval_test.go` | L1 |
| R-AGENT-33 | `recall_memory` can fetch `knowledge_facts` rows directly | `internal/agent/tools/memory_recall_test.go` | L1 |
| R-AGENT-34 | `search_memories` can find persisted graph facts | `internal/agent/tools/memory_search_test.go` | L1 |
| R-AGENT-35 | Graph retrieval can synthesize explicit path evidence between named entities | `internal/agent/memory/retrieval_test.go` | L1 |
| R-AGENT-36 | Graph retrieval can synthesize reverse dependency impact chains for blast-radius queries | `internal/agent/memory/retrieval_test.go` | L1 |
| R-AGENT-37 | Verifier extracts structured retrieved-evidence items from assembled memory context | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-38 | Verifier rejects answered subgoals that lack supporting retrieved evidence | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-39 | Standard pipeline path revises output when verifier detects unsupported answered-subgoal evidence | `internal/pipeline/pipeline_run_test.go` | L2 |
| R-AGENT-40 | Verifier extracts structured claims from responses and classifies certainty | `internal/pipeline/verifier_claims_test.go` | L1 |
| R-AGENT-41 | Verifier rejects weak provenance coverage when absolute claims outnumber evidence-supported claims on high-risk queries | `internal/pipeline/verifier_claims_test.go` | L1 |
| R-AGENT-42 | Verifier rejects unresolved contradicted claims when the response states absolutes on contested evidence without reconciliation | `internal/pipeline/verifier_claims_test.go` | L1 |
| R-AGENT-43 | Verifier rejects unsupported absolute claims on high-risk queries that lack evidence support and canonical anchors | `internal/pipeline/verifier_claims_test.go` | L1 |
| R-AGENT-44 | Working memory carries structured executive state (plan, assumptions, unresolved questions, verified conclusions, decision checkpoints, stopping criteria) | `internal/agent/memory/executive_test.go` | L1 |
| R-AGENT-45 | Executive state survives shutdown/startup vetting while transient turn summaries and notes are discarded | `internal/agent/memory/executive_test.go` | L1 |
| R-AGENT-46 | Executive-state entries honor a longer max-age cutoff than transient working memory entries | `internal/agent/memory/executive_test.go` | L1 |
| R-AGENT-47 | Context assembly surfaces executive state (plan, assumptions, unresolved questions, stopping criteria) in the Working State section | `internal/agent/memory/context_assembly_test.go` | L1 |
| R-AGENT-48 | Verifier parses executive-state sections out of the memory context and extracts unresolved questions and stopping criteria | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-49 | Verifier rejects responses that abandon unresolved questions while answering a related prompt | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-50 | Verifier rejects "task complete" claims that do not address the active stopping criteria | `internal/pipeline/verifier_test.go` | L1 |
| R-AGENT-51 | Post-turn growth records verified conclusions for covered + evidence-supported subgoals | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-52 | Post-turn growth opens unresolved questions for subgoals the turn could not close | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-53 | Post-turn growth resolves prior unresolved questions once the response answers them | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-54 | Post-turn growth does not auto-resolve open questions when the response is explicitly uncertain | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-55 | Post-turn growth is idempotent across repeated runs — no duplicate verified conclusions | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-56 | Verifier emits per-claim `ClaimAudit` records (certainty, supported, anchored, reconciled, issue code) | `internal/pipeline/verifier_trace_test.go` | L1 |
| R-AGENT-57 | `SummarizeVerification` produces claim count / absolute count / coverage ratio / flagged count | `internal/pipeline/verifier_trace_test.go` | L1 |
| R-AGENT-58 | Pipeline trace carries a `verifier.*` annotation group including a JSON claim map | `internal/pipeline/verifier_trace_test.go` | L1 |
| R-AGENT-59 | Multi-step task resumes across a simulated shutdown/startup cycle with plan, unresolved question, stopping criterion, and assumption intact | `internal/agent/memory/executive_restart_test.go` | L1 |
| R-AGENT-60 | Restart vet keeps executive and goal entries while discarding transient turn summaries and notes | `internal/agent/memory/executive_restart_test.go` | L1 |
| R-AGENT-61 | Verifier rejects unanchored absolute claims on financial/compliance/security queries (`proof_obligation_unmet`) | `internal/pipeline/verifier_claims_test.go` | L1 |
| R-AGENT-62 | Verifier accepts absolute claims whose supporting evidence carries a canonical marker, even without explicit in-response attribution | `internal/pipeline/verifier_claims_test.go` | L1 |
| R-AGENT-63 | Per-intent proof obligation does not fire on low-risk intents | `internal/pipeline/verifier_claims_test.go` | L1 |
| R-AGENT-64 | Plan subgoal diff is case-insensitive and whitespace-normalized | `internal/pipeline/plan_checkpoint_test.go` | L1 |
| R-AGENT-65 | Task synthesis records a decision checkpoint when subgoals change vs. the prior plan and skips the checkpoint when subgoals are identical | `internal/pipeline/plan_checkpoint_test.go` | L1 |
| R-AGENT-66 | Pipeline trace carries an `executive.*` annotation group on plan write with subgoals, added/removed diff, and checkpoint flag | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-67 | Executive plan trace omits checkpoint annotation when subgoals are unchanged | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-68 | `growExecutiveState` returns structured counts (verified, questions opened, questions resolved, assumptions) suitable for telemetry | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-69 | `extractAssumptions` picks up explicit assumption markers in the response and returns each clause | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-70 | `extractAssumptions` is word-boundary aware — no false positives on words containing an assumption marker | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-71 | `extractAssumptions` deduplicates equivalent clauses within a single turn | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-72 | Post-turn growth persists assumption entries extracted from the response into working memory under the active task | `internal/pipeline/executive_growth_test.go` | L1 |
| R-AGENT-73 | `KnowledgeGraph` reports accurate node/edge counts and only indexes traversable relations | `internal/agent/memory/graph_test.go` | L1 |
| R-AGENT-74 | `KnowledgeGraph.ShortestPath` finds multi-hop paths within the max-depth bound and returns nil for missing paths or over-depth queries | `internal/agent/memory/graph_test.go` | L1 |
| R-AGENT-75 | `KnowledgeGraph.Impact` and `Dependencies` return multi-hop reverse/forward traversals with correct depth bounding | `internal/agent/memory/graph_test.go` | L1 |
| R-AGENT-76 | `LoadKnowledgeGraph` reads every persisted `knowledge_facts` row; `LoadKnowledgeGraphWithLimit` honors the limit | `internal/agent/memory/graph_test.go` | L1 |
| R-AGENT-77 | `query_knowledge_graph` agent tool returns multi-hop path evidence and "no path" messages within the max-depth bound | `internal/agent/tools/graph_query_test.go` | L1 |
| R-AGENT-78 | `query_knowledge_graph` impact and dependencies operations walk reverse / forward adjacency and return node lists with min depth | `internal/agent/tools/graph_query_test.go` | L1 |
| R-AGENT-79 | `query_knowledge_graph` clamps max_depth and rejects unknown operations / missing required fields | `internal/agent/tools/graph_query_test.go` | L1 |
| R-AGENT-80 | `query_knowledge_graph` publishes a valid JSON parameter schema and returns a friendly message when the store is nil | `internal/agent/tools/graph_query_test.go` | L1 |
| R-AGENT-81 | Workflow-memory schema carries confidence / memory_state / version / category / success+failure evidence columns, and the consolidation confidence-sync query runs without silent skip | `internal/agent/memory/workflow_test.go` | L1 |
| R-AGENT-82 | `RecordWorkflow` persists full metadata and updates bump version while preserving success/failure counters | `internal/agent/memory/workflow_test.go` | L1 |
| R-AGENT-83 | `RecordWorkflowSuccess` appends evidence uniquely and increments the success counter | `internal/agent/memory/workflow_test.go` | L1 |
| R-AGENT-84 | `FindWorkflows` is query-sensitive across name / steps / preconditions / error_modes / context_tags | `internal/agent/memory/workflow_test.go` | L1 |
| R-AGENT-85 | Procedural retrieval surfaces workflows before tool-stat rollups and falls back to tool stats when no workflow matches | `internal/agent/memory/workflow_test.go` | L1 |
| R-AGENT-86 | `AnalyzeEpisode` carries evidence refs and verifier outcome into the episode summary with high result quality when tools and verifier all pass | `internal/agent/memory/reflection_episode_test.go` | L1 |
| R-AGENT-87 | Enriched reflection detects fail→success fix patterns and extracts failed hypotheses from self-corrections in the answer | `internal/agent/memory/reflection_episode_test.go` | L1 |
| R-AGENT-88 | Enriched reflection captures tool error messages, deduplicated, and produces low result quality when tools and verifier fail | `internal/agent/memory/reflection_episode_test.go` | L1 |
| R-AGENT-89 | `FormatForStorage` includes enriched fields (FixPatterns, EvidenceRefs, FailedHypotheses, Errors, Quality label) | `internal/agent/memory/reflection_episode_test.go` | L1 |
| R-AGENT-90 | `parseEpisodeSummary` round-trips enriched fields (outcome, fix patterns, evidence refs, quality) back out of the storage format | `internal/agent/memory/consolidation_distillation_test.go` | L1 |
| R-AGENT-91 | `phaseEpisodeDistillation` promotes fix patterns seen in 2+ successful episodes into `semantic_memory` under `fix_pattern` and is idempotent across re-runs | `internal/agent/memory/consolidation_distillation_test.go` | L1 |
| R-AGENT-92 | `phaseEpisodeDistillation` promotes evidence references seen in 3+ successful episodes into `semantic_memory` under `learned_fact` | `internal/agent/memory/consolidation_distillation_test.go` | L1 |
| R-AGENT-93 | `phaseEpisodeDistillation` ignores evidence below the support threshold and skips failure-outcome episodes | `internal/agent/memory/consolidation_distillation_test.go` | L1 |
| R-AGENT-94 | Workflow promotion extracts the first error line per failing step into `error_modes`, deduplicated and prefixed with the tool name | `internal/pipeline/workflow_promotion_test.go` | L1 |
| R-AGENT-95 | Workflow promotion seeds `preconditions` from the session's task intent, complexity, and subgoals | `internal/pipeline/workflow_promotion_test.go` | L1 |
| R-AGENT-96 | Workflow promotion tags the record with `auto_promoted` and an `intent:*` context tag derived from task state | `internal/pipeline/workflow_promotion_test.go` | L1 |
| R-AGENT-97 | `BuildPerception` classifies financial/production queries as high-risk and forces semantic + relationship tiers | `internal/pipeline/perception_test.go` | L1 |
| R-AGENT-98 | `BuildPerception` resolves policy queries to semantic source-of-truth, procedural "how to" to procedural, dependency queries to relationship, and current-state to external | `internal/pipeline/perception_test.go` | L1 |
| R-AGENT-99 | `BuildPerception` is deterministic and skips retrieval for conversational turns | `internal/pipeline/perception_test.go` | L1 |
| R-AGENT-100 | Pipeline trace carries a full `perception.*` annotation group (intent, risk, source-of-truth, required tiers, decomposition, freshness, confidence) | `internal/pipeline/perception_test.go` | L1 |
| R-AGENT-101 | Semantic upsert bumps `version` when a key's value changes and leaves it stable on idempotent rewrites | `internal/agent/memory/semantic_supersession_test.go` | L1 |
| R-AGENT-102 | `CurrentSemanticValue` walks multi-hop `superseded_by` chains and reaches the active revision with correct depth | `internal/agent/memory/semantic_supersession_test.go` | L1 |
| R-AGENT-103 | `CurrentSemanticValue` handles supersession cycles by returning `ErrSemanticChainCycle` with the partial revision | `internal/agent/memory/semantic_supersession_test.go` | L1 |
| R-AGENT-104 | `MarkSemanticSuperseded` flips an entry to stale, sets the pointer, and rejects inactive replacements | `internal/agent/memory/semantic_supersession_test.go` | L1 |
| R-AGENT-105 | Consolidation contradiction phase populates `superseded_by` on newly stale semantic rows | `internal/agent/memory/semantic_supersession_test.go` | L1 |
| R-AGENT-106 | `BuildRetrievalArtifact` hashes memory context + memory index deterministically and distinguishes different inputs | `internal/pipeline/retrieval_parity_test.go` | L1 |
| R-AGENT-107 | Standard and streaming sessions with identical memory state compute identical artifact hashes | `internal/pipeline/retrieval_parity_test.go` | L1 |
| R-AGENT-108 | Parity fitness detects silent memory-context drift between standard and streaming paths | `internal/pipeline/retrieval_parity_test.go` | L1 |
| R-AGENT-109 | `retrieval.*` trace namespace carries artifact_hash, per-field hashes, byte counts, and bounded previews | `internal/pipeline/retrieval_parity_test.go` | L1 |
| R-AGENT-110 | `rankWorkflowMatches` blends Laplace-smoothed success rate, failure penalty, query-token overlap, tag fit, recency decay, and confidence into a single score | `internal/agent/tools/workflow_search_test.go` | L1 |
| R-AGENT-111 | Ranker prefers larger sample sizes with identical apparent success rate (Laplace smoothing) and penalises failure counts | `internal/agent/tools/workflow_search_test.go` | L1 |
| R-AGENT-112 | Ranker drops candidates below the ranking floor so the tool does not surface untrusted workflows | `internal/agent/tools/workflow_search_test.go` | L1 |
| R-AGENT-113 | `find_workflow` tool returns ranked matches for `find`, fetches by exact name for `get`, and rejects unknown operations | `internal/agent/tools/workflow_search_test.go` | L1 |
| R-AGENT-114 | `find_workflow` multi-word queries match hyphenated workflow names via longest-token SQL prefilter + in-memory multi-token ranker | `internal/agent/tools/workflow_search_test.go` | L1 |
| R-AGENT-115 | Path retrieval ignores non-canonical relations (no permissive fallback) and still traverses canonical edges | `internal/agent/memory/graph_canonical_test.go` | L1 |
| R-AGENT-116 | Extractor patterns and `db.CanonicalGraphRelations` stay in sync — new relations added to one side must land on the other | `internal/agent/memory/graph_canonical_test.go` | L1 |
| R-AGENT-117 | `IsTraversableRelation` delegates to `db.IsCanonicalGraphRelation` as the single source of truth | `internal/agent/memory/graph_canonical_test.go` | L1 |
| R-AGENT-118 | `MemoryRepository.StoreKnowledgeFact` rejects non-canonical relations at write time | `internal/agent/memory/graph_canonical_test.go` | L1 |
| R-AGENT-119 | `ExtractToolFacts` harvests `recall_memory` semantic + knowledge-fact payloads with inherited confidence capped at 0.9 | `internal/pipeline/tool_facts_test.go` | L1 |
| R-AGENT-120 | `ExtractToolFacts` harvests `search_memories` results at 0.65 inventory confidence and `read_file` narrow `key: value` pairs at 0.75 | `internal/pipeline/tool_facts_test.go` | L1 |
| R-AGENT-121 | `ExtractToolFacts` harvests `query_knowledge_graph` hops at 0.75 and skips giant blobs / failure outputs / non-allowlisted tools | `internal/pipeline/tool_facts_test.go` | L1 |
| R-AGENT-122 | `ExtractToolFacts` harvests `find_workflow` `find` results at 0.65 inventory and `get` results with inherited workflow confidence | `internal/pipeline/tool_facts_test.go` | L1 |
| R-AGENT-123 | `FilterFactsReferencedByResponse` keeps only facts whose keywords appear in the final response, and requires 2-of-N matches for rich facts | `internal/pipeline/tool_facts_test.go` | L1 |
| R-AGENT-124 | Post-turn growth records referenced tool facts as assumptions with their per-source confidence, and skips tool facts the response did not reference | `internal/pipeline/tool_facts_test.go` | L1 |
| R-AGENT-125 | `NewClaimCertaintyClassifier` pre-embeds the curated adversarial corpus and returns a working classifier with no embedder configured | `internal/pipeline/verifier_classifier_test.go` | L1 |
| R-AGENT-126 | Semantic certainty classifier upgrades a paraphrased moderate-tagged claim and leaves already-tagged lexical claims untouched | `internal/pipeline/verifier_classifier_test.go` | L1 |
| R-AGENT-127 | Verifier with classifier flags paraphrased absolute claims (no lexical marker) under per-intent proof obligation; without classifier the same response stays moderate and passes | `internal/pipeline/verifier_classifier_test.go` | L1 |
| R-AGENT-128 | Curated certainty corpus covers absolute / high / hedged with at least 5 examples per category | `internal/pipeline/verifier_classifier_test.go` | L1 |
| R-AGENT-129 | `IngestPolicyDocument` rejects missing core fields (category / key / content / source_label) | `internal/agent/memory/policy_ingestion_test.go` | L1 |
| R-AGENT-130 | `IngestPolicyDocument` defaults `effective_date` to NULL and parses caller-supplied dates without substituting ingestion time | `internal/agent/memory/policy_ingestion_test.go` | L1 |
| R-AGENT-131 | `IngestPolicyDocument` enforces canonical guardrails: requires `asserter_id` AND (version OR effective_date); rejects asserters in `DisallowedAsserterIDs` | `internal/agent/memory/policy_ingestion_test.go` | L1 |
| R-AGENT-132 | `IngestPolicyDocument` rejects silent overwrites; allows replacement via explicit flag, strictly-higher version, or canonical-promotion | `internal/agent/memory/policy_ingestion_test.go` | L1 |
| R-AGENT-133 | Replacement marks prior row stale with `superseded_by` and the Milestone 3 chain-walker resolves from the prior id to the new row | `internal/agent/memory/policy_ingestion_test.go` | L1 |
| R-AGENT-134 | Semantic retrieval uses persisted `is_canonical` and `source_label` columns; rows without explicit canonical assertion no longer surface as canonical even when category contains "policy" | `internal/agent/memory/retrieval_test.go` | L1 |
| R-AGENT-135 | `ingest_policy` agent tool round-trips with explicit provenance, blocks self-asserter for canonical, rejects silent overwrites, and exposes RiskDangerous | `internal/agent/tools/policy_ingest_test.go` | L1 |
| R-AGENT-136 | M3.1 — every FTS-covered tier (`episodic_memory`, `semantic_memory`, `procedural_memory`, `relationship_memory`) keeps `memory_fts` synchronized across INSERT, UPDATE, and DELETE; future migrations cannot silently regress this contract | `internal/db/fts_trigger_completeness_test.go` | L1 |
| R-AGENT-137 | M3.1 — migration 048's `memory_fts` backfill is idempotent on already-current data (re-running the SQL produces zero new rows) | `internal/db/fts_trigger_completeness_test.go` | L1 |
| R-AGENT-138 | M3.2 / PAR-014 — semantic retrieval emits `retrieval.path.semantic` annotation on the clean path: `fts` for value matches, `fts` for key-driven semantic lookups via the enriched semantic FTS corpus, `empty` for unmatchable queries, and no annotation in non-search browse modes | `internal/agent/memory/retrieval_path_test.go` | L1 |
| R-AGENT-139 | M3.2 — procedural retrieval emits `retrieval.path.procedural` and exercises HybridSearch primary path; `deploy_cli`-style FTS-tokenisable queries surface via the FTS leg without falling through to LIKE | `internal/agent/memory/retrieval_path_test.go` | L1 |
| R-AGENT-140 | M3.2 — relationship retrieval emits `retrieval.path.relationship` and uses HybridSearch primary; the `relationship_memory` rows added by migration 048's INSERT/UPDATE triggers are surfaced via FTS | `internal/agent/memory/retrieval_path_test.go` | L1 |
| R-AGENT-141 | M3.2 — workflow retrieval emits `retrieval.path.workflow` and `findWorkflowsHybrid` returns workflows for query lexically matching the workflow name/tags via the FTS leg | `internal/agent/memory/retrieval_path_test.go` | L1 |
| R-AGENT-142 | M3.2 — LIKE safety net is exercised AND annotated as `like_fallback` (or matched via `fts`/`hybrid`) when the FTS leg can't tokenise the query directly; never silently falls through to `empty` while a matching workflow row exists | `internal/agent/memory/retrieval_path_test.go` | L1 |
| R-AGENT-143 | M3.2 — `classifyHybridPath` is total over (ftsHits, vectorHits): both → `hybrid`, fts-only → `fts`, vector-only → `vector`, neither → empty string (signals caller to engage LIKE fallback) | `internal/agent/memory/retrieval_path_test.go` | L1 |
| R-AGENT-144 | M3.2 — retrieval tier methods are safe to call without a tracer in context: results are identical whether `WithRetrievalTracer` was applied or not, only the annotation side-channel changes | `internal/agent/memory/retrieval_path_test.go` | L1 |
| R-AGENT-145 | M8 — `EpisodeSummary.Relations` round-trip through `FormatForStorage` ↔ `parseEpisodeSummary` preserves every extracted (subject, relation, object) triple | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-146 | M8 — recurring (≥`MinRelationDistillSupport`) high-quality canonical relations are promoted into `knowledge_facts` with `source_table='episodic_distillation'` and confidence ≤ `distilledRelationConfidenceCap` | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-147 | M8 — relations observed in fewer than `MinRelationDistillSupport` episodes are NOT promoted (anecdote-hijacking guard) | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-148 | M8 — failed / low-quality episodes do not drive relational promotion even when they recur many times | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-149 | M8 — relational promotion is idempotent across repeated consolidation runs (UPSERT in place via stable `distill_…` fact id) | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-150 | M8 — promoted relations are read by `KnowledgeGraph` as normal traversable edges; distillation source is invisible to graph reads | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-151 | M8 — non-canonical relations in episode summaries are blocked at the canonical write gate; `phaseEpisodeDistillation` filters them and `StoreKnowledgeFact` rejects them as defense-in-depth | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-152 | M8 — `parseRelationsList` drops malformed segments (wrong separator count, empty parts) without producing phantom triples | `internal/agent/memory/m8_relational_distillation_test.go` | L1 |
| R-AGENT-153 | M3.3 — `AggregateRetrievalPaths` flags a tier as `IsDormant=true` only when both the LIKE-fallback share is at or below `RetrievalPathRetirementThreshold` AND the total observation count clears `minSampleForDormancy` | `internal/agent/memory/retrieval_path_telemetry_test.go` | L1 |
| R-AGENT-154 | M3.3 — a tier with fallback share above the retirement threshold is NOT dormant, even with thousands of observations | `internal/agent/memory/retrieval_path_telemetry_test.go` | L1 |
| R-AGENT-155 | M3.3 — a tier observed below the sample minimum is NOT dormant even if every observation was on the FTS path (small-sample guard) | `internal/agent/memory/retrieval_path_telemetry_test.go` | L1 |
| R-AGENT-156 | M3.3 — multiple `retrieval.path.<tier>` annotations within the same trace span are tallied independently across tiers | `internal/agent/memory/retrieval_path_telemetry_test.go` | L1 |
| R-AGENT-157 | M3.3 — `RetrievalPathDistribution.SortedTiers` returns deterministic alphabetical ordering for stable dashboard / report output | `internal/agent/memory/retrieval_path_telemetry_test.go` | L1 |
| R-AGENT-158 | Guard-triggered standard-inference retry rebuilds `GuardContext` from the post-retry session state, so contextual guards see retry-added tool results/messages instead of the stale pre-retry snapshot | `internal/pipeline/guard_retry_artifacts_test.go` | L1/L2 |
| R-AGENT-159 | `InferenceParams.GuardViolations` and `GuardRetried` come from the actual final applied guard result, not a clean re-run over already-rewritten content | `internal/pipeline/guard_retry_artifacts_test.go` | L1/L2 |
| R-AGENT-160 | Early-return `guardOutcome(...)` paths (skill / shortcut exits) apply contextual guards with a live `GuardContext` when a session is available, instead of silently degrading to text-only checks | `internal/pipeline/guard_retry_artifacts_test.go` | L1/L2 |
| R-AGENT-161 | `buildGuardContext(...)` carries live pipeline hints and store-backed facts into contextual guards: task intent, delegation intent, enabled subagent names, inferred delegation provenance, and latest selected model | `internal/pipeline/guard_context_population_test.go` | L1/L2 |
| R-AGENT-162 | `ApplyFullWithContext(...)` runs guard-score precompute on the live path, so contextual guards see inferred intents plus `SemanticScores` such as `identity_claim` and `prev_overlap` without a separate caller-side precompute step | `internal/pipeline/guard_context_population_test.go` | L1/L2 |
| R-AGENT-163 | Live periodic checkpoint writes go through `CheckpointRepository.SaveRecord(...)` and persist the full repository-owned row shape (`system_prompt_hash`, `memory_summary`, `conversation_digest`, `turn_count`) instead of a second raw-SQL writer | `internal/pipeline/checkpoint_lifecycle_test.go`, `internal/db/coverage_boost_test.go` | L1/L2 |
| R-AGENT-164 | `CheckpointRepository.LoadLatestRecord(...)` returns the latest full checkpoint row with stable same-timestamp ordering (`created_at DESC, rowid DESC`) instead of exposing only `memory_summary` | `internal/db/coverage_boost_test.go` | L1 |
| R-AGENT-165 | Live checkpoint lifecycle prunes old checkpoint rows after repository-owned saves and restores a compact `[Checkpoint Digest]` ambient note into the final request through `buildAgentContext(...)` | `internal/pipeline/checkpoint_lifecycle_test.go`, `internal/daemon/daemon_adapters_test.go` | L1/L2 |
| R-AGENT-166 | Post-turn reflection reads persisted `tool_calls` and `pipeline_traces.total_ms` for the current turn, so episode summaries capture real tool actions, failures, and duration instead of message-adjacency inference plus a zero-duration TODO proxy | `internal/pipeline/post_turn_test.go` | L1/L2 |
| R-AGENT-167 | Live checkpoint lifecycle honors pipeline-owned checkpoint policy: disabled means no checkpoint rows are written, and a configured interval controls when writes occur | `internal/pipeline/checkpoint_lifecycle_test.go` | L1/L2 |
| R-AGENT-168 | Reflection carries structured inference artifacts into stored `episode_summary` output: selected model, react turn count, final guard violations, and guard-retry marker come from persisted turn metadata rather than being lost after inference | `internal/agent/memory/reflection_episode_test.go`, `internal/pipeline/post_turn_test.go` | L1/L2 |
| R-AGENT-169 | Consolidation distillation consumes recurring `Learnings` from stored `episode_summary` artifacts, so repeated structured turn lessons (including guard/retry and ReAct-derived learnings) promote into `semantic_memory` as `episode_learning` instead of dying in episodic storage | `internal/agent/memory/consolidation_distillation_test.go` | L1/L2 |
| R-AGENT-170 | Post-turn reflection runs after executive-state growth and records the resulting continuity delta (`ExecutiveVerified`, `ExecutiveQuestionsOpened`, `ExecutiveQuestionsResolved`, `ExecutiveAssumptions`) in stored `episode_summary` artifacts instead of leaving that state change implicit in separate stores | `internal/agent/memory/reflection_episode_test.go`, `internal/pipeline/post_turn_test.go` | L1/L2 |
| R-AGENT-171 | Executive-state growth blocks verified conclusions and unresolved-question resolution only on verifier failures that undermine evidence trust (certainty/contradiction/freshness/provenance), not on unrelated whole-turn failures like partial subgoal coverage or missing next steps | `internal/pipeline/executive_growth_test.go` | L1/L2 |
| R-AGENT-172 | `EpisodeSummary` persists a structured JSON payload without losing turn-state fields during round-trip serialization | `internal/agent/memory/reflection_episode_test.go` | L1/L2 |
| R-AGENT-173 | Post-turn reflection writes both compact text and structured `content_json` into `episodic_memory` on the live path | `internal/pipeline/post_turn_test.go` | L1/L2 |
| R-AGENT-174 | Consolidation prefers the structured `episodic_memory.content_json` payload over reparsing the summary string when both are present | `internal/agent/memory/consolidation_distillation_test.go` | L1/L2 |
| R-AGENT-180 | Consolidation breadth is pinned explicitly: only repeated high-quality learnings, evidence refs, fix patterns, and canonical relations promote, with distinct support thresholds and no widening from low-quality episode noise | `internal/agent/memory/consolidation_distillation_test.go` | L1/L2 |
| R-AGENT-175 | `Session.SetMemoryContext(...)` derives a typed `VerificationEvidence` artifact for compatibility callers, including executive-state subsections and canonical evidence detection | `internal/session/verification_evidence_test.go` | L1/L2 |
| R-AGENT-176 | Explicit typed verification evidence is not overwritten by later `SetMemoryContext(...)` compatibility normalization | `internal/session/verification_evidence_test.go` | L1/L2 |
| R-AGENT-177 | `BuildVerificationContext(...)` consumes typed evidence only; rendered-memory parsing is no longer owned by the verifier | `internal/pipeline/verifier_typed_evidence_test.go`, `internal/pipeline/verifier_test.go` | L1/L2 |
| R-AGENT-177A | Typed verification evidence carries structured contradiction items from both compatibility parsing and context assembly so verifier contradiction handling is not limited to a boolean flag | `internal/session/verification_evidence_test.go`, `internal/agent/memory/context_assembly_test.go`, `internal/pipeline/verifier_typed_evidence_test.go` | L1/L2 |
| R-AGENT-177B | Claim audits carry per-claim proof requirements and missing-proof diagnostics, and high-risk contradiction-aware claims can satisfy proof obligations by explicitly acknowledging contested evidence instead of being forced through unconditional-anchor rules | `internal/pipeline/verifier_claims_test.go` | L1/L2 |
| R-AGENT-177C | Verifier trace summary carries operator-legible counts for contested claims, reconciled claims, and proof-gap claims instead of hiding them only in raw claim JSON | `internal/pipeline/verifier_trace_test.go` | L1/L2 |
| R-AGENT-177D | Verifier retry guidance becomes contradiction/proof aware on high-risk failures, telling the model to reconcile contested evidence and anchor unsupported claims instead of only emitting a generic retry instruction | `internal/pipeline/verifier_test.go` | L1/L2 |
| R-AGENT-178 | `retryWithGuardsDetailed(...)` is the authoritative guard-triggered retry implementation and rebuilds contextual state across retries | `internal/pipeline/guard_retry_test.go`, `internal/pipeline/guard_retry_artifacts_test.go` | L1/L2 |
| R-AGENT-179 | Cache hits apply contextual guards using the live session-derived `GuardContext` instead of the weaker text-only guard path | `internal/pipeline/guard_retry_artifacts_test.go` | L1/L2 |
| R-AGENT-180 | `Pipeline.guardsForPreset(...)` is the live preset owner when no custom chain is injected: full/cached/stream resolve from the centralized `GuardRegistry`, while `GuardSetNone` disables guards entirely | `internal/pipeline/guard_registry_test.go` | L1/L2 |
| R-AGENT-181 | Runtime guard-chain fitness is asserted against the actual registry-backed chain surface rather than brittle source parsing: `DefaultGuardChain()` delegates to `FullGuardChain()`, required guards are present, and every surfaced guard still implements `Check(...)` | `internal/api/architecture_fitness_test.go` | L1/L2 |
| R-AGENT-182 | The system-prompt tool roster is derived from the same selected per-request tool defs that populate `llm.Request.Tools`, so the model is not told about tools outside the live injected surface | `internal/daemon/daemon_adapters_test.go` | L1/L2 |
| R-AGENT-183 | `ContextBuilder.BuildRequest` drops compacted history messages that collapse to empty content, so blank conversational messages are not emitted into the final `llm.Request` and later scrubbed only as a service-layer fallback | `internal/agent/context_user_message_invariant_test.go` | L1/L2 |
| R-AGENT-184 | Stage 8 authority resolution is a live `SecurityClaim` owner: it resolves the claim, attaches it to the session, annotates authority/claim sources on the trace, and applies threat-caution downgrade on the actual pipeline path | `internal/pipeline/security_claim_stage_test.go` | L1/L2 |
| R-AGENT-185 | Tool sandbox resolution now has one shared contract: `ResolvePath(...)` and `ValidatePath(...)` agree on `~` rejection, workspace-relative anchoring, and explicitly allowed absolute paths outside the workspace | `internal/agent/tools/sandbox_test.go` | L1/L2 |
| R-AGENT-185a | Tool sandbox canonicalization must stay stable for future child paths beneath symlinked workspace/allowlist roots, so a non-existent write target under `/var/...` is not rejected when the authoritative root resolves to `/private/var/...` | `internal/agent/tools/sandbox_test.go`, `internal/pipeline/sandbox_propagation_test.go`, `internal/agent/tools/builtins_test.go` | L1/L2 |
| R-LLM-119 | Tool-bearing routing must prefer observed `TOOL_USE` candidates and ignore under-evidenced models when proven tool-use candidates exist | `internal/llm/router_test.go`, `internal/llm/routing_fitness_test.go`, `internal/llm/task_semantics_test.go` | L1/L2 |
| R-AGENT-186 | Sensitive config mutation protection now uses one shared matcher across pre-execution policy and post-inference guards, so fields like `server.auth_token`, `wallet.keyfile`, and `trusted_proxy` cannot silently drift between those surfaces | `internal/security/config_protection_test.go`, `internal/agent/policy/engine_test.go`, `internal/pipeline/guards_config_protection_test.go` | L1/L2 |
| R-AGENT-187 | `FilesystemDenialGuard` now distinguishes false filesystem disclaimers from real sandbox denials by consulting tool results, so legitimate path-policy outcomes are not rewritten as if they were fake capability disclaimers | `internal/pipeline/guards_truthfulness_test.go` | L1/L2 |
| R-AGENT-188 | `pathProtectionRule` protects actual path/file markers without treating generic content words like `secret` as protected paths, while shared config-protection still covers sensitive config fields | `internal/agent/policy/engine_test.go` | L1/L2 |
| R-AGENT-189 | Workspace-only allowlist matching in `pathProtectionRule` uses exact-match-or-subtree semantics, so `/data/vault` does not accidentally admit `/data/vaultBackup` while still allowing `/data/vault/...` | `internal/agent/policy/engine_test.go` | L1/L2 |
| R-AGENT-190 | Post-inference execution and financial truth guards now respect real policy/sandbox denials and denied financial tool results, instead of treating any tool presence as proof of successful execution | `internal/pipeline/guards_truthfulness_test.go`, `internal/pipeline/guards_financial_truth_test.go`, `internal/pipeline/guards_financial_verification_test.go` | L1/L2 |
| R-AGENT-191 | Simple direct tasks are not widened into heavy autonomous turns by intent label alone: task synthesis only enables retrieval on explicit continuity cues, and turn policy keeps `simple` + `execute_directly` tasks on a focused execution envelope with bounded tools | `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/turn_policy_test.go` | L1/L2 |
| R-AGENT-192 | Placeholder scaffolding such as `[assistant message]` and `[agent message]` is suppressed at the loop boundary so it cannot leak into history, retries, or final output | `internal/agent/loop_test.go` | L1/L2 |
| R-AGENT-193 | Generic note titles or filenames containing lexical noise like `test` do not get upcast into coding intent unless code-specific phrases are actually present | `internal/pipeline/task_synthesis_test.go` | L1/L2 |
| R-AGENT-194 | Cross-turn repetition guards preserve temporal atomicity: `buildGuardContext` exposes only prior-turn assistant history to `PreviousAssistant` / `PriorAssistantMessages` while preserving current-turn tool results, so successful tool-backed confirmations are not misclassified as self-repetition | `internal/pipeline/guard_context_population_test.go`, `internal/pipeline/guards_quality_test.go` | L1/L2 |
| R-UPGRADE-1 | `applyProvidersUpdate` mismatch error is self-describing: includes URL fetched, expected hash from manifest, and received hash computed from downloaded bytes — symmetric with the binary-update narration so operators can triage without re-running curl | `cmd/updatecmd/update_parity_test.go` | L1 |
| R-UPGRADE-2 | `applySkillsUpdate` mismatch error identifies the specific skill file plus URL / expected / received hashes so operators can tell whether one file or the whole pack is misaligned | `cmd/updatecmd/update_parity_test.go` | L1 |
| R-UPGRADE-3 | `applyProvidersUpdate(refreshConfig=false)` preserves a customized local providers.toml: no fetch, no SHA check, no overwrite — even when the registry manifest declares a stale SHA. Local edits (API keys, custom providers) survive `roboticus upgrade all` | `cmd/updatecmd/update_parity_test.go` | L1 |
| R-UPGRADE-4 | `applyProvidersUpdate(refreshConfig=true)` is the documented opt-in escape hatch: downloads, verifies the SHA, and overwrites the local file even when customized | `cmd/updatecmd/update_parity_test.go` | L1 |
| R-UPGRADE-5 | `applySkillsUpdate(refreshConfig=false)` preserves per-file: a manifest-declared skill that exists locally is left untouched (no SHA check), while a manifest-declared skill that's missing locally is fresh-installed and SHA-verified in the same call | `cmd/updatecmd/update_parity_test.go` | L1 |
| R-DBPERMS-1 | `db.Open()` tightens a fresh database file to mode 0o600 even when the process umask is 0022 (default umask would otherwise produce 0o644 — world-readable) | `internal/db/store_permissions_test.go` | L1 |
| R-DBPERMS-2 | `db.Open()` tightens an existing 0o644 database file to 0o600 on next boot (upgrade-friendly: pre-v1.0.6 installs auto-fix without operator action) | `internal/db/store_permissions_test.go` | L1 |
| R-DBPERMS-3 | WAL sidecar files (`<path>-wal`, `<path>-shm`) created by SQLite's WAL mode also receive 0o600 mode — they hold uncommitted page data and warrant the same protection as the main DB | `internal/db/store_permissions_test.go` | L1 |
| R-DBPERMS-4 | `db.Open(":memory:")` short-circuits the chmod path silently — no stat error, no warning log — since there is no on-disk file to protect | `internal/db/store_permissions_test.go` | L1 |
| R-DBMIG-042 | Migration 042 (relationship_memory.updated_at) uses the SQLite-compliant ADD COLUMN nullable + UPDATE backfill pattern; non-constant defaults are rejected by ALTER TABLE per SQLite spec, and the broken original migration is replaced (existing installs at schema_version >= 42 are skipped by the runner) | `internal/db/fts_trigger_completeness_test.go` (round-trip exercises the new column path on every migration run) | L1 |
| R-DAEMON-PID-1 | `WritePIDFile` + `ReadPIDFile` round-trip the current process's PID with restrictive 0o600 mode (PID file lives alongside DB and shares the security profile) | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-PID-2 | `WritePIDFile` refuses to clobber a PID file pointing at a live, non-self process — prevents two daemons from racing into the same on-disk slot | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-PID-3 | `WritePIDFile` silently overwrites a stale PID file (pointing at a dead process); enables the kill -9 recovery path without operator intervention | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-STOP-1 | `Control(cfg, "stop")` is idempotent: stopping an already-stopped daemon (no PID file, service not installed) returns nil — `roboticus daemon stop` is safe to script as part of state-reset | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-STOP-2 | `Control(cfg, "stop")` cleans up a stale PID file (pointing at a dead process) and returns nil — exact recovery path for the v1.0.5 incident where the daemon was kill -9'd before the user ran `daemon stop` | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-STOP-3 | `Control(cfg, "stop")` against a live daemon sends SIGTERM, waits for graceful shutdown, removes the PID file, and returns nil — the happy path for normal shutdown | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-STOP-4 | `stopByPID` treats both `syscall.ESRCH` (process didn't exist when signal was dispatched) and `os.ErrProcessDone` (Go runtime intercepted Signal for a process it tracked) as semantically equivalent — both mean "process is gone, treat as already-stopped" | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-LAUNCHCTL-1 | `isLaunchctlNotLoaded` substring matcher correctly distinguishes "service not present in this domain" (a successful no-op when iterating system/ → gui/) from real launchctl failures; case-insensitive matching on documented launchctl error markers | `internal/daemon/control_pidfile_test.go` | L1 |
| R-DAEMON-NOBOOT-1 | `NewServiceOnly(cfg)` constructs a `service.Service` handle without opening the database, initializing the LLM stack, or spawning any goroutines — Install / Uninstall / Control / Status all use this path so `roboticus daemon stop` no longer prints the 12-step boot sequence and no longer chowns user files to root when run under sudo | covered structurally by the absence of `daemon.New()` calls in `Control` / `Install` / `Uninstall` / `Status` (`internal/daemon/daemon.go`); audit-only contract | A |
| R-WARNINGS-1 | `core.AddSystemWarning` dedupes on (Code, Detail): identical warnings recorded by multiple subsystems collapse to one entry on the dashboard banner | `internal/core/system_warnings_test.go` | L1 |
| R-WARNINGS-2 | Same Code with different Detail produces TWO entries — operators see one banner per affected resource (e.g., one warning per ambient DB created) | `internal/core/system_warnings_test.go` | L1 |
| R-WARNINGS-3 | Severity defaults to `normal` and `RaisedAt` defaults to `time.Now()` when caller omits them; explicit values are preserved | `internal/core/system_warnings_test.go` | L1 |
| R-WARNINGS-4 | Empty Code is silently rejected (defensive: prevents misconfigured callers from creating un-keyable banner entries the dashboard can't address) | `internal/core/system_warnings_test.go` | L1 |
| R-WARNINGS-5 | `SystemWarningsSnapshot()` returns `nil` (not empty slice) when collector is empty so JSON marshalling produces `null` — distinguishes "endpoint hasn't initialized" from "no warnings to show"; for the HTTP wire shape we explicitly coerce nil → `[]` to keep dashboard TS types non-nullable | `internal/core/system_warnings_test.go`, `internal/api/routes/system_warnings_test.go` | L1 |
| R-WARNINGS-6 | Snapshot mutation does not propagate back to the live collector; subsequent Add calls do not retroactively appear in earlier snapshots — necessary for safe dashboard polling without locking | `internal/core/system_warnings_test.go` | L1 |
| R-WARNINGS-7 | `GET /api/admin/system-warnings` returns `{warnings: [], count: 0}` (not `{warnings: null}`) when no warnings exist — dashboard TypeScript can rely on `warnings: SystemWarning[]` being non-null | `internal/api/routes/system_warnings_test.go` | L1 |
| R-WARNINGS-8 | `GET /api/admin/system-warnings` returns recorded warnings with stable `Code` field intact, so dashboard can key localized strings + dismissal state on the code | `internal/api/routes/system_warnings_test.go` | L1 |
| R-WARNINGS-9 | `initConfig` records `WarningCodeConfigDefaultsUsed` (severity high) when no config file is found at the resolved search path — the silent-default failure mode that produced the v1.0.5 rogue-DB report now surfaces as a dashboard banner AND a boot-time stderr warning | `cmd/root.go` (instrumentation) + `internal/core/system_warnings_test.go` (collector contract); audit-only for the wiring | A |
| R-WARNINGS-10 | `db.Open()` records `WarningCodeDatabaseCreatedAtPath` (severity high) when a new DB file is created on disk that didn't pre-exist — operators can spot ambient creation at the wrong path before it accumulates real data | `internal/db/store.go` (instrumentation) + `internal/core/system_warnings_test.go` (collector contract); audit-only for the wiring | A |
| R-MCP-DIAG-1 | `ConnectStdio` failure surfaces child stderr in the returned error (MCP-release-blocker-checklist item 4) — pre-v1.0.6 operators saw only `initialize failed: EOF` with zero context; now they see the actual cause (e.g., npm package not found, missing dependency, version mismatch) | `internal/mcp/stdio_diagnostic_test.go` | L1 |
| R-MCP-DIAG-2 | `ConnectStdio` failure surfaces child exit state (`child exit: exit status N`) so operators can distinguish "child died" from "child still running but stdout closed" — both have different remediation paths | `internal/mcp/stdio_diagnostic_test.go` | L1 |
| R-MCP-DIAG-3 | `StdioTransport.ChildDiagnostic()` truncates stderr to the most recent 8KB and indicates truncation — chatty children can't blow up the daemon's memory, but the diagnostic-relevant tail is preserved | `internal/mcp/stdio_diagnostic_test.go` | L1 |
| R-AGENT-PROMPT-1 | System prompt's `Tool Operations` block now directs the agent to ATTEMPT tool calls and surface real policy denials rather than reasoning preemptively about its own constraints — closes the v1.0.5 behavioral-soak `filesystem_count_only` failure where the agent self-censored without invoking any tool | `internal/agent/prompt.go` (instrumentation); behavioral verification via the soak harness in `scripts/run-agent-behavior-soak.py` | A |
| R-AGENT-PROMPT-2 | System prompt directs the agent to honor explicit output-format requests verbatim ("only the number", "single sentence", etc.) — closes the secondary `filesystem_count_only` failure mode where the agent narrated around a minimal-output ask | `internal/agent/prompt.go` (instrumentation); behavioral verification via the soak harness | A |
| R-PIPE-WATCHDOG-1 | `TraceRecorder.CurrentSpan()` returns the live in-flight span name + wall-clock duration; returns the zero value when no span is active so the watchdog never logs empty stage names during inter-stage gaps | `internal/pipeline/stage_watchdog_test.go` | L1 |
| R-PIPE-WATCHDOG-2 | `CurrentSpan().Duration` reflects live wall-clock time, not a snapshot — necessary for the watchdog's threshold check to ever trigger on a hung stage | `internal/pipeline/stage_watchdog_test.go` | L1 |
| R-PIPE-WATCHDOG-3 | `CurrentSpan` is concurrent-safe with `BeginSpan`/`Annotate`/`EndSpan` — the watchdog goroutine reads while the pipeline goroutine writes; verified under `go test -race` | `internal/pipeline/stage_watchdog_test.go` | L1 |
| R-PIPE-WATCHDOG-4 | `Pipeline.Run` spawns a stage liveness watchdog that logs `"pipeline stage running longer than expected"` after `stageLivenessThreshold` (20s) and re-logs every `stageLivenessProbeInterval` (10s) thereafter — turns cold-start hangs into actionable evidence of which stage is stuck | `internal/pipeline/pipeline.go` (instrumentation) + R-PIPE-WATCHDOG-1..3 (primitives); audit-only for the wiring | A |
| R-MCP-CHECKLIST-3 | `ConnectSSE` round-trips end-to-end against an in-tree httptest fixture: initialize → tools/list (1 tool) → tools/call (echo) → result content includes the original payload — proves the SSE transport implementation is correct without depending on any external network or third-party server availability | `internal/mcp/sse_validation_test.go::TestSSEReleaseChecklist_FullValidation` | L1 |
| R-MCP-SSE-04 | SSE transport honors `event: endpoint` discovery messages and switches subsequent POST traffic to the resolved message endpoint instead of assuming the stream URL is also the JSON-RPC POST target | `internal/mcp/sse_test.go::TestSSETransport_EndpointEventOverridesPostURL` | L1 |
| R-MCP-SSE-05 | Auth-bearing SSE config is propagated consistently across the live GET stream and POST call path, so header-based third-party SSE targets do not drift between startup, route tests, and validation harnesses | `internal/mcp/sse_test.go::TestSSETransport_ConfiguredHeadersAppliedToGetAndPost`, `internal/mcp/config_bridge_test.go::TestConfigFromCoreEntry_AuthTokenPropagatesToHeadersAndEnv` | L1 |
| R-MCP-SSE-06 | The named-target SSE validation harness returns structured evidence including server identity, tool inventory, resolved POST endpoint, and tool-call proof using the same runtime transport/config seam as daemon startup and route validation | `internal/mcp/sse_validation_test.go::TestValidateSSETarget_ProducesStructuredEvidence`, `internal/api/routes/routes_test.go::TestValidateSSEMCPServer` | L1/L2 |
| R-SANDBOX-1 | Less-restrictive: pipeline constructed with `AllowedPaths=["/A"]` propagates to `session.AllowedPaths` and `ToolContext.AllowedPaths`; ValidatePath against `/A/file` permits | `internal/pipeline/sandbox_propagation_test.go` | L1 |
| R-SANDBOX-2 | More-restrictive: pipeline with empty AllowedPaths denies anything outside the workspace; workspace-internal paths still permitted | `internal/pipeline/sandbox_propagation_test.go` | L1 |
| R-SANDBOX-3 | Bidirectional reconfiguration: widening AllowedPaths between sessions affects new sessions only; existing sessions snapshot their AllowedPaths at creation time so a live config reload can't silently widen or narrow active turns' permissions | `internal/pipeline/sandbox_propagation_test.go` | L1 |
| R-SANDBOX-4 | Snapshot isolation: the live pipeline session-context owner must copy `AllowedPaths` instead of sharing the slice header, so in-place mutation of pipeline.allowedPaths cannot retroactively affect existing sessions | `internal/pipeline/sandbox_propagation_test.go` | L1 |
| R-SANDBOX-5 | Audit: `Security.Filesystem.ToolAllowedPaths`, `Security.ScriptAllowedPaths`, and `Security.InterpreterAllow` are NOT yet wired through PipelineDeps to runtime tool execution. The test exists as documentation; if any of these are wired in the future, the test must be updated to cover the new path with a bidirectional regression pair | `internal/pipeline/sandbox_propagation_test.go` (audit) | A |
| R-SOAK-CACHE-1 | Clone-mode soak prep clears `semantic_cache` so behavioral scenarios exercise the live model + prompt/policy path on every run; without this, latency_s=0.0 across the board indicates cache replay is masking real regressions | `scripts/run-agent-behavior-soak.py::clear_response_cache` (audit) | A |
| R-SOAK-PATHS-1 | Managed soak prep merges required test paths into both `[security]` and `[security.filesystem]` allowlists in the ISOLATED config (operator's live config is untouched) so scenarios like `filesystem_count_only` and `folder_scan_downloads` exercise behavior rather than TOML-formatting accidents | `scripts/run-agent-behavior-soak.py::extend_allowed_paths_for_soak` (audit) | A |
| R-TOOLS-INTROSPECT-1 | The daemon registers both `introspect` and the natural-language alias `introspection`, so the live model can succeed even when it emits the soak-observed synonym instead of the canonical tool name | `internal/agent/tools/registry_test.go` | L1 |
| R-SANDBOX-TILDE-1 | Filesystem tools reject `~` home-directory shortcuts at path resolution time with a clear error instead of silently treating them as workspace-relative or allowing them to expand later in shell execution | `internal/agent/tools/builtins_test.go` | L1 |
| R-SANDBOX-TILDE-2 | In `workspace_only` mode, policy evaluation denies bash/tool arguments containing `~` before shell expansion, closing the behavioral-soak `tilde_distribution` gap where `find ~/...` escaped the intended workspace sandbox semantics | `internal/agent/policy/engine_test.go` | L1 |
| R-LLM-TOOLPARSE-1 | Text-mode tool-call fallback strips parsed `{\"tool_call\": ...}` JSON blocks back out of assistant content once they have been converted into structured ToolCalls, preventing raw invocation payloads from leaking to users or contaminating later turns | `internal/llm/tool_parsing_test.go`, `internal/llm/client_formats_test.go` | L1 |
| R-CTX-USERMSG-1 | `ContextBuilder.BuildRequest` ALWAYS includes the latest user message in the LLM request, even when system prompt + memory + tool defs exhaust the token budget. Pre-v1.0.6 the budget loop blindly broke at the first over-budget message, leaving `historyMessages` empty when `(sysTokCount + memTokCount + toolTokCount) >= budget`. The LLM never saw the user's prompt and replied "the user has not provided instructions" — the v1.0.6 cache-cleared soak failure mode for 6 of 10 scenarios | `internal/agent/context_user_message_invariant_test.go::TestBuildRequest_UserMessageSurvivesNegativeBudget` | L1 |
| R-CTX-USERMSG-2 | The latest user message is included VERBATIM, regardless of compaction stage. Pre-v1.0.6 layered bug: even when the user message survived the budget loop, `compact()` at StageSkeleton replaced its content with the literal "[user message]" string. The latest user message is the smallest, most important payload in the request — compacting it makes no sense regardless of pressure | `internal/agent/context_user_message_invariant_test.go::TestBuildRequest_UserMessageSurvivesNegativeBudget` | L1 |
| R-CTX-USERMSG-3 | Older history messages get COMPACTED (not the latest user message) when budget is tight. Padding from older messages does not survive verbatim under tight budget — confirms compaction enforcement still runs on history while the latest user message stays intact | `internal/agent/context_user_message_invariant_test.go::TestBuildRequest_OldHistoryDroppedFirst` | L1 |
| R-CTX-USERMSG-4 | Under generous budget, all session messages (user + assistant + history) are present in the LLM request — guards against a future refactor that over-aggressively drops history under non-tight budgets | `internal/agent/context_user_message_invariant_test.go::TestBuildRequest_UserMessagePresentInGenerousBudget` | L1 |
| R-CTX-USERMSG-5 | Anti-fade reminder injection is subordinate to the live request budget. The reminder is skipped when it would exceed the remaining request budget instead of silently pushing the final `llm.Request` over budget after history selection | `internal/agent/context_user_message_invariant_test.go::TestBuildRequest_SkipsAntiFadeReminderWhenItDoesNotFitBudget` | L1 |
| R-CTX-USERMSG-6 | Introspection-shaped turns get a runtime-owned capability snapshot in the system prompt, built from the live selected tool surface, active skills, and configured subagent roster, while non-introspection turns do not pay that prompt tax | `internal/daemon/daemon_adapters_test.go` | L1/L2 |
| R-TOOLS-05 | Operational delegation inventory remains on the live tool surface: `list-subagent-roster` and `list-available-skills` are registered by the daemon-owned registry, query authoritative store-backed metadata, and stay pinned in the default operational always-include set | `internal/agent/tools/tools_comprehensive_test.go`, `internal/agent/tools/tool_search_test.go`, `internal/daemon` | L1/L2 |
| R-TOOLS-06 | Delegated task lifecycle remains on the live runtime control plane: `task-status`, `list-open-tasks`, and `retry-task` are backed by one delegated-task repository over `tasks`, `task_events`, and delegation outcomes, are registered by the daemon-owned registry, and keep the read-side tools pinned in the default operational always-include set | `internal/db/delegated_task_lifecycle_repo_test.go`, `internal/agent/tools/tools_comprehensive_test.go`, `internal/agent/tools/tool_search_test.go`, `internal/daemon` | L1/L2 |
| R-TOOLS-07 | Subagent composition remains on the live runtime control plane: `compose-subagent` is backed by one authoritative `sub_agents` repository, is registered by the daemon-owned registry, stays pinned in the default operational always-include set, and rejects subagent callers so worker creation remains orchestrator-only | `internal/db/subagent_composition_repo_test.go`, `internal/agent/tools/tools_comprehensive_test.go`, `internal/agent/tools/tool_search_test.go`, `internal/daemon`, `internal/api/routes/admin_test.go` | L1/L2 |
| R-TOOLS-08 | Multi-subagent orchestration remains on the live runtime control plane: `orchestrate-subagents` is backed by one orchestration repository over `tasks`, `task_events`, and `agent_delegation_outcomes`, stays pinned in the default operational always-include set, rejects subagent callers, and the pipeline delegation stage writes and updates that same workflow artifact instead of relying on a prompt-only orchestration contract | `internal/db/subagent_orchestration_repo_test.go`, `internal/agent/tools/tools_comprehensive_test.go`, `internal/agent/tools/tool_search_test.go`, `internal/pipeline/delegation_orchestration_test.go`, `internal/daemon` | L1/L2 |
| R-TOOLS-14 | Skill composition remains on the live runtime control plane: `compose-skill` is backed by one authoritative repository that writes both the durable skill artifact and the `skills` row, the catalog install route reuses that same repository, the tool is registered and pinned in the default operational always-include set, and subagent callers are rejected so skill composition remains orchestrator-owned | `internal/db/skill_composition_repo_test.go`, `internal/agent/tools/tools_comprehensive_test.go`, `internal/agent/tools/tool_search_test.go`, `internal/api/routes/runtime_workspace_test.go`, `internal/daemon`, `internal/parity` | L1/L2 |
| R-TOOLS-15 | Obsidian vault authoring is a first-class runtime capability: when a vault is configured the daemon registers `obsidian_write`, prompt guidance references that explicit tool, the tool writes vault-relative Markdown notes into the configured vault root, and the operational always-include set keeps the capability available on live authoring turns | `internal/agent/tools/builtins_test.go`, `internal/agent/tools/tool_search_test.go`, `internal/agent/prompt_test.go`, `internal/daemon` | L1/L2 |
| R-TOOLS-16 | Capability truth converges across config, DB-enabled skills, runtime loading, and task synthesis: workspace-local Obsidian vaults are auto-detected when Obsidian is enabled but `vault_path` is blank, DB-enabled skill source files are loaded into the live runtime even when the configured skills directory is wrong, and Obsidian/vault requests satisfy capability fit through description-backed and hyphenated skill lexicon matching instead of drifting into “missing skill” false negatives | `internal/core/core_comprehensive_test.go`, `internal/agent/skills/loader_test.go`, `internal/pipeline/task_synthesis_test.go`, `internal/daemon` | L1/L2 |
| R-CTX-USERMSG-7 | Common capability/introspection questions are answered by a pipeline shortcut from the runtime-owned capability summary instead of forcing the model into an exploratory tool loop | `internal/pipeline/shortcut_handler_test.go`, `internal/pipeline/coverage_boost_test.go` | L1/L2 |
| R-VERIFY-FMT-1 | Formatting-only directives such as `reply with only the number 1` are normalized away before verification and executive-growth bookkeeping, so they do not become durable unresolved questions or semantic subgoals | `internal/pipeline/verifier_test.go`, `internal/pipeline/executive_growth_test.go` | L1 |
| R-TOOLS-17 | Direct filesystem inspection turns use a focused inspection envelope and pin the authoritative inspection tools (`glob_files`, `list_directory`, `read_file`, runtime context) instead of relying on semantically lucky default pruning | `internal/pipeline/turn_policy_test.go`, `internal/daemon/daemon_adapters_test.go`, `internal/agent/tools/semantics_test.go` | L1/L2 |
| R-VERIFY-CONT-1 | Canonical session-history evidence outranks generic retrieval gaps on continuity/recall turns, so `what did I tell you earlier` answers do not degrade into false uncertainty when the answer is already present in the current session | `internal/pipeline/verifier_test.go` | L1 |
| R-TOOLS-18 | Filesystem inspection tool contracts explicitly advertise both workspace-relative paths and absolute allowed paths, preventing the model from biasing itself into workspace-only path forms for allowed home-directory folders like Downloads | `internal/agent/tools/builtins_test.go` | L1 |
| R-TOOLS-19 | Focused inspection turns resolve allowed-root aliases such as `~` / “home folder” through the same inspection-target authority as absolute paths, so home-directory distribution/listing requests do not degrade into conversational “I’ll check” replies with zero tool calls | `internal/pipeline/inspection_turn_test.go`, `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/turn_policy_test.go` | L1/L2 |
| R-SCHED-1 | Focused scheduling turns keep explicit cron creation on the direct scheduling path while retrieving session continuity for shorthand aliases like `quiet ticker`, so compressed-history scheduling requests still create the intended job instead of widening into procedural churn | `internal/pipeline/task_synthesis_test.go`, `internal/pipeline/turn_policy_test.go`, `internal/pipeline/soak_behavior_test.go` | L1/L2 |
| R-AGENT-INSTRUMENT-1 | Agent loop's `think()` method emits INFO-level logs with `last_user` (200-char snippet of the most recent user message in the LLM request) and `content_preview` (200-char snippet of the LLM response) on every turn. Empty `last_user` is a structural signal of the empty-prompt bug class — operators watching live `roboticus serve` can spot it without needing post-mortem trace inspection | `internal/agent/loop.go::think` (instrumentation); audit-only | A |
| R-SOAK-CACHE-TOGGLE-1 | Soak script supports `SOAK_CLEAR_CACHE` (1=wipe semantic_cache before run) and `SOAK_BYPASS_CACHE` (1=send no_cache=true on every request) as orthogonal toggles. Operators can run cache-on AND cache-off soaks against the same build to evaluate cache efficacy AND uncached agent efficacy independently. Pre-v1.0.6 the soak unconditionally replayed cached responses, masking real agent behavior | `scripts/run-agent-behavior-soak.py` (env vars CLEAR_CACHE + BYPASS_CACHE) (audit) | A |
| R-DB-REPAIR-1 | If SQLite corruption is confined to rebuildable derived structures (`pipeline_traces`, `turn_diagnostic_events`, dependent trace tables/indexes, `memory_fts` internals), the store repairs them centrally, backs up the damaged files, rebuilds FTS from authoritative tables, and leaves authoritative session/memory state intact | `internal/db/derived_repair_test.go` | L1/L2 |
| R-REL-WORKFLOW-1 | Active CI/release workflow actions must not depend on deprecated Node 20 runtimes. This is enforced by explicit workflow version review of checkout/setup/artifact/release/security actions and by avoiding composite actions that vendor stale Node 20 dependencies into otherwise green jobs. | `/.github/workflows/ci.yml`, `/.github/workflows/release.yml` (audit) | A |

## Governance Rules

1. Every bug fix touching an advertised feature should add or update at least
   one regression row above.
2. A feature may not be marked complete in docs/UI/README without:
   - at least one deterministic regression test, and
   - inclusion in the live smoke path if it is operator-critical.
3. Any new dashboard or CLI feature must either:
   - gain test coverage and be added to this matrix, or
   - remain explicitly experimental and outside the feature-complete claim.
4. If Roboticus advertises a user-weighted metascore spider graph, it must have
   explicit weighting-correctness and efficacy tests. Approximate routing tests
   are not enough.
