# System 12: Plugin and Script Runtime

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-17
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
| SYS-12-001 | P1 | Plugin registry is not visibly daemon-owned on the live path | Rust runtime ownership needs explicit comparison | `plugin.Registry` exists and API routes expose it, but current daemon composition does not populate `AppState.Plugins`, and no startup scan/init path is evident in `Daemon.New()` / `run()` | Missing functionality / ownership gap | Open | `internal/plugin/plugin.go`, `internal/api/server.go`, `internal/daemon/daemon.go` |
| SYS-12-002 | P1 | Plugin install UX is administratively richer than runtime registration | Rust install/runtime relationship needs comparison | `/api/plugins/install` and `skills plugins install` can write plugin files, but that does not by itself register/init them into the live runtime registry | Missing functionality / split ownership | Open | `internal/api/routes/session_detail.go`, `cmd/skills/plugins.go` |
| SYS-12-003 | P2 | Plugin execution and agent script execution are separate runtimes | Rust separation needs explicit comparison | `ScriptPlugin.ExecuteTool(...)` inherits OS env plus plugin env and runs scripts from plugin dirs, while `agent.ScriptRunner` enforces interpreter allowlists, root containment, and optional sandboxed env for skills/scripts | Open | Open | `internal/plugin/script.go`, `internal/agent/script_runner.go` |
| SYS-12-004 | P2 | Registry-level permission/risk validation is stronger than a naive extension surface | Rust policy level needs comparison | Plugin registration validates names, risk levels, and strict-mode permissions before activation | Likely improvement | Accepted | `internal/plugin/plugin.go` |
| SYS-12-005 | P2 | Extension-backed tools are not yet clearly integrated into the main agent tool-selection surface | Rust tool-surface integration needs comparison | Plugin tools are exposed through plugin routes and registry methods, but this audit has not yet proven they are embedded into the same live request/tool-pruning path as built-ins and MCP tools | Open | Open | `internal/plugin/plugin.go`, `internal/agent/tools/registry.go`, `internal/api/routes/plugins.go` |

## Intentional Deviations

- Go may legitimately have richer plugin ergonomics; that still needs explicit
  classification, not assumption.
- Separate skill/script and plugin/script runtimes may be acceptable if their
  security and operator contracts are made explicit instead of drifting
  implicitly.

## Remediation Notes

Promoted from an implicit concern under System 08.

The first real issue is not “no plugin code exists.” It is that the
administrative/plugin-management surface appears ahead of the live runtime
ownership surface.

## Downstream Systems Affected

- System 02: tool exposure and pruning
- System 08: MCP and external integrations
- System 10: security / policy / sandbox semantics

## Open Questions

- Are plugins and scripts one runtime concern, or should they split later?
- Should plugin-backed tools join the same selected/pruned `llm.Request.Tools`
  surface as built-ins and MCP tools, or remain explicitly out-of-band?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
- 2026-04-17: Deepened with a concrete runtime-ownership gap: registry/API
  surfaces exist, but daemon-owned discovery/init/registration is not yet
  evident on the live path.
