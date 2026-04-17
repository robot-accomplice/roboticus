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
| R-RT-11 | Routing trace annotations are emitted from the actual `llm.Request` selection site, including real message/tool counts, not a synthetic user-only approximation | `internal/llm/routing_trace_test.go` | L1 |
| R-RT-12 | Model-selection events persist the actual routed request's winner and user excerpt when turn/session/channel context is present | `internal/llm/model_selection_event_test.go` | L1 |

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
| R-SOAK-03 | Managed live behavior soak supports `external`, `clone`, and `fresh` modes so copied-state and clean-state lanes can both be exercised without touching the operator's live config or database | `scripts/run-agent-behavior-soak.py` (audit) | A |
| R-SOAK-04 | Prompt compression quality is evaluated as a paired live soak (`off` vs `on`) on isolated configs, with pass→fail drift treated as a release-blocking regression | `scripts/run-prompt-compression-soak.py`, `scripts/prompt_compression_soak_test.go` | L1/L4 |

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
| R-AGENT-138 | M3.2 — semantic retrieval emits `retrieval.path.semantic` annotation: `fts` when the FTS leg matches a stored row, `empty` when no leg matches an unmatchable query, and no annotation in non-search browse modes (recency / empty query) | `internal/agent/memory/retrieval_path_test.go` | L1 |
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
| R-SANDBOX-1 | Less-restrictive: pipeline constructed with `AllowedPaths=["/A"]` propagates to `session.AllowedPaths` and `ToolContext.AllowedPaths`; ValidatePath against `/A/file` permits | `internal/pipeline/sandbox_propagation_test.go` | L1 |
| R-SANDBOX-2 | More-restrictive: pipeline with empty AllowedPaths denies anything outside the workspace; workspace-internal paths still permitted | `internal/pipeline/sandbox_propagation_test.go` | L1 |
| R-SANDBOX-3 | Bidirectional reconfiguration: widening AllowedPaths between sessions affects new sessions only; existing sessions snapshot their AllowedPaths at creation time so a live config reload can't silently widen or narrow active turns' permissions | `internal/pipeline/sandbox_propagation_test.go` | L1 |
| R-SANDBOX-4 | Snapshot isolation: in-place mutation of pipeline.allowedPaths must not retroactively affect existing sessions — guards against the slice-header-sharing foot-gun | `internal/pipeline/sandbox_propagation_test.go` | L1 |
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
| R-AGENT-INSTRUMENT-1 | Agent loop's `think()` method emits INFO-level logs with `last_user` (200-char snippet of the most recent user message in the LLM request) and `content_preview` (200-char snippet of the LLM response) on every turn. Empty `last_user` is a structural signal of the empty-prompt bug class — operators watching live `roboticus serve` can spot it without needing post-mortem trace inspection | `internal/agent/loop.go::think` (instrumentation); audit-only | A |
| R-SOAK-CACHE-TOGGLE-1 | Soak script supports `SOAK_CLEAR_CACHE` (1=wipe semantic_cache before run) and `SOAK_BYPASS_CACHE` (1=send no_cache=true on every request) as orthogonal toggles. Operators can run cache-on AND cache-off soaks against the same build to evaluate cache efficacy AND uncached agent efficacy independently. Pre-v1.0.6 the soak unconditionally replayed cached responses, masking real agent behavior | `scripts/run-agent-behavior-soak.py` (env vars CLEAR_CACHE + BYPASS_CACHE) (audit) | A |

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
