# AGENTS.md — Roboticus Development Guide

## Build & Test
```bash
go build ./...              # Build all packages
go test ./...               # Run full test suite
go test -v -run TestLiveSmokeTest .  # Live smoke test (boots server, hits all endpoints)
go vet ./...                # Lint
./roboticus parity-audit --roboticus-dir=../roboticus  # Feature parity check
```

## Architecture
- **Connector-Factory pattern**: All business logic lives in `internal/pipeline/`. Channel adapters and HTTP routes are thin connectors.
- Route handlers in `internal/api/routes/` must NOT import `internal/agent` directly — use interfaces or pass through pipeline. The architecture test (`architecture_test.go`) enforces this.
- All pipeline invocations should use `pipeline.RunPipeline()` (the package-level wrapper), not `p.Run()` directly.

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
- Environment overrides use `GOBOTICUS_` prefix (e.g., `GOBOTICUS_SERVER_PORT=8080`).

### Go Module Dependencies
- `nhooyr.io/websocket` — already in go.mod, used for WebSocket (EventBus + CDP sessions)
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
