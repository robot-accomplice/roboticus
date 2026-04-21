# Architecture Gap Report: Go Implementation vs Rust Reference

**Date**: 2026-04-19
**Auditor**: Automated deep audit (3 parallel agents)
**Scope**: Connector-factory compliance, security architecture, tool execution, context management, real-time transport, agentic retrieval architecture
**Reference**: `/Users/jmachen/code/roboticus-rust/ARCHITECTURE.md`

---

## Executive Summary

The Go implementation achieves **full structural compliance** with the connector-factory pattern. The pipeline is the single source of truth for business logic, all 8 entry points use `RunPipeline()`, and architecture tests enforce connector thinness. **All 7 original systemic gaps are now CLOSED** (v1.0.1 + v1.0.2 + v1.0.4), and the broader parity-forensics program has now been distilled into final validated or explicitly deferred dispositions rather than exploratory runtime-classification seams.

v1.0.5 introduced the **agentic retrieval architecture** scaffold — decomposer, router, reranker, context assembly, reflection, and working-memory persistence. v1.0.6 has now carried that scaffold much farther into runtime reality: router-selected retrieval modes influence actual tier retrieval, semantic / procedural / relationship / workflow reads are HybridSearch-first with per-tier `retrieval.path.*` trace attribution, semantic and relationship evidence preserve stronger provenance/freshness signals, the verifier consumes pipeline-computed task hints and claim-level proof obligations, a persisted graph-fact store now exists in production with reusable traversal APIs, and enriched episode distillation now promotes recurring canonical triples into `knowledge_facts`. v1.0.7 then closed the remaining architecture-led retrieval seams with explicit fusion, optional LLM reranking, and semantic FTS cleanup instead of residual heuristic SQL.

The parity-driven remediation effort also clarified several ownership seams that
older architecture docs had left too generic:

- **Request construction is now a first-class architecture seam.** Tool pruning,
  memory preparation, checkpoint restore, and prompt assembly converge into one
  `llm.Request`, and the request builder is now expected to preserve the latest
  user message, align prompt-layer tool guidance with the structured tool list,
  and drop empty compacted history before inference. Baseline/exercise now uses
  that same runtime request path rather than a direct-LLM bypass.
- **Continuity and learning are now explicitly artifact-driven.** Reflection,
  executive growth, checkpoints, and consolidation are expected to consume
  structured turn artifacts (`tool_calls`, `pipeline_traces`,
  `model_selection_events`, structured `episodic_memory.content_json`) instead
  of re-deriving durable state from lossy text summaries.
- **Security/policy truth ownership is sharper.** Stage 8 owns claim
  composition, policy/tool runtime own what actually happened, and
  post-inference guards are no longer allowed to overwrite legitimate
  policy/sandbox denials with fabricated canned outcomes.
- **Webhook ingress ownership is sharper.** Telegram and WhatsApp routes no
  longer own transport JSON parsing; adapters own normalization and WhatsApp
  verification/signature checks, while routes only bridge normalized inbound
  messages into the pipeline.
- **Plugin runtime ownership is sharper.** Daemon startup now owns plugin
  registry construction, directory scan, manifest parsing, init, and
  `AppState.Plugins` wiring. Install-time plugin writes now hot-load into that
  same registry, so plugin install/catalog UX no longer stands in for a missing
  runtime lifecycle. Manifest-backed plugin scripts now also share the same
  core execution contract as skill scripts, closing a policy drift seam at the
  extension boundary.
- **Manual cron execution now shares the durable scheduler lifecycle.** The
  live `/api/cron/{id}/run` path no longer bypasses lease/run-history/retry
  ownership; it delegates through `CronWorker.RunJobNow(...)` and preserves the
  same execution contract as scheduled runs.
- **Model lifecycle policy is now a first-class routing seam.** Live routing is
  no longer allowed to rely purely on metascore and fallback order to avoid bad
  candidates by accident. Per-model lifecycle state (`enabled`, `niche`,
  `disabled`, `benchmark_only`) and role eligibility (`orchestrator`,
  `subagent`) are now explicit, operator-visible, reasoned, and evidenced
  before ranking starts.
- **Operational delegation inventory belongs on the runtime tool surface.**
  Subagent roster and skill inventory are no longer allowed to exist only as
  admin/UI introspection or prompt-side capability snapshots. The live
  tool-pruning/request path must own explicit roster/inventory tools so
  orchestrators can inspect delegation capacity through the same authoritative
  surface they use for other operational decisions.
- **Subagent composition now belongs on the same runtime control plane.**
  Creating or updating subagents is no longer allowed to remain an admin-only
  route concern. The live tool surface now owns first-class subagent
  composition through one authoritative repository, with explicit
  orchestrator-only enforcement.
- **Delegated task lifecycle now belongs on the same runtime control plane.**
  Open delegated work, retry requests, and task-level status inspection are no
  longer allowed to remain embedded in connector-local routes, status sidecars,
  or orchestration-only in-memory structures. The runtime tool surface now owns
  first-class task lifecycle tools backed by one authoritative repository over
  delegated task state and events.
- **Multi-subagent orchestration now uses that same delegated-work control plane.**
  The system no longer treats orchestration as a prompt-only delegation trick
  layered over the loop. Workflow creation, assignment, and lifecycle evidence
  now flow through one authoritative orchestration control surface that writes
  the same `tasks`, `task_events`, and `agent_delegation_outcomes` artifacts
  the runtime already exposes for task inspection and retry.
- **Operator RCA flow is now a canonical diagnostics surface, not a trace dump.**
  The WebUI observability surface is expected to consume `turn_diagnostics`
  summary + events as the authoritative RCA artifact and render them as one
  canonical decision flow. The operator contract is macro-by-default,
  detailed-on-demand, and grouped by task / envelope / routing / execution /
  recovery / outcome seams instead of raw event order. That contract is not
  allowed to disappear when a turn predates canonical diagnostics or the
  diagnostics artifact is missing: the flow surface must keep visible
  macro/detail controls and fall back explicitly to a trace-only narrative
  keyed by the turn id instead of silently collapsing back into the old stage
  dump. The desktop presentation contract is left-to-right and bounded, but
  more importantly it is singular: there is one decision flow, not a stage
  strip beside a second RCA rail. Macro mode uses compact flow blocks only,
  with dense status state consolidated into one narrow top banner instead of a
  row of large tiles. Turn conclusion and health rating should not compete
  with that strip; they may live in a separate bottom banner when space is
  tight. That top banner must use the same severity model as the flow itself:
  degraded status is yellow, latency above one second is yellow, latency above
  one minute is red, and `high` / `critical` pressure is red. The top-line
  `Health` value is not allowed to read like an unrelated hidden score; it
  must be explicitly derived from the aggregate of the category outcomes shown
  in the flow. Those banner chips are also not allowed to rely on insider
  shorthand alone: each top-line metric must expose an explanatory hover/focus
  tooltip so values such as `degraded`, `high`, or `swap 78.8%` can be
  interpreted by an operator without leaving the flow or querying logs. Visible
  node copy must stay utility-first: macro nodes show only the
  single most decision-relevant signal for that node, with duration as the
  default and routing as the explicit exception because the selected model is
  more important than repeating stage identity. Verbose explanation belongs in
  a true floating tooltip layer anchored to pointer/focus position or in
  explicit detail mode rather than permanently expanded text panels. Detail
  mode must preserve ordinality first: operators should read one chronological
  event timeline, with task / envelope / routing / execution / recovery /
  outcome shown as annotations, not as separate buckets that force manual
  reconstruction of sequence. The flow
  container itself must remain
  width-bounded to the usable main-pane area, accounting for persistent chrome
  such as the sidebar, and that bound must come from the real content pane
  rather than raw viewport math. If the decision rail outgrows that space, the
  surface must expose intentional horizontal scrolling contained inside that
  pane. The flow must also preserve causal atomicity for repeat execution:
  when any step runs more than once, the affected block must carry an explicit
  repeat marker and the operator must be able to infer, from the UI alone,
  whether an earlier attempt succeeded, which guard or verifier intervened
  afterward, the exact retry reason, whether the retry reused the same
  model/provider or widened to fallback, and the final outcome. Stale
  trace-only fallback overlays are not allowed to survive session changes,
  expanded-row changes, or collapse/expand transitions once canonical
  diagnostics are available. The conclusion banner is not allowed to merely
  acknowledge that diagnostics exist; it must synthesize what the evidence
  implies about the turn. For degraded or retried turns, that means naming the
  causal event that changed the path (for example a post-success guard retry),
  whether the route widened or stayed the same, and what that implies about the
  likely fault boundary. Flow nodes themselves should also encode outcome
  quality visually: green for clean execution, yellow for concerns or partial
  degradation, and red for broken or clearly failed paths. Those colors must be
  derived from the same persisted RCA evidence rather than ad hoc UI guesses.
- **Trace and diagnostics artifacts must share the same turn identity.**
  Observability is not allowed to infer RCA presence by heuristic timestamp or
  session adjacency. `pipeline_traces.turn_id` and `turn_diagnostics.turn_id`
  must be written from the same authoritative turn record ID on live turns, or
  the operator flow will misclassify fresh canonical-diagnostics turns as
  trace-only fallback.
- **Host resource state is now part of benchmark validity and RCA truth.**
  Baseline runs, prompt-level exercise rows, and live turn diagnostics are not
  allowed to omit the machine state they were executed under. CPU, memory,
  swap, and relevant process RSS snapshots must be captured on the same
  central seams that already own benchmark persistence and inference RCA,
  otherwise the system cannot distinguish a weak model from a saturated host.
- **Agent roster surfaces must share one authoritative subagent projection.**
  The roster view and the editable subagent list are not allowed to drift by
  querying different route-local shapes over the same `sub_agents` corpus. The
  enriched roster projection is now the shared read model, with the roster page
  layering the orchestrator card on top only where that page actually needs it.
- **Skill composition now has to use one shared runtime control plane as well.**
  Creating or updating skills is not allowed to remain a route-only file-write
  helper. The live runtime tool surface, admin/catalog install flow, and skill
  inventory must converge on one authoritative repository that owns both the
  on-disk skill artifact and the `skills` table row.
- **Simple direct tasks must not be widened into heavy autonomous turns by intent alone.**
  The envelope owner is no longer allowed to treat every `task` intent as
  `heavy` regardless of complexity and planned action. When task synthesis says
  `simple` + `execute_directly`, the first-pass request must stay on a focused
  execution envelope with bounded context, bounded tool surface, and
  retrieval only when concrete continuity or evidence signals require it.
  “Task” is too broad a bucket to justify full autonomous tool-bearing ReAct
  behavior by itself.
- **Retrieval for action turns must be evidence-based, not intent-defaulted.**
  Task synthesis and retrieval policy are not allowed to infer
  `retrieval_needed = true` merely because a turn is imperative. Direct
  authoring or file-manipulation requests that do not depend on prior state,
  historical context, or canonically retrieved evidence must be able to stay
  local to the workspace/tool surface. Otherwise the system manufactures
  pressure and autonomy for no gain.
- **Workspace-local vault authoring must be a first-class runtime capability.**
  Obsidian integration is not allowed to remain a prompt hint or an indirectly
  referenced skill. If a vault is configured and sits within the runtime's
  writable workspace/allowlist, the live tool surface must expose an explicit
  vault-authoring capability with semantics aligned to note creation/update.
  Operators should not have to rely on the model inferring that a generic file
  tool plus a prose hint imply safe vault authoring.
- **Capability truth must converge before inference.**
  Task synthesis, skill inventory, runtime skill loading, tool registration,
  prompt guidance, and operator UI are not allowed to maintain separate partial
  truths about what the agent can do. If an enabled skill exists in the
  authoritative inventory, the runtime must either load it into the live
  matcher/tool surface or mark it unavailable for a concrete reason that every
  other layer can see. DB-backed skill catalogs, filesystem-backed runtime
  matchers, and config-gated tool registration must not drift independently.
- **Guard-context temporal atomicity must hold.**
  Cross-turn guards are not allowed to compare a completion against assistant
  content already emitted inside the same turn. `PreviousAssistant` and
  `PriorAssistantMessages` must exclude the in-flight turn's assistant output
  while still preserving current-turn tool results for truth/execution guards.
  Otherwise successful tool-backed confirmations are misclassified as
  repetition and the framework manufactures pointless retry churn.
- **Skill/capability matching must be semantic enough to preserve operator intent.**
  Capability fit is not allowed to be derived from whitespace-splitting raw
  skill names while ignoring enabled skill descriptions, triggers, aliases, or
  punctuation boundaries. Otherwise the system will conclude that `obsidian`
  and `vault` are missing even when `obsidian-vault` is installed and enabled.
- **Intent classification must not widen simple authoring turns on lexical noise.**
  The first-pass task classifier is not allowed to treat generic words like
  `test` inside filenames, titles, or note bodies as sufficient evidence for a
  coding turn. Simple document/note authoring requests must stay on the direct
  execution path unless stronger coding evidence exists.
- **Placeholder assistant scaffolding must be suppressed at the loop boundary.**
  Strings like `[assistant message]` or `[agent message]` are not legitimate
  assistant outputs and must not enter session history, guard comparison, RCA,
  or user-visible results. The loop owner must normalize or drop these
  placeholders before they can trigger repetition churn or contaminate
  diagnostics.

- **Personality setup interviewing must be owned by one shared contract.**
  The onboarding interview is not allowed to drift between API route wording,
  CLI/degraded fallback copy, and prompt-level LLM behavior. Pre-interview
  framing, question ordering, and convergence strategy belong to one shared
  interview contract: prime the operator to imagine a concrete archetype
  first, ask for the agent name as the first explicit question, then use
  repeated and differently-phrased behavioral probes to triangulate intended
  operating style instead of treating one shallow answer as sufficient.

For v1.0.7, the active parity backlog is no longer inferred from the historical
gap sections below. The authoritative remaining scope is:

- [docs/parity-forensics/parity-ledger.md](./parity-forensics/parity-ledger.md)
- [docs/parity-forensics/v1.0.7-roadmap.md](./parity-forensics/v1.0.7-roadmap.md)

The older gap sections remain valuable as closure history and evidence, but
they are not the release-driving backlog anymore.

| Category | Compliant | Gaps |
|----------|-----------|------|
| Connector-Factory Pattern | 8/8 entry points | 0 |
| Pipeline Stage Gating | 16 named stages | 0 |
| Guard Chain | 26 full / 6 stream | 0 |
| Post-Turn Parity (standard/stream) | Enforced by test | 0 |
| Security Claim Composition | Wired (v1.0.2) | **CLOSED** |
| HMAC Trust Boundaries | Active (v1.0.2) | **CLOSED** |
| Context Budget Tiers | L0-L3 config-driven (v1.0.4) | **CLOSED** |
| Memory Injection Guarantee | Two-stage (v1.0.1) | **CLOSED** |
| Topic-Aware Compression | StrategyTopicAware (v1.0.4) | **CLOSED** |
| Feature Parity Across Channels | Documented rationale per preset (v1.0.4) | **CLOSED** |
| Off-Pipeline Surfaces | 3 documented | 0 |
| WebSocket Transport (v1.0.3) | Thin connector | 0 |
| Config Schema Derivation (v1.0.3) | Struct-driven | 0 |
| Pipeline Cache Guards (v1.0.4) | Reject unparsed tool calls | 0 |
| Session-Aware Routing (v1.0.4) | Escalation tracker | 0 |
| Model Lifecycle Policy (v1.0.7) | State + reasoned eligibility filter ahead of metascore | 0 |
| **Agentic Retrieval Architecture (v1.0.5/v1.0.6)** | **Core runtime architecture materially wired** | **cleanup + follow-on gaps remain** |
| **Working Memory Persistence (v1.0.5)** | **Shutdown/startup** | **0** |
| **Post-Turn Reflection (v1.0.5)** | **Episode summaries** | **0** |
| **Verifier/Critic (v1.0.7)** | **Claim-level verifier with structured contradiction + proof diagnostics** | **0** |

### v1.0.6 Agentic Architecture Layers

| Layer | Component | File | Status |
|-------|-----------|------|--------|
| 2 | Query Decomposer | `decomposer.go` | Wired into RetrieveWithMetrics |
| 5 | Procedural Memory | `retrieval_tiers.go` + migration 040 | Enriched schema + learned_skills |
| 8 | Retrieval Router | `router.go` + `daemon_adapters.go` | Wired into retrieval with production intent signals |
| 11 | Reranker | `reranker.go` | Wired into RetrieveWithMetrics |
| 12 | Context Assembly | `context_assembly.go` | Structured evidence with provenance/authority labels |
| 14 | Verifier/Critic | `verifier.go` + `pipeline_stages.go` | Claim-level verifier with retry, task-hint inputs, action-plan and canonical-source checks, freshness gating, subgoal evidence-support checks, and per-intent proof obligations |
| 16 | Reflection | `reflection.go` | Wired into PostTurnIngest |
| — | Working Memory Persistence | `working_persistence.go` | Wired into Daemon Stop/Start |
| 7 | Graph Facts Persistence | `043_knowledge_facts.sql`, `manager.go`, `retrieval_tiers.go`, `graph.go` | Persisted typed relations with provenance/freshness, reusable traversal API, and retrieved first-class evidence with path/impact traversal |

### Remaining Gaps To Full Vision

| Layer | Component | Status |
|-------|-----------|--------|
| 4 | Parallel Retrieval | Closed in the v1.0.7 worktree; routed tiers now fan out concurrently inside the retriever and merge deterministically in router order |
| 3 | Semantic read-path cleanup | Closed in the v1.0.7 worktree; semantic retrieval now uses an enriched category/key/value FTS corpus and a tier-scoped FTS fallback instead of heuristic SQL |

### v1.0.7 Active Architecture-Led Parity Items

| Roadmap ID | Title | Primary architecture seam |
|------------|-------|---------------------------|
| `PAR-008` | Cross-vendor SSE MCP proof | SSE transport confidence must flow through one authoritative named-target validation harness and evidence artifact, with central MCP config conversion plus endpoint-discovery/auth-capable SSE transport semantics, then be backed by more than one real blessed target |

---

## Gap 1: SecurityClaim Resolvers Defined But Never Called

**Severity**: CLOSED
**Rust principle violated**: Section 5 (Clear Boundaries) — "Authority resolution" belongs in Pipeline

**Current state**: Closed in v1.0.6. Stage 8 (`authority_resolution`) is the live owner for `SecurityClaim` composition. The pipeline resolves channel/API/A2A claims through the proper resolver path, attaches the resolved claim to the session, annotates `authority` and `claim_sources` on the trace, and applies threat-caution downgrade on the live path.

**Rust behavior**: Every entry point constructs a proper SecurityClaim via the corresponding resolver. The claim carries through the entire pipeline and is attached to every tool call for audit.

**Fix**: Completed. Remaining work is transport-by-transport classification and broader cross-layer sandbox audit, not basic claim-owner wiring.

---

## Gap 2: API Routes Never Set Input.Claim

**Severity**: CLOSED
**Rust principle violated**: Section 6 (Feature Parity Across Channels) — all channels access same capabilities

**Current state**: Closed in v1.0.6. API-key routes do not need to synthesize `ChannelClaimContext`; under `AuthorityAPIKey`, Stage 8 resolves the claim through `ResolveAPIClaim(...)`. The old route-level `Input.Claim` scaffolding was removed because it obscured the true live owner.

**Rust behavior**: API requests also go through claim resolution (`resolve_api_claim`), producing a SecurityClaim with source tracking.

**Fix**: Completed by making Stage 8 the canonical API claim owner and removing dead route-layer claim placeholders.

---

## Gap 3: HMAC Trust Boundaries Passive — Model Not Instructed

**Severity**: MEDIUM
**Rust principle violated**: Section 4 (Cognitive Scaffold) — "the framework must preserve the model's reasoning chain"

**Current state**: `internal/agent/hmac_boundary.go` implements HMAC-SHA256 signing and verification. `SanitizeModelOutput()` strips forged markers. But:
- The system prompt (`internal/agent/prompt.go`) never mentions trust boundaries
- The model has no instruction to generate or respect boundaries
- Verification only catches markers that happen to be present (passive defense)

**Rust behavior**: System prompt includes boundary instructions. Boundaries are injected between prompt sections. Model output is verified against expected section structure.

**Fix**: Inject HMAC boundary markers between system prompt sections (personality, firmware, tools). Add verification on model output to detect section tampering. This is the Rust `inject_hmac_boundary` / `verify_hmac_boundary` pattern.

---

## Gap 4: Memory Injection Not Guaranteed — CLOSED (v1.0.1)

**Severity**: HIGH → **RESOLVED**
**Rust principle violated**: Section 4 (Cognitive Scaffold)

**Resolution (v1.0.1)**: Complete overhaul of memory injection architecture:
1. Two-stage injection: `RetrieveDirectOnly()` injects only working + ambient;
   all other tiers accessed via query-aware memory index + `recall_memory`/`search_memories` tools
2. Empty memory index injects orientation marker directing model to `search_memories(query)`
3. Query-aware `BuildMemoryIndex()` surfaces topic-matched entries alongside tier-priority top-N
4. Anti-confabulation behavioral contract prevents model from fabricating memories
5. `search_memories(query)` tool (beyond-parity) gives model on-demand FTS5 + LIKE search

**Files changed**: `daemon.go`, `retrieval.go`, `memory_recall.go`, `prompt.go`, `schema.go`
**Tests**: 15 regression tests in `memory_search_test.go`, `retrieval_direct_test.go`, `client_formats_test.go`
**Remaining**: Skill/subagent execution paths still bypass `buildAgentContext()` (tracked separately)

---

## Gap 5: Context Budget Missing Tier System — CLOSED (v1.0.4)

**Severity**: MEDIUM → **RESOLVED**
**Rust principle violated**: Section 4 (Cognitive Scaffold)

**Resolution (v1.0.4)**: Config-driven context budget tiers:
1. `ContextBudget.L0` through `.L3` fields added to config struct with defaults matching Rust (8K, 8K, 16K, 32K)
2. `SoulMaxContextPct` (0.4 default) caps personality budget
3. `ChannelMinimum` ("L1") enforces minimum tier per channel
4. Hardcoded budget percentages in pipeline/agent replaced with config-driven values
5. `EstimateTokens()` replaces all `len/4` heuristics with UTF-8 aware per-script estimation

**Files changed**: `config.go`, `config_defaults.go`, `tokencount.go` (new), 10+ call sites updated

---

## Gap 6: Topic-Aware History Compression Missing — CLOSED (v1.0.4)

**Severity**: MEDIUM → **RESOLVED**
**Rust principle violated**: Section 4 (Cognitive Scaffold)

**Resolution (v1.0.4)**: `StrategyTopicAware` compression strategy:
1. `CompressWithTopicAwareness()` groups messages by topic using Jaccard keyword similarity
2. Current-topic messages preserved in full; off-topic compressed aggressively
3. New `CompressionStrategy` enum value alongside existing `StrategyTruncate` and `StrategyDropLowRelevance`
4. Uses existing embedding infrastructure for topic similarity detection

**Files changed**: `compression.go`, `topic_compression.go` (new)

---

## Gap 7: Feature Parity — Channel Presets Missing Specialist/Skill — CLOSED (v1.0.4)

**Severity**: LOW → **RESOLVED**
**Rust principle violated**: Section 6 (Feature Parity)

**Resolution (v1.0.4)**: All four preset functions (`PresetAPI`, `PresetStreaming`, `PresetChannel`, `PresetCron`) now carry doc comments with explicit "Stage rationale for non-default values" sections documenting *why* each stage is enabled or disabled per preset:
- `PresetAPI`: SpecialistControls/SkillFirst disabled — API clients manage their own specialist UX
- `PresetStreaming`: GuardSetStream (6 guards) — retry-capable guards excluded from streaming; no nickname mid-stream
- `PresetChannel`: SpecialistControls/SkillFirst enabled — interactive specialist creation + trigger-based skills
- `PresetCron`: DedupTracking/Delegation/Shortcuts disabled — scheduler guarantees uniqueness, tasks self-contained

**Fix**: Add doc comments to each preset function documenting the rationale for any disabled stage, matching the Rust architecture's table format.

---

## Model Lifecycle Policy And Routing Eligibility (v1.0.7)

**Severity**: CLOSED
**Architecture principle extended**: eligibility is decided before ranking.

**Current state**: Closed in v1.0.7. The Go implementation now treats model
policy as an explicit architecture seam instead of an accidental byproduct of
metascore tuning. The policy is no longer only an in-memory/config concern; it
has a persistent operator-managed lifecycle store with merge semantics against
configured defaults.

- per-model lifecycle state is configurable and inspectable:
  - `enabled`
  - `niche`
  - `disabled`
  - `benchmark_only`
- per-model role eligibility is configurable and inspectable:
  - orchestrator
  - subagent
- every lifecycle decision may carry:
  - primary reason code
  - secondary reason codes
  - operator-readable reason text
  - evidence references
  - source
- policy is resolved centrally from:
  - configured defaults
  - persisted operator overrides
  - canonical normalization of provider-qualified model specs
- live routing now filters candidates by lifecycle state and role eligibility
  before metascore or heuristic ranking runs
- benchmark/exercise selection uses the same lifecycle policy seam instead of
  a second ad hoc allowlist

**Architectural rule**: metascore is not allowed to stand in for hard policy.
A model that is disabled or benchmark-only must never enter the live routing
pool. A model that is subagent-only must never be considered for
operator-facing orchestration. Ranking happens only inside the surviving
eligible set.

**Why this matters**: the benchmark program has already shown that “installed”
and “live-routable on this hardware” are not the same thing. Without explicit
model policy states, the runtime keeps overloading ranking heuristics to solve
a lifecycle-management problem they were never meant to own.

**Architectural rule**: policy resolution happens once, centrally, and is then
reused by live routing, benchmark selection, diagnostics, and operator/admin
surfaces. State transitions must remain reasoned and evidenced; hidden
blocklists are not an acceptable substitute.

**Documentation follow-through**: the primary C4 document
(`docs/diagrams.md`) now reflects this seam directly. Model lifecycle policy,
benchmark history, canonical turn diagnostics, and the operator-facing
WebSocket/webchannel observability surface are shown as first-class containers
instead of being implied only by code or supplementary rules diagrams.

---

## WebSocket-First Dashboard Architecture (v1.0.3)

**Severity**: N/A (new capability, not a gap)
**Architectural assessment**: COMPLIANT

The v1.0.3 WebSocket-first dashboard replaces all HTTP polling with topic-based subscriptions. Key architectural properties:

1. **Thin connector**: `ws_protocol.go` handles upgrade, ticket validation, and message framing only. No business logic.
2. **Pipeline bridge**: Pipeline stages publish lifecycle events (session start/end, trace, health) to the EventBus. The WS layer subscribes and broadcasts — it does not query or transform.
3. **Ticket authentication**: WS connections require a pre-validated ticket (anti-CSRF, anti-replay). Ticket issuance is in the API route layer; validation is in the WS upgrade handler.
4. **Topic isolation**: `ws_topics.go` defines a registry of subscribable topics. Clients subscribe to specific topics; the server does not broadcast everything to everyone.
5. **Zero polling**: All `setInterval`-based polling removed from dashboard. All state updates arrive via WS push.

This is a transport-layer change. The pipeline remains the single behavioral authority. The WS layer is a delivery connector, analogous to the existing SSE streaming connector.

---

## v1.0.4 Architectural Changes

### Pipeline Stage Extraction
`pipeline.Run()` refactored from 874-line monolith to 16 named stage methods operating on a `pipelineContext` struct. Each stage returns `(*Outcome, error)` or mutates context. Zero behavioral change — all existing tests pass unchanged. This is the first step toward pluggable stage pipelines.

### Security Hardening
- `Store.DB()` deleted — no raw `*sql.DB` access. Architecture test prevents re-introduction.
- Wallet passphrase keystore-only — env var fallback and machine-derived passphrase removed.
- Cache guards reject responses containing unparsed tool call JSON (`"tool_call"`, `"function_call"`).
- All credential config fields (`APIKeyEnv`, `TokenEnv`, `PasswordEnv`) removed — keystore is the only credential store.

### Session-Aware Model Routing
`SessionEscalationTracker` monitors per-session inference quality. On 2+ consecutive failures or quality < 0.3 for 3+ turns, the router escalates to a higher-capability model. This is stateful routing — the router maintains session context, not just per-request signals.

### Financial Action Verification
`FinancialActionTruthGuard` added to the guard chain (26th guard). Before a pipeline response claiming financial success is delivered, the guard verifies the claimed action against tool execution output. Prevents fabricated trading/transfer results.

### Cross-Layer Security / Sandbox Truth Ownership
v1.0.6 also tightened a cross-cutting architectural seam that had been too
implicit in earlier releases:

- Stage 8 is the live owner for `SecurityClaim` composition.
- Policy evaluation and tool/runtime path resolution now share substantially
  tighter sandbox and config-protection semantics.
- Post-inference truth guards have been narrowed so they preserve real
  policy/sandbox denials and failed execution outcomes instead of flattening
  every denial-shaped answer into a fake-capability case.

This is not just "more guards." It is an ownership correction: policy and tool
runtime define what actually happened; post-inference guards are only allowed to
police fabricated narration around that outcome.

### v1.0.6 Final Closure Verdict

The parity program for v1.0.6 is now **decision-complete**.

- Every scoped parity system has a final disposition in
  `docs/parity-forensics/parity-ledger.md`.
- The codebase was materially strengthened in ways that are now backed by both
  runtime tests and durable architecture documentation.
- Prompt compression is **not** part of that strengthening story for this
  release. It failed the corrected history-bearing soak gate and now has an
  explicit benchmark-only disposition: disabled by default, not recommended for
  live use, and retained only for controlled comparison work.

The explicit release-readiness answer for v1.0.6 is:

**Yes** — the code was materially strengthened, and the docs now record that
strengthening truthfully, with the remaining deferred items called out
explicitly rather than hidden behind vague unresolved language.

### Final Audits

#### Architectural Audit

- Single ownership is now explicit for the highest-risk seams:
  request construction, tool pruning, routing truth, checkpoint lifecycle,
  plugin runtime lifecycle, webhook normalization, MCP runtime tool sync, and
  policy/sandbox truth.
- The major shadow-path contradictions found during parity work were either
  removed or demoted out of the live path.
- Durable docs now reflect the validated ownership model rather than the older
  generic container story alone.

#### Functional Audit

- Release-facing claims are supportable by the runtime and tests.
- Channel, scheduler, MCP, cache, and guard behavior now match their documented
  operator contracts closely enough to treat remaining differences as accepted
  deviations or explicit deferrals.
- Prompt compression is clearly benchmark-only and is not being presented as a
  release-ready feature or live optimization.

#### Fitness Audit

- Test coverage now pins the newly closed seams directly, including request
  artifact invariants, selected tool-surface reuse, routing trace truth,
  checkpoint lifecycle, MCP timeout/tool-surface truth, and route-family
  observability contracts.
- Observability surfaces are materially more truthful: canonical route-family
  ownership is explicit, dead MCP transports no longer masquerade as healthy,
  and release notes now function as audited truth surfaces rather than vague
  confidence prose.
- The recent fixes reduced ambiguity and drift overall; they did not add new
  broad subsystems or placeholder abstractions.

### v1.0.7 Root Cause Analysis + Final Parity Goal

v1.0.7 should be treated as the **Root Cause Analysis build** for inference
stalling and fallback behavior, and as the release that takes the remaining
post-v1.0.6 deferred parity edges to final disposition.

The current runtime can show that inference ran long and that a provider
eventually timed out, but it still cannot attribute the delay precisely enough
to distinguish:

- bad route choice
- provider queueing or cold start
- machine saturation
- time-to-first-header failure
- fallback-chain delay

That is a post-v1.0.6 architecture goal, not a v1.0.6 release blocker. The
next release should add first-class per-attempt timing, fallback-chain
attribution, router health inputs, and user-visible stall/reroute status so the
system can explain and react to this class of failure from runtime truth rather
than operator guesswork. It should also take the remaining accepted/deferred
parity edges from v1.0.6 and either retire them, redesign them, or close them
with explicit final rationale rather than leaving them as indefinite release
residue.

### Request Artifact Ownership
v1.0.6 clarified that the final `llm.Request` is itself an architectural
artifact, not just a local implementation detail. The validated ownership is:

- Stage 8 / 8.5 prepare authority and memory artifacts.
- Tool pruning writes the selected structured tool surface before request
  assembly.
- `ContextBuilder.BuildRequest` owns final message assembly, including
  checkpoint digest restore, history compaction/compression, and prompt-layer
  tool roster alignment.
- Routing trace and model-selection audit must reflect that actual request,
  rather than a synthetic user-only approximation.

This closes an important class of migration errors where parity-looking helper
code existed, but the actual inference artifact was still assembled by weaker
or duplicate paths.

### Continuity / Learning Artifact Ownership
v1.0.6 also moved continuity work closer to long-term architecture instead of
release-specific patching:

- checkpoint save/load/prune now share repository-owned lifecycle seams
- reflection reads real turn artifacts instead of zero-duration and adjacency
  proxies
- `episodic_memory` now stores both a compact human-readable summary and a
  structured `content_json` payload
- consolidation prefers the structured payload over reparsing compact text

That is an architectural shift toward durable, machine-consumable turn state.
It materially lowers the risk of future drift caused by helper-specific string
formats becoming accidental downstream contracts.

---

## Compliant Areas (No Gaps)

### Connector-Factory Pattern ✓
All 8 entry points use `pipeline.RunPipeline()`. No business logic in connectors. Architecture tests enforce:
- `TestArchitecture_RoutesDontImportAgent`
- `TestArchitecture_ConnectorFilesInvokeRunPipeline`
- `TestArchitecture_ConnectorsDoNotContainPolicyDecisions`
- `TestArchitecture_ConnectorFilesAreStructurallyThin` (line limits)

### Pipeline Stage Gating ✓
All 13 boolean flags and 4 enums are checked in `Run()`. No unconditional stages.

### Guard Chain ✓
26 guards in Full chain (was 25 — added `FinancialActionTruthGuard` in v1.0.4), 6 in Streaming chain. Cached uses Full. All registered in `DefaultGuardChain()`.

### Post-Turn Parity ✓
Standard and streaming paths both run memory ingest, embedding, observer dispatch, and nickname refinement through the pipeline. Enforced by `TestMandate_StreamingCallsFinalizeStream`.

### Injection Defense ✓
4 layers deployed. L1/L2 in pipeline stage 2 for all entry points. L4 in agent loop after every tool execution. Unicode normalization, homoglyph folding, zero-width stripping.

### Tool Execution ✓
Policy denials soft-fail with structured reason. Error dedup suppresses repeated failures. L4 output scan on every tool result. Sequential execution with loop detection.

### Off-Pipeline Surfaces ✓
3 documented exemptions (interview, session analysis, turn analysis). All use `llmSvc.Complete()` directly for analytics, not agent inference.

---

## Prioritized Fix Order

| Priority | Gap | Effort | Impact |
|----------|-----|--------|--------|
| ~~P0~~ | ~~Gap 4: Memory injection not guaranteed~~ | **CLOSED v1.0.1** | Two-stage injection + search_memories tool |
| ~~P1~~ | ~~Gap 1: SecurityClaim resolvers not wired~~ | **CLOSED v1.0.2** | Resolvers called at stage 8, claim stored on session + trace |
| ~~P1~~ | ~~Gap 5: Context budget missing tier system~~ | **CLOSED v1.0.4** | L0-L3 config-driven, EstimateTokens(), SoulMaxContextPct |
| ~~P2~~ | ~~Gap 3: HMAC boundaries passive~~ | **CLOSED v1.0.2** | Prompt now includes boundary instructions |
| ~~P2~~ | ~~Gap 6: Topic-aware compression missing~~ | **CLOSED v1.0.4** | StrategyTopicAware with Jaccard similarity grouping |
| ~~P3~~ | ~~Gap 2: API routes never set Claim~~ | **CLOSED v1.0.2** | Both API routes now construct ChannelClaimContext |
| ~~P3~~ | ~~Gap 7: Preset doc comments missing~~ | **CLOSED v1.0.4** | All 4 presets carry stage rationale doc comments |

**All 7 original gaps are CLOSED.** That does **not** mean the parity or
architecture program is complete. Open architectural/parity work still remains
in request shaping, MCP transport semantics, cache/replay semantics, and the
cross-cutting scheduler/plugin/channel families tracked in
`docs/parity-forensics/`.
