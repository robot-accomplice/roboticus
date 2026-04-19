# System 12: Plugin and Script Runtime

## Status

- Owner: parity-forensics program
- Audit status: `validated`
- Last updated: 2026-04-19
- Related release: v1.0.6

## Why This System Matters

Plugins and scripts extend the tool/runtime surface. If parity or architectural
discipline is weak here, the system can look aligned in built-ins while drifting
at the extension boundary.

## Scope

In scope:

- plugin discovery and loading
- script/plugin execution contract
- tool registration from plugin/runtime sources
- lifecycle and failure handling for extension code

Out of scope:

- built-in tool semantics already covered elsewhere

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Plugin/script runtime | `src/.../plugin*`, `src/.../script*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Plugin registry / discovery | `internal/plugin/plugin.go` |
| Script-backed plugin execution | `internal/plugin/script.go` |
| Agent-level script runtime | `internal/agent/script_runner.go` |
| Plugin API / install UX | `internal/api/routes/plugins.go`, `internal/api/routes/session_detail.go`, `cmd/skills/plugins.go` |
| Daemon/API composition | `internal/daemon/daemon.go`, `internal/api/server.go` |

## Live Go Path

Extension runtime crosses discovery, registration, and execution. The audit
needs to track the full lifecycle, not just whether a script executor exists.

The current Go codebase already has two distinct extension runtimes:

- `plugin.Registry` + `ScriptPlugin` for manifest-backed plugin tools
- `agent.ScriptRunner` for script execution under the skills/agent surface

That split may be valid, but it must be classified explicitly.

## Artifact Boundary

- discovered plugin/script entries
- registered tool surface from those entries
- execution outcome for an extension-backed tool

## Success Criteria

- Closure artifact(s):
  - registered extension-backed tool set
  - successful and failed execution outcomes
- Live-path proof:
  - tests prove extension-backed tools reach the same runtime surfaces as
    built-ins
- Blocking conditions:
  - extension runtime has helper-only coverage but no live-path proof
- Accepted deviations:
  - Go-native plugin ergonomics are acceptable if runtime ownership is still
    coherent

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-12-001 | P1 | Plugin registry is not visibly daemon-owned on the live path | Rust runtime ownership needs explicit comparison | Closed in v1.0.6: `Daemon.New()` now constructs the registry, scans `cfg.Plugins.Dir`, initializes discovered plugins, and attaches the live registry to `AppState.Plugins` so plugin routes observe the same runtime-owned surface | Remediated | Closed | `internal/plugin/plugin.go`, `internal/api/server.go`, `internal/daemon/daemon.go`, `internal/daemon/daemon_coverage_test.go` |
| SYS-12-002 | P1 | Plugin install UX must share the live runtime registry | Rust install/runtime relationship needs comparison | Closed in v1.0.6: install flows now write a manifest-backed plugin directory and immediately load it through `plugin.Registry.LoadDirectory(...)`, so the running daemon observes the same registry-owned surface without restart | Remediated | Closed | `internal/api/routes/plugin_install.go`, `internal/plugin/plugin.go`, `internal/api/server.go`, `internal/api/routes/coverage_test.go` |
| SYS-12-006 | P1 | Startup plugin discovery previously loaded names without real tool surfaces | Rust runtime ownership needs explicit comparison | Closed in v1.0.6: `ScanDirectory(...)` now parses TOML/YAML manifests with real `tools`, `requirements`, and metadata instead of line-scanning only `name/version/description`, so startup-loaded plugins expose actual runtime tool definitions | Remediated | Closed | `internal/plugin/plugin.go`, `internal/plugin/coverage_test.go`, `internal/daemon/daemon_coverage_test.go` |
| SYS-12-003 | P1 | Plugin execution and agent script execution previously enforced different script-runtime contracts | Rust separation needs explicit comparison | Closed in v1.0.6: both paths now delegate to the same shared script execution contract for root containment, interpreter allowlists, output truncation, and sandbox env shaping; daemon-owned plugin registries source that policy from `cfg.Skills`, while plugin-specific env injection remains an intentional extension mechanism | Remediated | Closed | `internal/core/script_exec.go`, `internal/plugin/script.go`, `internal/agent/script_runner.go`, `internal/daemon/daemon.go`, `internal/plugin/coverage_test.go` |
| SYS-12-004 | P2 | Registry-level permission/risk validation is stronger than a naive extension surface | Rust policy level needs comparison | Plugin registration validates names, risk levels, and strict-mode permissions before activation | Likely improvement | Accepted | `internal/plugin/plugin.go` |
| SYS-12-005 | P1 | Extension-backed tools previously drifted from the main agent tool-selection surface on hot install/enable | Rust tool-surface integration needs comparison | Closed in v1.0.6: plugin-backed tools now hot-sync into the main registry and refresh descriptor embeddings during install/enable, so the live semantic pruning surface stays aligned with startup-owned plugin discovery instead of leaving newly loaded tools rankless until daemon restart | Remediated | Closed | `internal/api/routes/plugins.go`, `internal/api/routes/plugin_install.go`, `internal/agent/tools/registry.go`, `internal/api/server.go`, `internal/daemon/daemon.go`, `internal/api/routes/coverage_test.go` |

## Intentional Deviations

- Go may legitimately have richer plugin ergonomics; that still needs explicit
  classification, not assumption.
- Separate skill/script and plugin/script wrappers remain acceptable so long as
  they share one underlying execution contract and only differ in wrapper-level
  concerns such as manifest discovery and plugin-specific environment injection.

## Remediation Notes

Promoted from an implicit concern under System 08.

The first real issue is not “no plugin code exists.” It is that the
administrative/plugin-management surface appears ahead of the live runtime
ownership surface.

## Downstream Systems Affected

- System 02: tool exposure and pruning
- System 08: MCP and external integrations
- System 10: security / policy / sandbox semantics

## Final Disposition

System 12 is closed for v1.0.6.

- Plugin discovery/init, install-time hot loading, tool-surface sync, and
  script execution contract now have one coherent runtime ownership story.
- Remaining differences are wrapper ergonomics, not runtime drift.

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
- 2026-04-17: Deepened with a concrete runtime-ownership gap: registry/API
  surfaces exist, but daemon-owned discovery/init/registration is not yet
  evident on the live path.
- 2026-04-17: Closed the startup ownership gap. `Daemon.New()` now owns
  registry construction, directory scan, init, and `AppState.Plugins`
  wiring; `ScanDirectory(...)` now parses real manifest tool definitions.
- 2026-04-17: Closed the hot-registration gap. `/api/plugins/install` now
  writes a manifest-backed plugin directory and loads it into the live
  registry immediately through `plugin.Registry.LoadDirectory(...)`.
- 2026-04-17: Closed the hot tool-surface gap. Plugin install/enable now
  refreshes descriptor embeddings after syncing plugin tools into the main
  registry, so newly loaded tools participate in semantic pruning/ranking
  without waiting for daemon restart.
- 2026-04-17: Closed the script-runtime contract drift. Skills and
  manifest-backed plugin scripts now execute through one shared core contract
  for containment, interpreter allowlists, output limits, and sandbox env
  shaping, with registry-owned plugins sourcing that policy from `cfg.Skills`.
