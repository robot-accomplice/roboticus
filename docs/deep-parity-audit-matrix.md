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
| Skills CLI/API | `crates/roboticus-cli/src/cli/admin/skills.rs` | `cmd/skills.go`, `internal/api/routes/turns_skills.go` | `partial` | Are show/import/export/catalog/install/activate flows present and real? |
| Plugins CLI/API | `crates/roboticus-cli/src/cli/admin/plugins.rs` | `cmd/plugins.go`, `internal/api/routes/plugins.go` | `partial` | Are install/uninstall/enable/disable/info/search/pack behaviors equivalent? |
| MCP CLI/API/runtime | `crates/roboticus-cli/src/cli/mcp.rs`, Rust runtime/admin routes | `cmd/mcp_cmd.go`, `internal/api/routes/mcp.go`, `internal/mcp/` | `partial` | Do runtime state, inspection, and test flows match, not just listing? |
| Runtime devices/pairing | Rust advanced runtime routes | `internal/api/routes/runtime.go` | `partial` | Are device identity, trust state, pair/verify/unpair semantics, and discovery workflows equivalent? |
| Workspace tasks/events | Rust workspace task routes | `internal/api/routes/workspace_tasks.go` | `partial` | Is Go exposing real task lifecycle summaries and event semantics, not raw DB dumps? |
| Config CLI/API | Rust admin config CLI/routes | `cmd/config_cmd.go`, `internal/api/routes/config_apply.go`, `workspace.go` | `partial` | Are show/get/set/unset/lint/backup/apply behaviors fully aligned and truthful? |
| Wallet CLI/API | `crates/roboticus-cli/src/cli/wallet.rs` | `cmd/wallet_cmd.go`, `internal/api/routes/wallet_routes.go` | `partial` | Are show/address/balance/sign/send/scan flows equivalent and complete? |
| Schedule/cron CLI/API | `crates/roboticus-cli/src/cli/schedule.rs` | `cmd/schedule.go`, `internal/api/routes/cron.go` | `partial` | Do list/create/update/lease/worker behaviors align with the Rust operator contract? |
| Security/mechanic CLI | Rust misc/mechanic/security helpers | `cmd/security.go`, `cmd/mechanic.go` | `partial` | Are health, repair, audit, and recommendation paths materially equivalent? |
| Daemon/service CLI | Rust daemon/service lifecycle | `cmd/service.go`, `cmd/daemon.go` | `partial` | Are install/start/stop/restart/status/uninstall behaviors complete and platform-correct? |
| Channels CLI/API | Rust channel ops | `cmd/channels.go`, API channel routes | `partial` | Are list/health/connect/disconnect/replay/dead-letter behaviors equivalent? |
| Auth/OAuth CLI/runtime | Rust OAuth maintenance/update hooks | `cmd/auth.go`, `internal/llm/oauth.go`, `cmd/update.go` | `partial` | Does Go have the same storage repair and post-update auth maintenance? |
| Routing/metascore control plane | Rust model triage/routing utilities | `internal/llm/`, `internal/api/routes/routing_admin.go`, `cmd/models.go` | `partial` | Are reset/export/baseline/exercise loops and operator feedback surfaces equivalent? |
| Status/health CLI | `crates/roboticus-cli/src/cli/status.rs` | `cmd/status.go` | `partial` | Does Go surface the same online/offline operator summary and dependent runtime state? |
| Dashboard/Web UI | `crates/roboticus-api/src/dashboard.rs` + modular assets | `internal/api/dashboard_spa.html`, `internal/api/dashboard.go` | `partial` | Does the Go dashboard expose the same operator workflows and regression hooks as the Rust dashboard? |

## Current Deep Findings

### Severity Guide

- `P1`: release-blocking parity gap
- `P2`: meaningful operator workflow drift
- `P3`: lower-priority drift or maintainability risk that still affects parity confidence

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

### Config CLI

Status: `partial`

Evidence:

- Go `config set` and `config unset` in `cmd/config_cmd.go` mutate TOML with
  leaf-key string replacement only.
- Rust `config` logic in `crates/roboticus-cli/src/cli/admin/config.rs`
  parses and mutates TOML structurally, preserves nested paths, and supports
  proper lint semantics.

Code-backed gap summary:

1. `[P1]` Go `config set` is not path-aware. It strips `models.primary` down to
   `primary` and rewrites the first matching leaf key in the raw file.
2. `[P1]` Go `config unset` has the same leaf-key problem and can delete the
   wrong entry when the same leaf exists in multiple sections.
3. `[P2]` Go `config get` reads from local `viper` state, not the live runtime
   first and then the on-disk TOML fallback as Rust does.
4. `[P2]` Go uses `validate` with a `lint` alias, while Rust has a first-class
   `lint` flow with explicit file targeting and no-apply semantics.

Remediation implications:

- Replace the line-based config mutation approach with structured TOML
  navigation and serialization.
- Add regression cases for nested paths, duplicate leaf keys, arrays, booleans,
  numbers, and no-apply behavior.

### Memory CLI

Status: `partial`

Evidence:

- Go `cmd/memory.go` exposes `working`, `episodic`, `semantic`, `search`,
  `stats`.
- Rust `crates/roboticus-cli/src/cli/memory.rs` exposes `list`, `search`,
  `consolidate`, `reindex` and uses tier-aware semantics like required session
  IDs for working memory.

Code-backed gap summary:

1. `[P1]` Go is missing CLI `memory consolidate`.
2. `[P1]` Go is missing CLI `memory reindex`.
3. `[P2]` Go `memory working` calls `/api/memory/working` globally, while Rust
   requires a session for working-memory inspection.
4. `[P2]` Go `memory semantic` only hits `/api/memory/semantic` and does not
   expose Rust's category-targeted behavior class.
5. `[P2]` Go `memory stats` is a local count loop over endpoints, not a real
   operator workflow from the Rust baseline.

Remediation implications:

- Rebuild the Go memory CLI around the Rust command model: `list`, `search`,
  `consolidate`, `reindex`.
- Add strict session/category flag handling and operator-facing output tests.

### MCP CLI

Status: `partial`

Evidence:

- Go `cmd/mcp_cmd.go` manages live connections directly.
- Rust `crates/roboticus-cli/src/cli/mcp.rs` treats the CLI as a server
  management interface: list configured servers, print TOML snippets for
  add/remove, and test a named configured server.

Code-backed gap summary:

1. `[P1]` Go `mcp list` shows active connections, not configured MCP servers.
2. `[P1]` Go has `connect`/`disconnect`, but Rust has `add`/`remove` contract
   semantics that preserve config-driven server management.
3. `[P2]` Go `mcp show` scrapes `/api/mcp/tools` and infers server details from
   tool payloads instead of hitting the dedicated server-inspection route first.
4. `[P2]` Go `mcp test` performs a connect/list/disconnect dance, while Rust
   calls `/api/mcp/servers/{name}/test` directly.

Remediation implications:

- Align Go CLI semantics to configured-server management, not transient
  connection management.
- Keep connection commands only if explicitly de-advertised as Go-only extras.

### Auth/OAuth

Status: `partial`

Evidence:

- Go `cmd/auth.go` is API-key management against `/api/providers/{provider}/key`.
- Rust CLI args define OAuth-oriented auth commands with provider flags and
  client ID support.

Code-backed gap summary:

1. `[P1]` Go `auth login` is API-key entry, not OAuth login.
2. `[P2]` Go `auth status` reports configured `api_key_env` fields, not actual
   token status.
3. `[P2]` Go has no CLI surface for OAuth client-ID override behavior.
4. `[P1]` Update parity is still incomplete because Go does not yet match the
   Rust post-update OAuth storage maintenance path.

Remediation implications:

- Decide whether Goboticus will preserve the Rust OAuth contract or explicitly
  replace it. Until then, auth parity should be considered incomplete.

### Schedule CLI

Status: `partial`

Evidence:

- Go `cmd/schedule.go` exposes create/delete/history aliases on top of cron.
- Rust `crates/roboticus-cli/src/cli/schedule.rs` centers on `list`, `run`,
  and `recover`.

Code-backed gap summary:

1. `[P1]` Go is missing the Rust `schedule recover` operator workflow.
2. `[P2]` Go `schedule run` only accepts an ID path, while Rust resolves both
   job names and IDs.
3. `[P2]` Go `schedule list` output is thinner than Rust's intent/last-run/error
   oriented view.
4. `[P3]` Go has extra create/delete/history flows, which are not themselves a
   bug, but they mask the missing recovery path if parity is judged superficially.

Remediation implications:

- Add `schedule recover`.
- Make `schedule run` resolve names and IDs.
- Add regression coverage for paused-job recovery semantics.

### Wallet CLI

Status: `partial`

Evidence:

- Go `cmd/wallet_cmd.go` just proxies `/api/wallet/balance` and
  `/api/wallet/address`, then merges maps for `show`.
- Rust `crates/roboticus-cli/src/cli/wallet.rs` presents treasury, swap, tax,
  accounting, queue, and seed-exercise readiness information.

Code-backed gap summary:

1. `[P2]` Go `wallet show` is structurally much thinner than Rust's wallet
   operator summary.
2. `[P2]` Go has no dedicated formatting or workflow around treasury/revenue
   readiness signals even if the API may expose some of them.

Remediation implications:

- Promote `wallet show` from a raw merged dump to an operator summary matching
  the Rust behavior class.

### Plugins CLI

Status: `partial`

Evidence:

- Go `plugins search` is now real.
- Go `plugins uninstall` still prints manual instructions.
- Rust `crates/roboticus-cli/src/cli/admin/plugins.rs` has real install-source
  detection, manifest validation, dependency checks, and uninstall behavior.

Code-backed gap summary:

1. `[P1]` Go `plugins uninstall` is still a manual instruction shell, not a
   real uninstall flow.
2. `[P2]` Go install path is catalog-oriented and thinner than Rust's local
   directory/archive/catalog install-source handling.
3. `[P2]` Go lacks Rust's companion-skill and dependency-check behavior class.

Remediation implications:

- Finish plugin lifecycle parity; search alone does not close this workstream.

### Channels And Integrations CLI

Status: `partial`

Evidence:

- Go has `channels list|test|dead-letter|replay`.
- Rust has both `channels` and `integrations` workflow families, including
  health/connect/disconnect guidance.

Code-backed gap summary:

1. `[P1]` Go is missing the `integrations` command family.
2. `[P2]` Go has no CLI health summary for integrations.
3. `[P2]` Go has no config-snippet guidance flow for connect/disconnect.

Remediation implications:

- Implement `integrations list|test|health|connect|disconnect` or explicitly
  retire that contract before claiming parity.

### Status CLI

Status: `partial`

Evidence:

- Go `cmd/status.go` checks `/api/health` and `/api/agent/status` only.
- Rust `crates/roboticus-cli/src/cli/status.rs` builds a broader online/offline
  summary from config, sessions, skills, cron, cache, and wallet state.

Code-backed gap summary:

1. `[P2]` Go has no graceful offline-mode operator response like Rust.
2. `[P2]` Go status omits sessions/skills/cron/cache/wallet summary context.

Remediation implications:

- Bring Go status up to the Rust summary class and add offline-path tests.

### Runtime Devices

Status: `partial`

Evidence:

- Go `internal/api/routes/runtime.go` returns `"device_id":
  "roboticus-local-device"` as the runtime identity.

Code-backed gap summary:

1. `[P1]` Device identity is still hardcoded instead of being backed by a real
   persisted/public-key identity model.
2. `[P2]` Go does not yet expose the richer trust identity material present in
   Rust.

Remediation implications:

- Implement persistent local device identity and expose the real public-key or
  fingerprint surface.

### Workspace Tasks And Events

Status: `partial`

Evidence:

- Go `internal/api/routes/workspace_tasks.go` returns basic task rows plus
  subtask counts, and dumps task events as recent rows.
- Rust has a richer workspace dashboard task control plane.

Code-backed gap summary:

1. `[P2]` Go task summaries are too raw and do not compute the richer active and
   recent task views present in Rust.
2. `[P2]` Event data is exposed, but the route is still a raw feed rather than
   a dashboard-grade operational summary.
3. `[P2]` Runtime producers for `task_events` are still sparse, so parity on
   event semantics is not yet proven.

Remediation implications:

- Add task-summary derivations and verify runtime producers write the expected
  event stream.

### Session And Turn Analysis

Status: `partial`

Evidence:

- Go routes now run heuristic analysis and LLM summarization.
- Rust analysis routes do the same class of work with a dedicated helper.

Code-backed gap summary:

1. `[P2]` This gap is smaller than it was, but Go still needs parity validation
   for output richness, token/cost reporting, and prompt content quality.
2. `[P2]` Session analysis currently aggregates per-turn tips locally; this
   needs behavioral tests against Rust examples, not just surface tests.

Remediation implications:

- Keep this in the deep-audit program until side-by-side behavioral fixtures are
  added.

### Update Orchestration

Status: `partial`

Code-backed gap summary:

1. `[P1]` Go still lacks the full Rust post-update maintenance contract,
   especially OAuth maintenance and broader restart/repair choreography.
2. `[P2]` Resumability and local-modification handling are still weaker than the
   Rust baseline.

### Dashboard/Web UI

Status: `partial`

Evidence:

- Rust dashboard is modular in `crates/roboticus-api/src/dashboard.rs` and
  includes dedicated regression hooks and tests for sections/pages.
- Go serves one monolithic `/Users/jmachen/code/goboticus/internal/api/dashboard_spa.html`.

Code-backed gap summary:

1. `[P2]` Go's dashboard is much harder to keep in parity because UI logic is a
   single monolith instead of Rust's modular asset composition.
2. `[P2]` Go has CSP and basic HTML tests, but not Rust-style section/regression
   hooks for page-level workflow coverage.
3. `[P2]` Web UI drift is likely to recur unless dashboard behavior is audited
   page by page against the backing API contracts.

Remediation implications:

- Add page-level dashboard tests and consider decomposing the Go dashboard into
  modules that mirror the Rust information architecture.

## Release-Blocking Backlog From Deep Audit

1. Rebuild the `models` CLI to support `exercise`, `suggest`, `reset`, and
   `baseline`, and make `scan` probe providers directly.
2. Replace line-based config mutation with structured TOML path mutation.
3. Align memory CLI semantics with Rust: `list`, `search`, `consolidate`,
   `reindex`, and strict session/category targeting.
4. Implement the `integrations` command family and `schedule recover`.
5. Finish plugin lifecycle parity with a real uninstall flow and richer install
   source handling.
6. Replace API-key-only auth semantics with the final intended OAuth/provider
   contract, or explicitly de-scope and de-advertise it.
7. Replace the hardcoded runtime device identity with a persisted identity model.
8. Finish update-orchestration parity for post-update maintenance.

## Next Audit Passes

To complete the deep audit rather than the first broad pass, the next sweeps
should focus on:

1. API payload shape comparisons for dashboard-critical endpoints
2. Wallet runtime behavior beyond CLI output formatting
3. Scheduler worker/lease behavior and daemon lifecycle behavior
4. Skills/plugins import/export/install behavior under failure conditions
5. Side-by-side dashboard workflow testing for overview, sessions, memory,
   skills, scheduler, metrics, workspace, settings, and wallet pages

## Operating Rule

No subsystem should be marked "done" for parity because:

- the command name exists
- the route exists
- the output shape looks similar

It is only done when the Go code supports the same operator workflow class as
the final Rust baseline and regression tests enforce that claim.
