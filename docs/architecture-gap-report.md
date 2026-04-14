# Architecture Gap Report: Go Implementation vs Rust Reference

**Date**: 2026-04-14 (updated for v1.0.5)
**Auditor**: Automated deep audit (3 parallel agents)
**Scope**: Connector-factory compliance, security architecture, tool execution, context management, real-time transport, agentic retrieval architecture
**Reference**: `/Users/jmachen/code/roboticus-rust/ARCHITECTURE.md`

---

## Executive Summary

The Go implementation achieves **full structural compliance** with the connector-factory pattern. The pipeline is the single source of truth for business logic, all 8 entry points use `RunPipeline()`, and architecture tests enforce connector thinness. **All 7 original systemic gaps are now CLOSED** (v1.0.1 + v1.0.2 + v1.0.4).

v1.0.5 introduces the **agentic retrieval architecture** — a 13-layer system that replaces the previous "embed query → search vector DB" approach with intent-driven routing, evidence filtering, query decomposition, structured context assembly, and post-turn reflection. Working memory persistence across restarts. All layers wired into production.

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
| **Agentic Retrieval Architecture (v1.0.5)** | **7 layers wired** | **0** |
| **Working Memory Persistence (v1.0.5)** | **Shutdown/startup** | **0** |
| **Post-Turn Reflection (v1.0.5)** | **Episode summaries** | **0** |

### v1.0.5 Agentic Architecture Layers

| Layer | Component | File | Status |
|-------|-----------|------|--------|
| 2 | Query Decomposer | `decomposer.go` | Wired into RetrieveWithMetrics |
| 5 | Procedural Memory | `retrieval_tiers.go` + migration 040 | Enriched schema + learned_skills |
| 8 | Retrieval Router | `router.go` | Wired into RetrieveWithMetrics |
| 11 | Reranker | `reranker.go` | Wired into RetrieveWithMetrics |
| 12 | Context Assembly | `context_assembly.go` | Wired into RetrieveWithMetrics |
| 16 | Reflection | `reflection.go` | Wired into PostTurnIngest |
| — | Working Memory Persistence | `working_persistence.go` | Wired into Daemon Stop/Start |

### v1.1.0 Planned Additions (not yet built)

| Layer | Component | Status |
|-------|-----------|--------|
| 4 | Parallel Retrieval | Tiers queried sequentially |
| 7 | Knowledge Graph Persistence | Ephemeral in-memory only |
| 10 | Verifier/Critic | Guards exist; formal verification not yet |
| 11 | LLM-based Reranking | Score-based only in v1.0.5 |

---

## Gap 1: SecurityClaim Resolvers Defined But Never Called

**Severity**: HIGH
**Rust principle violated**: Section 5 (Clear Boundaries) — "Authority resolution" belongs in Pipeline

**Current state**: `internal/core/security_claim.go` defines `ResolveChannelClaim()`, `ResolveAPIClaim()`, and `ResolveA2AClaim()` with proper grant/ceiling composition (`min(max(grants), min(ceilings))`). These are **never called in production**. Instead, authority flows through the simpler `pipeline.ResolveAuthority()` function in `internal/pipeline/config.go` which lacks:
- Threat-based ceiling downgrades
- Multi-source grant composition
- SecurityClaim audit trail on ToolCallRequest

**Rust behavior**: Every entry point constructs a proper SecurityClaim via the corresponding resolver. The claim carries through the entire pipeline and is attached to every tool call for audit.

**Fix**: Wire the three resolvers into the pipeline's authority resolution stage. Replace or augment `ResolveAuthority()` to call the appropriate resolver and produce a `SecurityClaim` that flows through to the policy engine.

---

## Gap 2: API Routes Never Set Input.Claim

**Severity**: MEDIUM
**Rust principle violated**: Section 6 (Feature Parity Across Channels) — all channels access same capabilities

**Current state**: `internal/api/routes/agent.go` and `sessions.go` construct `pipeline.Input{}` with `Claim: nil`. The pipeline's `ResolveAuthority(AuthorityAPIKey, nil)` hardcodes `AuthorityCreator`, bypassing claim composition entirely. This works but creates an inconsistency: API requests skip the security claim pipeline while channel requests go through it.

**Rust behavior**: API requests also go through claim resolution (`resolve_api_claim`), producing a SecurityClaim with source tracking.

**Fix**: Construct a proper `ChannelClaimContext` for API requests (with `SenderInAllowlist: true` since API keys are fully trusted). This ensures all paths produce claims for audit consistency.

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

**All 7 original gaps are CLOSED.** No open architectural gaps remain.
