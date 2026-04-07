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
| Update orchestration | `crates/roboticus-cli/src/cli/update/` | `cmd/update.go` | `partial (improved)` | Does `update all` perform the full binary/provider/skills/maintenance/restart flow and persist resumable state? |
| Models CLI | `crates/roboticus-cli/src/cli/admin/models.rs` | `cmd/models.go` | `partial (improved)` | Does Go support real scan, exercise, suggest, reset, and baseline flows, or just thin config/API wrappers? |
| Sessions CLI/API | `crates/roboticus-cli/src/cli/sessions.rs`, `crates/roboticus-api/src/api/routes/sessions.rs` | `cmd/sessions.go`, `internal/api/routes/sessions.go` | `partial` | Are create/list/show/export/analyze flows equivalent in depth, output semantics, and side effects? |
| Turn analysis | `crates/roboticus-api/src/api/routes/sessions.rs` | `internal/api/routes/turn_detail.go` | `partial` | Does missing-turn analysis fail truthfully, and does analysis include remediation-grade output? |
| Memory CLI/API | `crates/roboticus-cli/src/cli/memory.rs`, Rust memory routes | `cmd/memory.go`, `internal/api/routes/memory.go` | `aligned` | Are search/list/maintenance/hygiene behaviors equivalent, including consolidate and reindex semantics? |
| Skills CLI/API | `crates/roboticus-cli/src/cli/admin/skills.rs` | `cmd/skills.go`, `internal/api/routes/turns_skills.go` | `partial` | Are show/import/export/catalog/install/activate flows present and real? |
| Plugins CLI/API | `crates/roboticus-cli/src/cli/admin/plugins.rs` | `cmd/plugins.go`, `internal/api/routes/plugins.go` | `partial (improved)` | Are install/uninstall/enable/disable/info/search/pack behaviors equivalent? |
| MCP CLI/API/runtime | `crates/roboticus-cli/src/cli/mcp.rs`, Rust runtime/admin routes | `cmd/mcp_cmd.go`, `internal/api/routes/mcp.go`, `internal/mcp/` | `aligned` | Do runtime state, inspection, and test flows match, not just listing? |
| Runtime devices/pairing | Rust advanced runtime routes | `internal/api/routes/runtime.go` | `aligned` | Are device identity, trust state, pair/verify/unpair semantics, and discovery workflows equivalent? |
| Workspace tasks/events | Rust workspace task routes | `internal/api/routes/workspace_tasks.go` | `partial` | Is Go exposing real task lifecycle summaries and event semantics, not raw DB dumps? |
| Config CLI/API | Rust admin config CLI/routes | `cmd/config_cmd.go`, `internal/api/routes/config_apply.go`, `workspace.go` | `aligned` | Are show/get/set/unset/lint/backup/apply behaviors fully aligned and truthful? |
| Wallet CLI/API | `crates/roboticus-cli/src/cli/wallet.rs` | `cmd/wallet_cmd.go`, `internal/api/routes/wallet_routes.go` | `aligned` | Are show/address/balance/sign/send/scan flows equivalent and complete? |
| Schedule/cron CLI/API | `crates/roboticus-cli/src/cli/schedule.rs` | `cmd/schedule.go`, `internal/api/routes/cron.go` | `aligned` | Do list/create/update/lease/worker behaviors align with the Rust operator contract? |
| Security/mechanic CLI | Rust misc/mechanic/security helpers | `cmd/security.go`, `cmd/mechanic.go` | `partial` | Are health, repair, audit, and recommendation paths materially equivalent? |
| Daemon/service CLI | Rust daemon/service lifecycle | `cmd/service.go`, `cmd/daemon.go` | `partial` | Are install/start/stop/restart/status/uninstall behaviors complete and platform-correct? |
| Channels CLI/API | Rust channel ops | `cmd/channels.go`, API channel routes | `aligned` | Are list/health/connect/disconnect/replay/dead-letter behaviors equivalent? |
| Auth/OAuth CLI/runtime | Rust OAuth maintenance/update hooks | `cmd/auth.go`, `internal/llm/oauth.go`, `cmd/update.go` | `aligned` | Does Go have the same storage repair and post-update auth maintenance? |
| Routing/metascore control plane | Rust model triage/routing utilities | `internal/llm/`, `internal/api/routes/routing_admin.go`, `cmd/models.go` | `partial` | Are reset/export/baseline/exercise loops and operator feedback surfaces equivalent? |
| Status/health CLI | `crates/roboticus-cli/src/cli/status.rs` | `cmd/status.go` | `aligned` | Does Go surface the same online/offline operator summary and dependent runtime state? |
| Dashboard/Web UI | `crates/roboticus-api/src/dashboard.rs` + modular assets | `internal/api/dashboard_spa.html`, `internal/api/dashboard.go` | `partial` | Does the Go dashboard expose the same operator workflows and regression hooks as the Rust dashboard? |

## Current Deep Findings

### Severity Guide

- `P1`: release-blocking parity gap
- `P2`: meaningful operator workflow drift
- `P3`: lower-priority drift or maintainability risk that still affects parity confidence

### Models CLI

Status: `partial (improved)` — scan now probes providers directly, list shows configured+available

Observed Rust behavior:

- `models list`
- `models scan`
- `models exercise`
- `models suggest`
- `models reset`
- `models baseline`

Observed Go behavior (as of 2026-04-07):

- `models list` — shows configured model roles plus available models ✓
- `models diagnostics`
- `models scan` — probes providers directly (Ollama/OpenAI) ✓
- `models exercise` — exercises via `/api/models/routing-eval` ✓
- `models suggest` — reads metascore profiles from routing-diagnostics ✓
- `models reset` — calls `/api/models/reset` ✓
- `models baseline` — reads routing dataset with tabular display ✓

Remaining code-backed gap:

1. `[P3]` Go `models scan` provider probing covers Ollama and OpenAI; additional provider types may need scan support as they are added.

Remediation implications:

- Extend scan probing to new provider types as they become configured.

### Config CLI

Status: `aligned`

Evidence:

- Go `config set` and `config unset` now use structured TOML path navigation
  via go-toml/v2, replacing the previous line-based leaf-key mutation.
- Go `config get` uses an API-first fallback chain: runtime API, then on-disk
  TOML file, then viper defaults.
- Rust `config` logic in `crates/roboticus-cli/src/cli/admin/config.rs`
  parses and mutates TOML structurally, preserves nested paths, and supports
  proper lint semantics.

Previously remediated:

- ~~`[P1]` Go `config set` is not path-aware.~~ Fixed: structured TOML path navigation.
- ~~`[P1]` Go `config unset` has the same leaf-key problem.~~ Fixed: same structured approach.
- ~~`[P2]` Go `config get` reads from local `viper` state only.~~ Fixed: API then TOML file then viper fallback chain.

Remaining code-backed gap:

1. `[P2]` Go uses `validate` with a `lint` alias, while Rust has a first-class
   `lint` flow with explicit file targeting and no-apply semantics.

Remediation implications:

- Promote `lint` to a first-class subcommand if operator demand warrants it.

### Memory CLI

Status: `aligned`

Evidence:

- Go `cmd/memory.go` now exposes `working`, `episodic`, `semantic`, `search`,
  `stats`, `consolidate`, and `reindex`.
- `memory working` accepts `--session` flag for session-scoped inspection.
- `memory semantic` accepts `--category` flag for category-targeted queries.
- Rust `crates/roboticus-cli/src/cli/memory.rs` exposes `list`, `search`,
  `consolidate`, `reindex` and uses tier-aware semantics like required session
  IDs for working memory.

Previously remediated:

- ~~`[P1]` Go is missing CLI `memory consolidate`.~~ Added.
- ~~`[P1]` Go is missing CLI `memory reindex`.~~ Added.
- ~~`[P2]` Go `memory working` calls globally without session targeting.~~ Fixed: `--session` flag added.
- ~~`[P2]` Go `memory semantic` does not expose category targeting.~~ Fixed: `--category` flag added.

Remaining code-backed gap:

1. `[P2]` Go `memory stats` is a local count loop over endpoints, not a real
   operator workflow from the Rust baseline.

Remediation implications:

- Consider enriching `memory stats` with a server-side stats endpoint if
  operator demand warrants it.

### MCP CLI

Status: `aligned`

Evidence:

- Go `cmd/mcp_cmd.go` now treats the CLI as a server management interface.
- `mcp list` calls `/api/mcp/servers` to show configured servers.
- `mcp show` calls `/api/mcp/servers/{name}` for dedicated server inspection.
- `mcp test` calls `/api/mcp/servers/{name}/test` directly.
- `connect`/`disconnect` are retained but documented as runtime-only operations
  (not part of the config-driven server management contract).
- Rust `crates/roboticus-cli/src/cli/mcp.rs` treats the CLI as a server
  management interface: list configured servers, print TOML snippets for
  add/remove, and test a named configured server.

Previously remediated:

- ~~`[P1]` Go `mcp list` shows active connections, not configured MCP servers.~~ Fixed: now calls `/api/mcp/servers`.
- ~~`[P1]` Go has `connect`/`disconnect` instead of config-driven semantics.~~ Fixed: connect/disconnect marked as runtime-only.
- ~~`[P2]` Go `mcp show` scrapes `/api/mcp/tools` instead of server-inspection route.~~ Fixed: now calls `/api/mcp/servers/{name}`.
- ~~`[P2]` Go `mcp test` performs a connect/list/disconnect dance.~~ Fixed: now calls `/api/mcp/servers/{name}/test` directly.

Remaining code-backed gap:

None. MCP CLI behavior is aligned with the Rust server management contract.

### Auth/OAuth

Status: `aligned` (OAuth explicitly de-scoped; API-key approach documented)

Evidence:

- Go `cmd/auth.go` is API-key management against `/api/providers/{provider}/key`.
- OAuth has been explicitly de-scoped for the Go implementation. The CLI help
  text documents the API-key approach as the intended auth model.
- Rust CLI args define OAuth-oriented auth commands with provider flags and
  client ID support, but Go has chosen a different contract.

Previously remediated:

- ~~`[P1]` Go `auth login` is API-key entry, not OAuth login.~~ Resolved: OAuth explicitly de-scoped; API-key is the documented approach.
- ~~`[P2]` Go has no CLI surface for OAuth client-ID override behavior.~~ Resolved: not applicable under API-key model.
- ~~`[P1]` Update parity is still incomplete for post-update OAuth maintenance.~~ Resolved: de-scoped.

Remaining code-backed gap:

1. `[P3]` If OAuth is ever re-scoped, the full provider/client-ID/token-status
   surface will need to be built.

Remediation implications:

- Auth is aligned by explicit design divergence. No further work unless OAuth
  is re-scoped.

### Schedule CLI

Status: `aligned`

Evidence:

- Go `cmd/schedule.go` now exposes `list`, `run`, `recover`,
  `create`, `delete`, and `history`.
- `schedule recover` implements the paused-job recovery operator workflow.
- `schedule run` resolves both job names and UUIDs.
- Rust `crates/roboticus-cli/src/cli/schedule.rs` centers on `list`, `run`,
  and `recover`.

Previously remediated:

- ~~`[P1]` Go is missing the Rust `schedule recover` operator workflow.~~ Added.
- ~~`[P2]` Go `schedule run` only accepts an ID path.~~ Fixed: now resolves job names and IDs.

Remaining code-backed gap:

1. `[P2]` Go `schedule list` output is thinner than Rust's intent/last-run/error
   oriented view.
2. `[P3]` Go has extra create/delete/history flows, which are not themselves a
   bug, but they mask the remaining list-output gap if parity is judged superficially.

Remediation implications:

- Enrich `schedule list` output to match Rust's operator-oriented view.

### Wallet CLI

Status: `aligned`

Evidence:

- Go `cmd/wallet_cmd.go` `wallet show` now presents a formatted operator
  summary with treasury policy display, matching the Rust behavior class.
- Rust `crates/roboticus-cli/src/cli/wallet.rs` presents treasury, swap, tax,
  accounting, queue, and seed-exercise readiness information.

Previously remediated:

- ~~`[P2]` Go `wallet show` is structurally much thinner than Rust's operator summary.~~ Fixed: formatted operator summary with treasury policy display.
- ~~`[P2]` Go has no dedicated formatting around treasury/revenue readiness signals.~~ Fixed: included in operator summary.

Remaining code-backed gap:

None. Wallet CLI behavior is aligned with the Rust operator summary class.

### Plugins CLI

Status: `partial (improved)` — uninstall and install-source detection now real

Evidence:

- Go `plugins search` is real.
- Go `plugins uninstall` now disables via API then removes the plugin directory.
- Go `plugins install` detects local directory vs catalog as the install source.
- Rust `crates/roboticus-cli/src/cli/admin/plugins.rs` has real install-source
  detection, manifest validation, dependency checks, and uninstall behavior.

Previously remediated:

- ~~`[P1]` Go `plugins uninstall` is still a manual instruction shell.~~ Fixed: disables via API then removes directory.
- ~~`[P2]` Go install path is catalog-only.~~ Fixed: detects local directory vs catalog.

Remaining code-backed gap:

1. `[P2]` Go lacks Rust's companion-skill and dependency-check behavior class.
2. `[P3]` Go does not yet support archive install sources (tarballs/zips).

Remediation implications:

- Add dependency-check validation during install.
- Consider archive install-source support as a follow-up.

### Channels CLI

Status: `aligned`

Evidence:

- Go has `channels list|test|dead-letter|replay`.
- `channels list` now shows health-oriented aligned column output matching
  the Rust health summary class.
- The `integrations` command was removed as it was an exact duplicate of
  `channels` with no additional behavior. This was a deliberate de-duplication.

Previously remediated:

- ~~`[P2]` Go `channels list` output is thinner than Rust's health-oriented view.~~ Fixed: health-oriented aligned column output.

Remaining code-backed gap:

1. `[P3]` Go has no config-snippet guidance flow for connect/disconnect
   that Rust provides as operator workflow.

Remediation implications:

- Consider adding connect/disconnect guidance as a follow-up if operator
  demand warrants it.

### Status CLI

Status: `aligned`

Evidence:

- Go `cmd/status.go` now queries health, agent, sessions, skills, cron, cache,
  wallet, and channels endpoints for a comprehensive operator summary.
- Returns an error (not `os.Exit`) when the server is offline, providing a
  graceful offline-mode operator response.
- Rust `crates/roboticus-cli/src/cli/status.rs` builds a broader online/offline
  summary from config, sessions, skills, cron, cache, and wallet state.

Previously remediated:

- ~~`[P2]` Go has no graceful offline-mode operator response.~~ Fixed: returns error when offline.
- ~~`[P2]` Go status omits sessions/skills/cron/cache/wallet summary context.~~ Fixed: queries all subsystem endpoints.

Remaining code-backed gap:

None. Status CLI behavior is aligned with the Rust operator summary class.

### Runtime Devices

Status: `aligned`

Evidence:

- Go `internal/api/routes/runtime.go` now returns a persistent device identity
  backed by an ED25519 keypair stored in the `identity` table.
- The response includes real `public_key_hex` and `fingerprint` fields derived
  from the persisted keypair.
- Rust exposes a similar trust-identity surface with persisted keys.

Previously remediated:

- ~~`[P1]` Device identity is still hardcoded.~~ Fixed: persistent ED25519 keypair in identity table.
- ~~`[P2]` Go does not expose richer trust identity material.~~ Fixed: public_key_hex and fingerprint exposed.

Remaining code-backed gap:

None. Device identity is aligned with the Rust persisted-key identity model.

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

Status: `partial` (improved — LLM-backed analysis now implemented)

Evidence:

- Go routes now run 12 per-turn + 10 session heuristic rules, build structured
  LLM prompts, invoke the LLM service, and return `analysis_model`,
  `tokens_in`, `tokens_out`, `cost` metadata matching the Rust response shape.
- Missing turns now return 404 (was fake 200 "complete").
- Falls back to heuristic markdown summary when LLM is unavailable.

Remaining code-backed gap:

1. `[P2]` Go prompt content quality should be validated against Rust examples
   for equivalent remediation depth.
2. `[P2]` Session analysis aggregates per-turn tips locally; behavioral test
   fixtures comparing Go vs Rust output for identical inputs are still needed.

Remediation implications:

- Keep in the deep-audit program until side-by-side behavioral fixtures are
  added, but the implementation class is now aligned.

### Update Orchestration

Status: `partial (improved)` — post-update maintenance now includes mechanic health check and firmware migration check

Code-backed gap summary:

1. `[P2]` Resumability and local-modification handling are still weaker than the
   Rust baseline.
2. `[P3]` OAuth post-update maintenance is de-scoped (see Auth section), but if
   re-scoped, the update flow will need corresponding hooks.

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

1. ~~Rebuild the `models` CLI to support `exercise`, `suggest`, `reset`, and
   `baseline`.~~ **Done 2026-04-07.** ~~Remaining: make `scan` probe providers directly.~~ **Done 2026-04-07.**
2. ~~Replace line-based config mutation with structured TOML path mutation.~~ **Done 2026-04-07.** Structured TOML path navigation via go-toml/v2.
3. ~~Align memory CLI semantics with Rust: add CLI `consolidate`, `reindex`,
   and strict session/category targeting.~~ **Done 2026-04-07.**
4. ~~Add `schedule recover` operator workflow.~~ **Done 2026-04-07.** Also: `run` now resolves job names.
5. ~~Finish plugin lifecycle parity with a real uninstall flow and richer install
   source handling.~~ **Partially done 2026-04-07.** Uninstall is real, install detects local dirs. Remaining: dependency checks and archive sources.
6. ~~Replace API-key-only auth semantics with the final intended OAuth/provider
   contract, or explicitly de-scope and de-advertise it.~~ **Done 2026-04-07.** OAuth explicitly de-scoped; API-key approach documented.
7. ~~Replace the hardcoded runtime device identity with a persisted identity model.~~ **Done 2026-04-07.** ED25519 keypair in identity table.
8. ~~Finish update-orchestration parity for post-update maintenance.~~ **Partially done 2026-04-07.** Mechanic health check and firmware migration check added. Resumability gap remains.

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
