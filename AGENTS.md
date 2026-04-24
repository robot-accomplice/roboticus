# AGENTS.md — Roboticus Development Guide

## Build & Test
```bash
go build ./...              # Build all packages
go test ./...               # Run full test suite
go test -v -run TestLiveSmokeTest .  # Live smoke test (boots server, hits all endpoints)
go vet ./...                # Lint
./roboticus parity-audit --rust-dir=../roboticus-rust  # Feature parity check
```

## Operating Principles
- Control flow is hierarchical by default: Operators direct orchestrators, orchestrators direct subagents, subagents execute bounded work and report results upward, and orchestrators analyze and repackage results for operators.
- Subagents are execution workers, not operator-facing personas. They should default to zero personality and prove work with concrete evidence, artifacts, or observations from the current run.
- Subagents must never report directly to operators, including scheduled or cron-triggered subagent work. Scheduled subagent output is still input to orchestration, not a substitute for operator-facing reporting.

## Architecture
- **Connector-Factory pattern**: All business logic lives in `internal/pipeline/`. Channel adapters and HTTP routes are thin connectors.
- Route handlers in `internal/api/routes/` must NOT import `internal/agent` directly — use interfaces or pass through pipeline. The architecture test (`architecture_test.go`) enforces this.
- All pipeline invocations should use `pipeline.RunPipeline()` (the package-level wrapper), not `p.Run()` directly.

## Operating Principles
- **Orchestrator vs subagent split**: The orchestrator is operator-facing and may carry a budgeted personality layer. Subagents are not operator-facing; they exist to execute bounded delegated work and report back to the orchestrator.
- **Subagents default to zero personality**: Subagents should not inherit the orchestrator's personality, operator-context, or broad directive footprint by default. Any subagent prompt shaping must be task-local, minimal, and justified by execution need rather than style.
- **Determinism over style for subagents**: Subagent optimization targets are correctness, bounded execution, and clear structured results. Human-facing tone, voice, and persona are orchestrator concerns unless a subagent explicitly requires otherwise.

## Lessons Learned

### Connector-Factory Architecture Enforcement
- The architecture test `TestArchitecture_RoutesDontImportAgent` catches direct imports of `internal/agent` in route handlers. When adding new routes that need agent types (like approvals), use interfaces in the `routes` package instead of importing agent directly.

### SQLite Schema: Inline vs Separate Tables
- The scheduler lease system originally referenced a nonexistent `cron_leases` table. The correct approach (matching roboticus) is using inline `lease_holder`/`lease_expires_at` columns on the `cron_jobs` table with atomic UPDATE for contention safety.
- SQLite ALTER TABLE ADD COLUMN works for adding columns with defaults but doesn't support RENAME COLUMN in older versions.

### Parity Audit False Positives
- The `parity-audit` command uses keyword matching to detect gaps. Many "gaps" are false positives where the Go implementation uses different naming. Always verify manually before implementing.
- Example: `hybrid_search` was flagged as missing but existed in `retrieval.go` under a different function name.

### Existing Code Before Writing New
- Always check for existing implementations before writing new code. The plugin system (`script.go`) already had a full `ScriptPlugin` with `ExecuteTool`, but the parity audit flagged it as missing because the registry lacked `ScanDirectory`.

### Test Infrastructure
- `testutil.TempStore(t)` creates an isolated SQLite DB per test — use it for all DB-touching tests.
- `testutil.MockLLMServer(t, handler)` creates a mock LLM endpoint for integration tests.
- Session creation may return 201 (not 200) — use flexible status assertions.

### Config Changes
- New config sections must be added to `internal/core/config.go` in the `Config` struct AND given defaults in `DefaultConfig()`.
- Environment overrides use `ROBOTICUS_` prefix (e.g., `ROBOTICUS_SERVER_PORT=8080`).

### Architecture And Documentation Ordering
- We ALWAYS start with architecture.
- We ALWAYS follow with documentation.
- Both architecture and documentation changes happen before code changes.
- If a change introduces or materially alters an architectural seam, ownership boundary, lifecycle policy, or cross-layer control flow, update `docs/architecture-gap-report.md` and `docs/architecture-rules-diagrams.md` as part of the change before implementation is considered complete.

### Go Module Dependencies
- `github.com/coder/websocket` — already in go.mod, used for WebSocket (EventBus + CDP sessions)
- `github.com/charmbracelet/bubbletea` — TUI framework (added for `roboticus tui`)
- `github.com/charmbracelet/lipgloss` — TUI styling

## Key File Locations
| Concern | Path |
|---------|------|
| Pipeline entry point | `internal/pipeline/pipeline.go` |
| Route registration | `internal/api/server.go` |
| DB schema + migrations | `internal/db/schema.go`, `internal/db/migrations/` |
| Config struct | `internal/core/config.go` |
| Agent loop | `internal/agent/loop.go` |
| Memory retrieval | `internal/agent/retrieval.go` |
| LLM routing | `internal/llm/router.go` |
| Channel adapters | `internal/channel/` |
| CLI commands | `cmd/` |
| Smoke test | `smoke_test.go` |
| Test infrastructure | `testutil/` |
| Pipeline traces | `internal/pipeline/trace.go` |
| Log ring buffer | `internal/api/logbuffer.go` |
| Memory analytics | `internal/api/routes/memory_analytics.go` |
| MCP client | `internal/mcp/` |
| Plugin API routes | `internal/api/routes/plugins.go` |
| Trace API routes | `internal/api/routes/traces.go` |
| Memory architecture spec | `docs/memory-architecture-spec.md` |

## Release Checklist (MANDATORY — every release, no exceptions)

Every release MUST complete ALL of these before the PR is merged:

1. **Release notes**: `docs/releases/v{X.Y.Z}-release-notes.md` — summary, changes table, test coverage, file diff
2. **Architecture gap report**: `docs/architecture-gap-report.md` — close any resolved gaps, update severity table
3. **Architecture diagrams**: `docs/architecture-rules-diagrams.md` — update if any architectural patterns changed
4. **Regression test matrix**: `docs/regression-test-matrix.md` — add rows for every new regression test
5. **Site changelog**: `roboticus-site/src/lib/changelog-updates.ts` — add release entry with geek + layman text
6. **Spec docs**: Update any spec documents affected by the changes (e.g., `docs/memory-architecture-spec.md`)

Skipping any of these is a release defect. The release PR must not be created until all 6 are done.

## Release Ceremony (MANDATORY — review every release)

Before starting the release ceremony, review:

- `docs/testing/release-procedure.md`

That procedure is the canonical release order. Do not improvise the sequence.
In particular:

- release branch PR goes to `develop` first
- `develop` is audited and green before PR to `main`
- tag creation happens only after merge to `main`
- release monitoring includes artifacts, site sync, fingerprinting, and install verification

Late-cycle release fixes follow an additional hard rule:

- before pushing any release-blocker fix, rerun the exact failing local gate,
  plus formatting/lint and directly affected package tests
- if the fix changes the release path after a branch has already advanced, the
  release resumes from the correct earlier gate; do not "sneak" fixes forward
  on a later branch to save time
- repeated expensive PR churn caused by missing local preflight is itself a
  process defect
- the release workflow must also be replayable for an existing tag through one
  canonical dispatch path; do not invent manual artifact publication when the
  audited workflow can be rerun instead
- release publication must use one explicit tag authority across create/edit,
  asset upload, prerelease gating, and site sync; hidden action context is not
  acceptable release control flow
- active CI/release workflow control flow must prefer explicit first-party CLI
  or API calls over opaque third-party actions where release truth depends on
  tag identity, dispatch semantics, or publication state
- security tooling installed inside CI/release workflows must be pinned to an
  explicit version; `@latest` is not acceptable in release-critical automation
- the release workflow must self-evaluate the published release object and send
  a success/failure report instead of treating a green publish step as
  sufficient proof that the release actually completed correctly
