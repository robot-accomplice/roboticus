# Parity Remediation Board

This board tracks code-first parity remediation work against the final Rust
baseline. It is intentionally interruption-safe: every item should show
current status, concrete file targets, and the next handoff step.

## Status Key

- `todo`: not started
- `in_progress`: active implementation
- `blocked`: waiting on prerequisite
- `done`: implemented and covered by tests

## Release Blockers

| ID | Status | Scope | Primary Targets | Notes |
| --- | --- | --- | --- | --- |
| RB1 | in_progress | `update all` parity | `cmd/update.go`, `cmd/update_parity_test.go` | Persistent state, registry manifest support, provider/skills orchestration, config maintenance migrations, and `upgrade all` compatibility coverage are landed. Remaining gap: richer overwrite/resume semantics, OAuth storage maintenance parity, and broader end-to-end coverage. |
| RB2 | todo | `upgrade all` compatibility parity | `cmd/update.go` | Fold into RB1 once shared orchestration exists. |
| RB3 | done | session analysis parity | `internal/api/routes/sessions.go`, `internal/pipeline/analyzer.go` | Heuristic analyzer with 10 session rules + 12 per-turn rules. Structured Tip output matching Rust shape. Tests updated. |
| RB4 | done | turn analysis parity | `internal/api/routes/turn_detail.go`, `internal/pipeline/analyzer.go` | Structured TurnData from context_snapshots + tool_calls. 12 heuristic rules covering budget, memory, tools, cost, quality. |
| RB5 | done | workspace tasks API | `internal/api/server.go`, `internal/api/routes/workspace_tasks.go`, `internal/db/task_events_repo.go` | Workspace task inventory and admin task-event routes are implemented with DB-backed regression coverage. |
| RB6 | done | runtime device lifecycle | `internal/api/routes/runtime.go`, `internal/db/schema.go` | Paired-device list/pair/verify/unpair plus discovered-agent verify are now DB-backed and test-covered. |
| RB7 | done | MCP server show/test + runtime MCP state | `internal/api/routes/mcp.go`, `internal/mcp/` | Runtime MCP summary and MCP server list/show/test surfaces are implemented with route/server coverage. |
| RB8 | done | memory consolidate/reindex | `internal/api/routes/memory.go`, `internal/pipeline/memory_maintenance.go` | Consolidation and ANN reindex endpoints implemented with pipeline-level maintenance helpers and regression coverage. |
| RB9 | done | routing dataset export + model reset | `internal/api/routes/routing_admin.go`, `internal/db/routing_dataset.go`, `internal/llm/quality.go` | Routing dataset export, TSV opt-in behavior, and metascore quality reset are implemented with DB/route/LLM coverage. |
| RB10 | done | migrate import/export | `cmd/migrate.go`, `cmd/migrate_test.go` | Workspace import/export now copies real artifacts with safety checks and a manifest instead of returning placeholder errors. |
| RB11 | done | keystore rekey | `cmd/keystore.go`, `internal/core/keystore.go` | Implemented native rekey flow and regression coverage. |
| RB12 | done | plugins search | `cmd/plugins.go`, shared update-manifest plumbing | Implemented real registry-backed catalog search with regression coverage. |
| RB13 | done | theme catalog install | `internal/api/routes/themes.go`, `internal/api/server.go` | Catalog install route and activation flow implemented with DB-backed installed theme state. |
| RB14 | done | trace search | `internal/api/routes/traces.go`, `internal/api/server.go` | Search route implemented with Rust-style filters and regression coverage. |
| RB15 | done | API route-set parity test | `internal/api/route_parity_test.go` | Walks chi route tree and asserts all ~120 critical endpoints are registered. |
| RB16 | done | CLI tree parity test | `cmd/cli_contract_test.go` | 6 contract tests covering global flags, top-level commands, aliases, subcommand sets, update/upgrade all, schedule/cron alias. |
| RB17 | done | placeholder guard tests | `cmd/parity_placeholder_test.go`, `internal/api/routes/parity_placeholder_test.go` | AST-based scan rejects any string literal containing "not yet implemented" or "placeholder". |
| RB18 | done | blocker-surface smoke coverage | `smoke_test.go` | Added 10 smoke subtests: keystore-status, workspace-tasks, task-events, runtime-devices, runtime-mcp, routing-dataset, traces-search, memory-consolidate, memory-reindex, mcp-servers. |

## Current Focus

### RB1: Update System Parity

Target behavior:
- `update all` must upgrade binary, provider pack, and skills pack
- update workflow must persist state for resumability and change detection
- update workflow must be registry-driven, not binary-only

Planned slices:
1. Add Go-side update state model and persistence
2. Add registry manifest parsing and URL resolution
3. Add provider pack update flow
4. Add skills pack update flow
5. Wire `update all`/`upgrade all` through shared orchestration
6. Add regression coverage for state persistence and orchestration

### Deep Audit Program

Behavior parity audits are now tracked separately in
`docs/deep-parity-audit-matrix.md`.

Use that matrix when evaluating parity-sensitive work:

- do not close an item because the command or route exists
- compare Go behavior against Rust workflow depth
- record missing side effects, maintenance steps, and operator semantics
- add regression coverage only after the real behavior class is understood

## Progress Log

### 2026-04-06

- Created interruption-safe remediation board.
- Re-verified that several earlier “missing” assumptions are stale, so this board reflects only current code-backed blockers.
- Began `RB1` implementation in `cmd/update.go`.
- Added Go-side update state persistence in `~/.roboticus/update_state.json`.
- Added registry manifest parsing and resolution for `ROBOTICUS_REGISTRY_URL` and config-based `update.registry_url`.
- Added provider-pack update flow writing to configured `providers_file` or `~/.roboticus/providers.toml`.
- Added skills-pack update flow writing to configured `skills.directory` or `~/.roboticus/skills`.
- Wired `update all` through shared orchestration so binary, providers, and skills updates now run in one path.
- Added config-maintenance hooks during update for legacy `allowed_models` removal and automatic `[security]` section insertion when absent.
- Added `upgrade all` workflow compatibility coverage so the historical alias exercises the same full orchestration path.
- Added focused regression coverage in `cmd/update_parity_test.go`.
- Verified with `go test ./cmd -count=1`.
- Implemented `keystore rekey` for real by adding a core `Keystore.Rekey(...)` path and CLI wiring.
- Added rekey coverage in `internal/core/keystore_test.go` and `cmd/keystore_rekey_test.go`.
- Implemented `plugins search` against the remote registry manifest instead of returning a placeholder error.
- Added plugin-search coverage in `cmd/plugins_test.go`.
- Implemented theme catalog install route and DB-backed activation of installed catalog themes.
- Added route coverage for catalog listing and install+activate flow.
- Implemented `/api/traces/search` with tool/guard/duration filters.
- Added trace-search regression coverage.
- Implemented `/api/memory/consolidate` and `/api/memory/reindex` using pipeline-owned maintenance helpers so routes stay thin.
- Added regression coverage for on-demand consolidation and ANN reindex.
- Implemented `/api/models/routing-dataset` with JSON + TSV export behavior and privacy redaction by default.
- Implemented `/api/models/reset` to clear metascore quality observations for one model or all models.
- Added routing dataset extraction/summary support in `internal/db/routing_dataset.go`.
- Added DB, route, LLM, and server coverage for routing dataset export and model-score reset.
- Implemented real `migrate import` and `migrate export` flows that move config, providers, keystore, personality files, skills, and runtime DB artifacts with a migration manifest.
- Implemented runtime MCP summary plus MCP server list/show/test routes backed by real config and live connectivity checks.
- Implemented DB-backed runtime device pair/verify/unpair lifecycle and discovered-agent verification.
- Implemented workspace task inventory and admin task-event read surfaces backed by `agent_tasks` and `task_events`.
- Added `docs/deep-parity-audit-matrix.md` to track subsystem-by-subsystem behavior parity instead of surface-only parity.
- Recorded the first deep-audit finding there: the `models` CLI is still materially behind Rust, including missing `exercise`, `suggest`, `reset`, and `baseline` flows and a thinner `scan` implementation.
- Expanded the deep audit from a `models` spot check into a first full-system pass covering CLI, API/runtime, dashboard shape, update orchestration, and operator control-plane semantics.
- Recorded concrete behavior drift in `docs/deep-parity-audit-matrix.md` for config, memory, MCP, auth, schedule, wallet, plugins, channels/integrations, runtime devices, workspace tasks, dashboard, and status surfaces.
- Marked the main release-blocking deep-parity backlog there so implementation can proceed against behavior gaps instead of name-only parity.

## Handoff Notes

If interrupted during `RB1`, the next agent should:
1. Read this board.
2. Inspect `cmd/update.go` and associated update tests.
3. Finish the remaining Rust parity deltas in `RB1` before moving on:
   - richer local-modification handling for provider/skills content
   - stronger resume/idempotency semantics beyond the current persisted state
   - OAuth storage maintenance parity once a persistent Go-side OAuth storage/repair path exists
