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
- `go test -v -run TestLiveSmokeTest .`
- `./roboticus parity-audit --roboticus-dir=../roboticus`

## Matrix

### R-ARCH: Architecture Integrity

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-ARCH-01 | Routes remain thin connectors, not business-logic owners | `internal/api/architecture_test.go` | L0 |
| R-ARCH-02 | Route handlers do not import `internal/agent` directly | `internal/api/architecture_test.go` | L0 |
| R-ARCH-03 | Connectors use `pipeline.RunPipeline()` instead of direct `p.Run()` | `internal/api/architecture_test.go` | L0 |
| R-ARCH-04 | Pipeline does not depend back on `internal/api` or `AppState`-style service bags | `internal/api/architecture_test.go` | L0 |

### R-API: Contract Honesty

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-API-01 | Read paths must not hide DB/query failures behind `200` empty/default payloads | route tests across `internal/api/routes/*_test.go` | L2 |
| R-API-02 | Write paths must reject invalid persisted state instead of accepting it silently | route tests for config/theme/subagent/config-key flows | L2 |
| R-API-03 | Any intentionally unavailable surface must return explicit disabled/unavailable semantics | route tests + smoke | L2/L3 |
| R-API-04 | Stream and non-stream message surfaces preserve behavior parity where required | route/integration tests + smoke | L2/L3 |

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

### R-RT: Routing, Breakers, And Metascores

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-RT-01 | Breaker-tripped providers are excluded from selection | router/unit tests | L1 |
| R-RT-02 | Fallback order is deterministic when primary is unavailable | router/service tests | L1/L2 |
| R-RT-03 | Metascore routing actually drives execution, not just selection | `internal/llm/metascore_routing_test.go` | L2 |
| R-RT-04 | Session-aware and contextual metascore behavior remain effective | metascore fitness tests | L2/L4 |
| R-RT-05 | User weighting / spider-graph weighting changes the winner predictably when advertised | routing-profile tests | L2/L4 |
| R-RT-06 | Metascore-weighted routing improves outcome metrics over baseline on a fixed corpus | efficacy tests | L4 |
| R-RT-07 | OpenAI-compatible tool_call_id serialization includes explicit `content` field on assistant tool-call messages | `internal/llm/client_formats_test.go` | L1 |
| R-RT-08 | Tool result messages serialize `tool_call_id`, `content`, and `name` fields | `internal/llm/client_formats_test.go` | L1 |
| R-RT-09 | IntentMemoryRecall scoring rewards tool use and penalizes confabulation | `internal/llm/exercise_memory_recall_test.go` | L1 |
| R-RT-10 | Every model in CommonIntentBaselines has a MEMORY_RECALL entry | `internal/llm/exercise_memory_recall_test.go` | L1 |

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
| R-TOOLS-03 | Plugin discovery/execute remains stable | plugin tests + route tests | L1/L2 |
| R-TOOLS-04 | MCP management surfaces stay aligned across API/UI/CLI where advertised | MCP tests + smoke | L2/L3 |
| R-TOOLS-05 | Config-protection and action-verification guards block forbidden or fabricated behavior | guard tests + behavior tests | L1/L2/L4 |

### R-AN: Analysis And Recommendations

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-AN-01 | Turn/session analysis returns real, non-placeholder output | route tests | L2 |
| R-AN-02 | Recommendations are generated from live data and not fake-complete shells | route tests + smoke | L2/L3 |
| R-AN-03 | Operator analytics fail honestly on query failure | route tests | L2 |

### R-SCHED: Scheduler And Background Work

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-SCHED-01 | Cron CRUD contract remains stable | route tests | L2 |
| R-SCHED-02 | Cron worker executes due jobs, leases safely, and records runs | scheduler tests + smoke | L1/L2/L3 |
| R-SCHED-03 | UI-created schedule kinds are executable by the worker | integration tests | L2/L3 |
| R-SCHED-04 | Background maintenance tasks do real work or are explicitly disabled | smoke + subsystem tests | L2/L3 |

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

### R-PIPE: Pipeline Stages (v1.0.4)

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-PIPE-01 | Pipeline `Run()` orchestrator delegates to 16 named stage methods | `internal/pipeline/pipeline_test.go` | L1/L2 |
| R-PIPE-02 | All 8 pipeline trace annotations are wired into stage methods | `internal/pipeline/trace_test.go` | L1 |
| R-PIPE-03 | `agentSkills` populated from `SkillMatcher.ListEnabled()`, not empty | `internal/pipeline/pipeline_test.go` | L1 |
| R-PIPE-04 | Cache rejects responses containing `"tool_call"` or `"function_call"` | `internal/pipeline/pipeline_cache_test.go` | L1 |
| R-PIPE-05 | Cache rejects parroting responses (>60% text overlap) | `internal/pipeline/pipeline_cache_test.go` | L1 |
| R-PIPE-06 | `FinancialActionTruthGuard` verifies financial claims against tool output | `internal/pipeline/guards_financial_truth_test.go` | L1/L2 |

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
| R-SOAK-03 | 10/10 soak scenarios pass with local 32B model | behavior soak | L4 |

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

### R-REL: Release Confidence

| ID | Regression Class | Required Coverage | Layer |
| --- | --- | --- | --- |
| R-REL-01 | Live smoke must cover every advertised subsystem | `smoke_test.go` | L3 |
| R-REL-02 | Parity audit must report no remaining required gaps versus frozen Roboticus baseline | parity-audit + tests | L3/L4 |
| R-REL-03 | Feature-complete checklist and docs stay aligned with shipped behavior | doc/release review gate | L4 |
| R-REL-04 | Release artifacts and `SHA256SUMS.txt` are complete and installer-compatible | release gate + artifact validation tests | L4 |
| R-REL-05 | `roboticus.ai` sync succeeds from the Go release source and publishes matching metadata | site-sync dry run + deploy gate | L4 |
| R-REL-06 | Public installer scripts install the Go-based runtime without changing the operator contract unexpectedly | installer smoke on Unix + Windows | L3/L4 |

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
