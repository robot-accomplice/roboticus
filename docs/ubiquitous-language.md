# Roboticus Ubiquitous Language

This document defines the shared product and architecture vocabulary for
Roboticus. Use these terms consistently in architecture docs, code comments,
logs, CLI help, API descriptions, dashboard labels, release notes, tests, and
operator-facing diagnostics.

Legacy function, route, command, and table names may remain when renaming would
create compatibility churn. New prose should still use the terms below.

## Architectural Style

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Connector-Factory Pattern** | The core architecture: connectors normalize I/O and call factories; factories own behavior. | Do not use to describe route handlers that make business decisions. |
| **Connector** | A thin I/O adapter for Telegram, API, CLI, webhook, SSE, websocket, dashboard webchannel, or another transport. It parses, calls the factory, and formats output. | Do not put policy, memory, routing, tool, or agent behavior in connectors. |
| **Factory** | A behavior owner that transforms input into output without caring which transport invoked it. The Unified Pipeline is the primary factory. | Do not use for transport-specific glue. |
| **Unified Pipeline** | The canonical turn factory in `internal/pipeline`. It owns request lifecycle, authority, memory retrieval, tool selection, routing, inference, guards, post-turn ingest, diagnostics, and output. | Do not create parallel agent-turn execution paths. |
| **Pipeline Stage** | A named step in the Unified Pipeline with a bounded responsibility and explicit inputs/outputs through `pipelineContext`. | Do not let stages call each other directly or share hidden mutable state. |
| **Composition Root** | The daemon/bootstrap layer that wires dependencies together. | Do not put business logic here because it happens to have all dependencies. |
| **Control Plane** | Operator/admin/runtime management surfaces: configuration, observability, model state, repair, update, routing, scheduler, and diagnostics. | Do not conflate with model inference behavior. |
| **Data Plane** | Runtime execution data: turns, tool calls, traces, memory rows, model calls, delivery events, and task artifacts. | Do not use for settings pages or admin toggles. |

## Interfaces And Delivery Surfaces

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **API Surface** | `/api/**` routes intended for externally addressable clients and independently permissioned access. | Do not use as the dashboard's private state bus. |
| **Dashboard Webchannel** | Dashboard-private websocket/topic delivery for UI state snapshots and UI events. | Do not expose as an external API contract. |
| **Producer / Composition Seam** | Shared code that builds domain truth consumed by both APIs and webchannels. | Do not duplicate business logic separately in API routes and dashboard code. |
| **Channel Adapter** | A connector for a specific external channel such as Telegram or WhatsApp. | Do not let channel adapters own capability truth. |
| **SSE Stream** | Server-sent event transport for streaming responses. | Do not use as a separate pipeline. |
| **Websocket Topic** | Named dashboard/webchannel topic carrying state updates. | Do not make topics reconstruct state from unrelated API payloads. |

## Agent Roles And Execution

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Operator** | The human directing Roboticus. | Do not call the operator a user in architecture docs when authority matters. |
| **Orchestrator** | Operator-facing agent persona that interprets tasks, delegates bounded work, and packages results. | Do not use for execution-only workers. |
| **Subagent** | Non-operator-facing execution worker with bounded task scope and minimal/no personality. | Do not let subagents report directly to operators. |
| **Taskable Agent** | Any agent that can receive work: orchestrator or subagent. | Do not use when only subagents are meant. |
| **Agent Registry** | Single source of truth for agent roster/inventory across Workspace, Agents: Roster, Agents: List, routing, and delegation. | Do not let each UI/API surface invent its own roster. |
| **Workspace** | Visual runtime map of orchestrators, subagents, tools, memory, and system surfaces. It is an Agents tab, not a separate agent registry. | Do not count orchestrators as idle worker agents. |
| **Delegation** | Orchestrator assigns bounded work to a subagent and receives structured results. | Do not use for operator-facing final reporting. |
| **Task State** | Structured current-turn/task interpretation: intent, complexity, source of truth, required tools, risk, and expected outputs. | Do not replace with prose-only inference. |

## Turn Lifecycle

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Turn** | One operator input and the system's resulting processing/output. | Do not use interchangeably with a stored message row. |
| **Session** | Durable conversation scope keyed by agent/channel/scope. | Do not assume cross-channel continuity without explicit consent. |
| **Message** | Stored user/assistant/tool content inside a session. | Do not use message ID where canonical turn ID is required. |
| **Canonical Turn ID** | Stable ID established before execution and used across traces, tool calls, RCA, benchmark rows, and UI drill-down. | Do not derive only after a successful turn. |
| **Execution-Only Context** | Framework scaffolding used for current-turn model/tool execution but not persisted as user transcript or mined as memory. | Do not store it in `session_messages` as operator-authored text. |
| **Pending Action** | Typed state that the assistant left a concrete unresolved next action. Short follow-ups bind to this state. | Do not make exact phrases such as `please do` the owner of continuation. |
| **Continuation** | Resuming an unresolved current task/action from state and recent context. | Do not use for arbitrary retries or social chat. |
| **Correction Turn** | Operator disputes, corrects, or redirects a prior answer. | Do not treat as praise or confirmation. |
| **State-First Continuity** | Continuation determined by prior task/action/evidence state before lexical phrase hints. | Do not implement magic-word mechanics as control flow. |
| **Magic Phrase** | A brittle exact text trigger. It may be a weak detector hint, never the authority. | Do not encode behavior around blessed wording. |

## Agent Loop Models

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **R-TEOR-R** | Internal architecture model: Retrieve, Think, Execute, Observe, Reflect, Remember. It names where evidence and memory actions happen. | Do not collapse the two Remember phases. |
| **ROVER** | Public-facing simplified model: Retrieve, Orient, Validate, Execute, Reflect. | Do not use when phase attribution must be precise. |
| **TOTOF** | Reflect/finalization brief: user Task, authoritative Observed results, key Tool outcomes, Open issues, Finalization instruction. | Do not let it replace full conversation history. It is a trailing overlay. |
| **Reflection** | Post-observation reasoning that decides whether to finalize, continue, or preserve partial useful work. | Do not use to re-execute tools unless the execution phase explicitly resumes. |
| **Observe** | The authoritative result surface from tools, model calls, policy decisions, and runtime evidence. | Do not replace observations with model guesses. |
| **Remember** | Writing useful, evidence-grounded state to memory after a turn or curation pass. | Do not persist derivable or mutable facts as durable truth. |

## Memory

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Memory Curation** | Umbrella lifecycle that keeps memory useful after turns complete: index hygiene, deduplication, promotion, distillation, contradiction handling, confidence/importance decay, pruning, orphan cleanup, and skill/procedure confidence sync. | Do not use `consolidation` as the umbrella term. |
| **Memory Consolidation** | Sub-phase of Memory Curation that merges or promotes related memory: within-tier deduplication, episodic-to-semantic promotion, episode distillation, and relation distillation. | Do not use for pruning, orphan cleanup, index backfill, or confidence decay. |
| **Index Hygiene** | Backfilling, synchronizing, decaying, and cleaning derived index/search rows such as `memory_index`, FTS rows, and embeddings. | Do not imply index rows are authoritative memory. |
| **Memory Governance** | Lifecycle decisions that change whether memory should influence future behavior: contradiction/supersession, stale marking, confidence/importance decay, pruning, quarantine, and trash policy. | Do not treat as retrieval scoring only. |
| **Working Memory** | Short-term conversational/action context for the active session: current task, recent observations, pending action frame, and continuity. | Do not replace it with long-term retrieval. |
| **Hippocampus / Memory Index** | Indexing repository that maps semantic, episodic, procedural, relationship, workflow, and fact stores into retrievable handles. | Do not treat index hits as the source memory itself. |
| **Semantic Memory** | Longer-lived facts and knowledge chunks. | Do not persist derivable or mutable facts unless they carry durable value. |
| **Episodic Memory** | Event/experience rows grounded in observed turns. | Do not promote failed or low-quality episodes as successful lessons. |
| **Procedural Memory** | Learned tool/workflow patterns and execution outcomes. | Do not treat as style guidance or persona memory. |
| **Relationship Memory** | Entity trust, interaction, and relationship state. | Do not use for arbitrary facts about people. |
| **Knowledge Facts** | Canonical subject/relation/object graph facts distilled from supported evidence. | Do not create from one anecdote or low-quality episode. |
| **Ternary Memory Influence** | Memory affects confidence as contradiction `-1`, neutral `0`, or reinforcement `1`. | Do not treat absent memory as failure. |
| **Derivable Fact** | A fact that can be recomputed from current tools, code, runtime, or prompt context. | Do not preserve as durable memory by default. |
| **Quiescence** | Period with no active session activity, required before mutating memory curation. | Do not mutate memory during active turns unless explicitly forced and safe. |
| **Memory Trash / Quarantine** | Proposed reversible holding area for records removed during hygiene/repair so forensic review and restore remain possible. | Do not hard-delete first unless policy explicitly allows it. |

## Tools, Skills, And Capabilities

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Tool** | Executable capability exposed to the model with a name, schema, operation class, policy, and runtime implementation. | Do not call a skill a tool unless it is executable through the tool surface. |
| **Skill** | File-backed or DB-backed capability/instruction package that teaches or configures behavior. | Do not assume a skill is executable without a matching tool path. |
| **Capability** | What the runtime can actually do now, proven by registry, tool surface, policy, and runtime wiring. | Do not infer from model self-description. |
| **Runtime Inventory** | Live reconciled inventory of skills/tools/providers/agents available without restart. | Do not treat boot-time snapshots as truth. |
| **Tool Pruning** | Selection of which tools enter the current model request. | Do not let pruning hide an operator-named available tool. |
| **Pinned Tool** | A registered tool explicitly named or required by the operator/current task. It must survive pruning unless policy blocks it. | Do not use semantic ranking to drop it silently. |
| **Selected Tool Surface** | Final tool set attached to the model request. | Do not diagnose capability denial without checking it. |
| **Policy Denial** | Real runtime decision blocking a tool/action. | Do not rewrite into fabricated model limitation prose. |
| **Capability Denial** | Assistant claim that it lacks a capability. Valid only when selected tool/runtime/policy evidence proves it. | Do not use as generic fallback. |

## Models, Providers, And Routing

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Provider** | External or local inference backend such as OpenRouter, DeepSeek, Moonshot, or Ollama. | Do not confuse with model name. |
| **Provider Pack** | Refreshable provider metadata in `providers.toml`: endpoints, auth, model catalog, aliases, compatibility, and deprecation metadata. | Do not require binary changes for ordinary model/catalog drift. |
| **Provider Precedence** | Config resolution order: main config, provider pack, embedded bundled defaults. | Do not let embedded defaults override user config. |
| **Wire Format** | Provider protocol contract: request/response JSON, streaming, tool calls, auth, and error semantics. | Do not model as mere config if the parser/runtime must change. |
| **Behavior Profile** | Future provider/model quirk metadata such as malformed tool JSON tolerance. | Do not hardcode one provider's quirks in generic parser code. |
| **Model Lifecycle State** | Whether a model is configured, reachable, authenticated, usable, degraded, unavailable, disabled, or benchmark-only. | Do not render all configured models as healthy. |
| **Model Role Eligibility** | Whether a model may serve live orchestrator, subagent, benchmark-only, or disabled roles. | Do not let benchmark-only models route live traffic. |
| **Routing Trace** | Evidence explaining candidate selection, filters, breakers, provider state, and chosen model. | Do not replace with a single selected model string. |
| **Model-Attributable Latency** | Time spent inside model inference calls only. | Do not include framework/tool/dashboard overhead in baseline scorecards. |
| **Pipeline Latency** | End-to-end turn/runtime duration including framework, tools, routing, and persistence. | Do not use as model efficacy latency. |

## Guardrails, Verification, And Contracts

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Guard** | Runtime check that strips unsafe artifacts, blocks invalid output, requests recovery, or classifies defects. | Do not use guards to inject canned prose. |
| **Verifier** | Evidence-aware evaluator that checks whether a response satisfies the task and observed facts. | Do not use as a style/verbosity scorer. |
| **Agent Behavioral Contract (ABC)** | Contract-shaped runtime evidence inspired by the ABC paper: preconditions, invariants, governance policy, recovery behavior, severity, and confidence effect. | Do not use as marketing or a broad DSL without measured benefit. |
| **Hard Invariant** | Contract violation that cannot ship as-is. | Do not recover silently without RCA evidence. |
| **Soft Invariant** | Recoverable degradation or quality risk. | Do not mark as full success without reason. |
| **Recovery Window** | Bounded opportunity to retry, refine, or preserve useful partial output after a violation. | Do not create infinite retry loops. |
| **Canned Prose** | Fixed user/operator-facing answer text injected by the framework. | Forbidden except protocol-mandated bodies. |
| **Partial Useful Answer** | Imperfect but task-relevant answer grounded in evidence. | Do not discard solely because it is not perfect. |

## Diagnostics, Benchmarking, And RCA

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **RCA** | Root-cause analysis grounded in traces, model calls, tool calls, guards, verifier evidence, memory, and benchmark row identity. | Do not treat as a post-hoc prose summary only. |
| **Benchmark Validity** | Whether a benchmark row is eligible to score model efficacy. Transport/provider/config failures can be invalid rather than model failure. | Do not score invalid rows as model quality zero. |
| **Model Efficacy** | The model's answer quality on valid rows. | Do not mix with framework failure or provider outage. |
| **Evidence Row** | Benchmark or diagnostic row with enough trace/turn/tool/model evidence to classify. | Do not infer from console text alone. |
| **Rescore** | Regrading stored raw outputs after rubric changes without rerunning inference. | Do not mutate original raw evidence. |
| **Soak** | Multi-turn scenario that exercises realistic behavior over time. | Do not replace with isolated unit prompts when continuity is the subject. |
| **Flight Recorder** | Durable operator-facing trace/feed of important runtime events. | Do not leave empty if traces exist elsewhere. |
| **Context Footprint** | Per-turn breakdown of model request budget: system, tools, memory, history, latest user, overlays, and unused context. | Do not show only a vague pressure label. |
| **Health Aggregate** | Evidence-backed trace/provider/model health classification. | Do not render unknown/unreachable as healthy. |

## Configuration, Repair, And Release

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Mechanic** | Local repair/hygiene command family for deterministic safe remediation. | Do not make it superficial health checks only. |
| **Repair Primitive** | Idempotent repair operation with evidence and status: repaired, skipped, needs_manual_action, or failed. | Do not silently mutate state. |
| **Upgrade Cleanup** | Pre/post-upgrade repair primitives for stale updater state, sidecars, provider/skill state, schema drift, and partial failures. | Do not leave installs ambiguous after non-binary failures. |
| **Schema Drift** | Local DB shape differs from runtime expectations. | Do not report 500s instead of compatibility status and repair path. |
| **Provider Drift** | Provider endpoint/model metadata changes without runtime wire-format change. | Do not solve with binary updates. |
| **Release Ceremony** | Canonical release procedure: release branch to `develop`, audit, promote to `main`, tag, publish, verify. | Do not start without explicit operator approval. |
| **Release Gate** | Required test/doc/audit condition before release progress. | Do not downgrade to optional checklist language. |

## UI And Dashboard Terms

| Term | Meaning | Avoid Using As |
| --- | --- | --- |
| **Sessions: List** | Session inventory view. | Do not show context drill-down details here. |
| **Sessions: Context** | Turn-by-turn context footprint and forensic drill-down. | Do not route links to a separate page that drops session tabs. |
| **Agents: Roster** | Card/grid view of all taskable agents from the single registry. | Do not reconstruct separately from Workspace/List. |
| **Agents: List** | Tabular subagent management view from the single registry. | Do not omit taskable agents shown in Workspace. |
| **Agents: Workspace** | Visual agent/system map inside the Agents section. | Do not make it a separate top-level registry. |
| **Prompt Performance: Tuning** | Dedicated controls tab for prompt-performance actions/settings. | Do not mix controls into long informational tabs. |
| **Unknown** | Insufficient evidence to classify. | Do not render unknown as healthy, zero cost, or success. |
| **Unavailable** | Current evidence shows the surface cannot be used. | Do not render as configured/healthy. |

## Compatibility Notes

- `ConsolidationPipeline`, `RunMemoryConsolidation`,
  `roboticus memory consolidate`, and `/api/memory/consolidate` are legacy
  compatibility names. Their operator-facing descriptions should call the
  operation **Memory Curation**.
- Existing public command and API paths should keep backward-compatible aliases
  unless a release plan explicitly includes migration.
