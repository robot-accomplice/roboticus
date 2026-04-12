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
