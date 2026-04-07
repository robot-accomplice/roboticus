# Deep Parity Audit Matrix

This matrix shifts parity work from surface matching to behavior matching.
The goal is not "the same commands and routes exist." The goal is "the Go
implementation behaves like the final Rust baseline in operator-relevant ways."

## Audit Standard

Every subsystem must be checked against the Rust code for:

1. Command/route presence
2. Behavioral depth
3. State transitions and side effects
4. Error semantics
5. Operator workflow completeness
6. Regression coverage

Each subsystem should be classified as:

- `aligned`: code-level behavior is materially in parity
- `partial`: surface exists but behavior is thinner or incomplete
- `missing`: baseline Rust behavior is absent in Go
- `unknown`: not yet deeply audited

## Deep Audit Workboard

| Area | Rust Baseline | Go Surface | Status | Key Behavioral Questions |
| --- | --- | --- | --- | --- |
| Update orchestration | `crates/roboticus-cli/src/cli/update/` | `cmd/update.go` | `partial` | Does `update all` perform the full binary/provider/skills/maintenance/restart flow and persist resumable state? |
| Models CLI | `crates/roboticus-cli/src/cli/admin/models.rs` | `cmd/models.go` | `partial` | Does Go support real scan, exercise, suggest, reset, and baseline flows, or just thin config/API wrappers? |
| Sessions CLI/API | `crates/roboticus-cli/src/cli/sessions.rs`, `crates/roboticus-api/src/api/routes/sessions.rs` | `cmd/sessions.go`, `internal/api/routes/sessions.go` | `partial` | Are create/list/show/export/analyze flows equivalent in depth, output semantics, and side effects? |
| Turn analysis | `crates/roboticus-api/src/api/routes/sessions.rs` | `internal/api/routes/turn_detail.go` | `partial` | Does missing-turn analysis fail truthfully, and does analysis include remediation-grade output? |
| Memory CLI/API | `crates/roboticus-cli/src/cli/memory.rs`, Rust memory routes | `cmd/memory.go`, `internal/api/routes/memory.go` | `partial` | Are search/list/maintenance/hygiene behaviors equivalent, including consolidate and reindex semantics? |
| Skills CLI/API | `crates/roboticus-cli/src/cli/admin/skills.rs` | `cmd/skills.go`, `internal/api/routes/turns_skills.go` | `unknown` | Are show/import/export/catalog/install/activate flows present and real? |
| Plugins CLI/API | `crates/roboticus-cli/src/cli/admin/plugins.rs` | `cmd/plugins.go`, `internal/api/routes/plugins.go` | `partial` | Are install/uninstall/enable/disable/info/search/pack behaviors equivalent? |
| MCP CLI/API/runtime | `crates/roboticus-cli/src/cli/mcp.rs`, Rust runtime/admin routes | `cmd/mcp_cmd.go`, `internal/api/routes/mcp.go`, `internal/mcp/` | `partial` | Do runtime state, inspection, and test flows match, not just listing? |
| Runtime devices/pairing | Rust advanced runtime routes | `internal/api/routes/runtime.go` | `partial` | Are device identity, trust state, pair/verify/unpair semantics, and discovery workflows equivalent? |
| Workspace tasks/events | Rust workspace task routes | `internal/api/routes/workspace_tasks.go` | `partial` | Is Go exposing real task lifecycle summaries and event semantics, not raw DB dumps? |
| Config CLI/API | Rust admin config CLI/routes | `cmd/config_cmd.go`, `internal/api/routes/config_apply.go`, `workspace.go` | `unknown` | Are show/get/set/unset/lint/backup/apply behaviors fully aligned and truthful? |
| Wallet CLI/API | `crates/roboticus-cli/src/cli/wallet.rs` | `cmd/wallet_cmd.go`, `internal/api/routes/wallet_routes.go` | `unknown` | Are show/address/balance/sign/send/scan flows equivalent and complete? |
| Schedule/cron CLI/API | `crates/roboticus-cli/src/cli/schedule.rs` | `cmd/schedule.go`, `internal/api/routes/cron.go` | `unknown` | Do list/create/update/lease/worker behaviors align with the Rust operator contract? |
| Security/mechanic CLI | Rust misc/mechanic/security helpers | `cmd/security.go`, `cmd/mechanic.go` | `unknown` | Are health, repair, audit, and recommendation paths materially equivalent? |
| Daemon/service CLI | Rust daemon/service lifecycle | `cmd/service.go`, `cmd/daemon.go` | `unknown` | Are install/start/stop/restart/status/uninstall behaviors complete and platform-correct? |
| Channels CLI/API | Rust channel ops | `cmd/channels.go`, API channel routes | `unknown` | Are list/health/connect/disconnect/replay/dead-letter behaviors equivalent? |
| Auth/OAuth CLI/runtime | Rust OAuth maintenance/update hooks | `cmd/auth.go`, `internal/llm/oauth.go`, `cmd/update.go` | `partial` | Does Go have the same storage repair and post-update auth maintenance? |
| Routing/metascore control plane | Rust model triage/routing utilities | `internal/llm/`, `internal/api/routes/routing_admin.go`, `cmd/models.go` | `partial` | Are reset/export/baseline/exercise loops and operator feedback surfaces equivalent? |

## Current Deep Findings

### Models CLI

Status: `partial`

Observed Rust behavior:

- `models list`
- `models scan`
- `models exercise`
- `models suggest`
- `models reset`
- `models baseline`

Observed Go behavior:

- `models list`
- `models diagnostics`
- `models scan`

Code-backed gap summary:

1. Go has no `models exercise`.
2. Go has no `models suggest`.
3. Go has no `models reset`.
4. Go has no `models baseline`.
5. Go `models scan` is only a thin call to `/api/models/available?validation_level=scan`; Rust directly probes configured providers and formats discovered model inventory.
6. Go `models list` lists available models, while Rust `models list` reports configured model roles plus routing settings.

Remediation implications:

- Treat the entire `models` command family as a behavior-parity workstream, not a subcommand-counting exercise.
- Add contract tests for the missing command tree.
- Add behavior tests for provider probing, baseline/reset loops, and fallback suggestion output.

## Operating Rule

No subsystem should be marked "done" for parity because:

- the command name exists
- the route exists
- the output shape looks similar

It is only done when the Go code supports the same operator workflow class as
the final Rust baseline and regression tests enforce that claim.
