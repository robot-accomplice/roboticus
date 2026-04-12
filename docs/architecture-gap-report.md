# Architecture Gap Report: Go Implementation vs Rust Reference

**Date**: 2026-04-09
**Auditor**: Automated deep audit (3 parallel agents)
**Scope**: Connector-factory compliance, security architecture, tool execution, context management
**Reference**: `/Users/jmachen/code/roboticus-rust/ARCHITECTURE.md`

---

## Executive Summary

The Go implementation achieves **strong structural compliance** with the connector-factory pattern. The pipeline is the single source of truth for business logic, all 8 entry points use `RunPipeline()`, and architecture tests enforce connector thinness. However, there are **7 systemic gaps** where the Go code diverges from the Rust reference architecture's principles.

| Category | Compliant | Gaps |
|----------|-----------|------|
| Connector-Factory Pattern | 8/8 entry points | 0 |
| Pipeline Stage Gating | 13/13 flags checked | 0 |
| Guard Chain | 25 full / 6 stream | 0 |
| Post-Turn Parity (standard/stream) | Enforced by test | 0 |
| Security Claim Composition | Code exists | **Not wired** |
| HMAC Trust Boundaries | Code exists | **Not active** |
| Context Budget (tool overhead) | Fixed this session | Verify |
| Memory Injection Guarantee | Two-stage (v1.0.1) | **CLOSED** |
| Feature Parity Across Channels | Mostly | **2 gaps** |
| Off-Pipeline Surfaces | 3 documented | 0 |

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

## Gap 5: Context Budget Missing Tier System

**Severity**: MEDIUM
**Rust principle violated**: Section 4 (Cognitive Scaffold) — context budget as a hard constraint with tier allocation

**Current state**: `internal/agent/context.go` uses a flat 8192-token budget. Tool definition overhead was added this session but there's no tier system (L0-L3). All sessions get the same budget regardless of complexity. Personality budget is not capped at a percentage.

**Rust behavior**: Four budget tiers (L0: 8K, L1: 8K, L2: 16K, L3: 32K) mapped to complexity tiers. Personality capped at `soul_max_context_pct` (5%). Channel minimum level enforced. Budget segments are pre-allocated: system prompt → personality → tools → memory → history.

**Fix**: Implement tiered budgets. At minimum, add personality budget capping and segment pre-allocation (system prompt + personality + tools counted before history allocation). Full L0-L3 tier system is a larger follow-up.

---

## Gap 6: Topic-Aware History Compression Missing

**Severity**: MEDIUM
**Rust principle violated**: Section 4 (Cognitive Scaffold) — "Continuity preservation"

**Current state**: `internal/agent/context.go` compacts messages using a single compaction stage (verbatim → selective trim → summarize → emergency drop). Compaction is applied uniformly — no distinction between current-topic messages and off-topic history.

**Rust behavior**: Messages are partitioned by `topic_id`. Current-topic messages are included in full. Off-topic messages are semantically clustered and summarized. This preserves reasoning continuity within the active topic while freeing tokens.

**Fix**: The Go session already stores `topic_tag` on messages (stored in `pipeline_stages.go` line 107). Add topic-aware partitioning to the context builder: current-topic messages get priority, off-topic messages get compressed first.

---

## Gap 7: Feature Parity — Channel Presets Missing Specialist/Skill

**Severity**: LOW
**Rust principle violated**: Section 6 (Feature Parity) — "Disabling a stage for a channel requires a documented rationale"

**Current state**: `PresetChannel()` has `SpecialistControls: true` and `SkillFirstEnabled: true`. `PresetAPI()` has both `false`. `PresetStreaming()` has both `false`. The Rust reference documents exactly which stages differ and why (Table in Section 6).

**Missing**: The Go preset constructors lack doc comments explaining *why* stages are disabled for each preset. The Rust architecture document requires documented rationale.

**Fix**: Add doc comments to each preset function documenting the rationale for any disabled stage, matching the Rust architecture's table format.

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
25 guards in Full chain, 6 in Streaming chain. Cached uses Full. All registered in `DefaultGuardChain()`.

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
| P1 | Gap 5: Context budget missing tier system | Large | Long sessions overflow, tool instructions drowned |
| ~~P2~~ | ~~Gap 3: HMAC boundaries passive~~ | **CLOSED v1.0.2** | Prompt now includes boundary instructions |
| P2 | Gap 6: Topic-aware compression missing | Large | Off-topic history wastes budget |
| ~~P3~~ | ~~Gap 2: API routes never set Claim~~ | **CLOSED v1.0.2** | Both API routes now construct ChannelClaimContext |
| P3 | Gap 7: Preset doc comments missing | Small | Documentation, not code |
