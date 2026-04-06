# CLI Parity Checklist: Go Roboticus vs Rust CLI.md Contract

Generated: 2026-04-06

## Legend
- **Complete**: Fully implemented and tested
- **Partial**: Core functionality works, edge cases may differ
- **Deferred**: Explicitly deferred with documented reason

---

## Global Flags

| Flag | Short | Env Var | Status |
|------|-------|---------|--------|
| `--config` | `-c` | `ROBOTICUS_CONFIG` | Complete |
| `--url` | — | `ROBOTICUS_URL` | Complete |
| `--profile` | — | `ROBOTICUS_PROFILE` | Complete |
| `--color` | — | — | Complete (flag registered, rendering TBD) |
| `--theme` | — | `ROBOTICUS_THEME` | Complete (flag registered, rendering TBD) |
| `--no-draw` | — | — | Complete (flag registered) |
| `--nerdmode` | — | `ROBOTICUS_NERDMODE` | Complete (flag registered) |
| `--quiet` | — | — | Complete (flag registered) |
| `--json` | — | — | Complete (flag registered, output layer TBD) |
| `--port` | — | — | Complete |
| `--bind` | — | — | Complete |

## Aliases

| Alias | Target | Status |
|-------|--------|--------|
| `start` | `serve` | Complete |
| `run` | `serve` | Complete |
| `onboard` | `setup` | Complete |
| `doctor` | `mechanic` | Complete |
| `cron` | `schedule` | Complete (both registered) |
| `schedule` | `cron` | Complete (both registered) |
| `daemon` | `service` | Complete |
| `upgrade all` | `update all` | Complete |

## Commands

### Lifecycle

| Command | Status | Notes |
|---------|--------|-------|
| `serve` | Complete | Aliases: start, run |
| `init` | Complete | |
| `setup` | Complete | Alias: onboard |
| `check` | Complete | |
| `version` | Complete | |
| `update check` | Complete | |
| `update all` | Complete | |
| `update binary` | Complete | Same as update all |
| `update providers` | Partial | Shares update all flow |
| `update skills` | Partial | Shares update all flow |

### Operations

| Command | Status | Notes |
|---------|--------|-------|
| `status` | Complete | |
| `mechanic` | Complete | Alias: doctor |
| `logs` | Complete | -n, -f, -l flags |
| `circuit status` | Complete | |
| `circuit reset` | Complete | |

### Data

| Command | Status | Notes |
|---------|--------|-------|
| `sessions list` | Complete | |
| `sessions show` | Complete | |
| `sessions create` | Complete | NEW |
| `sessions export` | Complete | --format json/markdown |
| `sessions backfill-nicknames` | Complete | NEW |
| `memory list <TIER>` | Complete | working/episodic/semantic |
| `memory search` | Complete | |
| `memory stats` | Complete | |
| `skills list` | Complete | |
| `skills show` | Complete | NEW |
| `skills reload` | Complete | |
| `skills catalog-list` | Complete | NEW |
| `skills catalog-install` | Complete | NEW |
| `skills catalog-activate` | Complete | NEW |
| `skills import` | Complete | NEW |
| `skills export` | Complete | NEW |
| `mcp list` | Complete | |
| `mcp show` | Complete | NEW |
| `mcp test` | Complete | NEW |
| `mcp connect` | Complete | |
| `mcp disconnect` | Complete | |
| `schedule list` | Complete | |
| `schedule create` | Complete | |
| `schedule delete` | Complete | |
| `schedule run` | Complete | |
| `schedule history` | Complete | |
| `metrics costs` | Complete | |
| `metrics transactions` | Complete | NEW |
| `metrics cache` | Complete | |
| `wallet show` | Complete | NEW |
| `wallet address` | Complete | |
| `wallet balance` | Complete | |

### Authentication

| Command | Status | Notes |
|---------|--------|-------|
| `auth status` | Complete | |
| `auth login` | Complete | |
| `auth logout` | Complete | |

### Configuration

| Command | Status | Notes |
|---------|--------|-------|
| `config show` | Complete | |
| `config get` | Complete | |
| `config set` | Complete | NEW |
| `config unset` | Complete | NEW |
| `config lint` | Complete | Alias for validate |
| `config backup` | Complete | NEW |

### Model Discovery

| Command | Status | Notes |
|---------|--------|-------|
| `models list` | Complete | |
| `models scan` | Complete | NEW |
| `models diagnostics` | Complete | |

### Plugins

| Command | Status | Notes |
|---------|--------|-------|
| `plugins list` | Complete | |
| `plugins info` | Complete | NEW |
| `plugins install` | Complete | NEW |
| `plugins uninstall` | Partial | Prints instructions (restart needed) |
| `plugins enable` | Complete | NEW |
| `plugins disable` | Complete | NEW |
| `plugins search` | Deferred | No catalog API; prints message |
| `plugins pack` | Complete | NEW |

### Agents

| Command | Status | Notes |
|---------|--------|-------|
| `agents list` | Complete | NEW |
| `agents start` | Complete | NEW |
| `agents stop` | Complete | NEW |

### Channels

| Command | Status | Notes |
|---------|--------|-------|
| `channels list` | Complete | |
| `channels dead-letter` | Complete | |
| `channels replay` | Complete | NEW |

### Security

| Command | Status | Notes |
|---------|--------|-------|
| `security show` | Complete | |
| `security audit` | Complete | NEW |

### Credentials

| Command | Status | Notes |
|---------|--------|-------|
| `keystore status` | Complete | |
| `keystore list` | Complete | |
| `keystore set` | Complete | NEW |
| `keystore get` | Complete | NEW |
| `keystore remove` | Complete | NEW |
| `keystore import` | Complete | NEW |
| `keystore rekey` | Deferred | Needs server-side support |

### Migration

| Command | Status | Notes |
|---------|--------|-------|
| `migrate` (DB) | Complete | |
| `migrate import` | Deferred | Legacy data migration TBD |
| `migrate export` | Deferred | Legacy data export TBD |

### System

| Command | Status | Notes |
|---------|--------|-------|
| `daemon install` | Complete | NEW (alias for service) |
| `daemon start` | Complete | NEW |
| `daemon stop` | Complete | NEW |
| `daemon restart` | Complete | NEW |
| `daemon status` | Complete | NEW |
| `daemon uninstall` | Complete | NEW |
| `service install` | Complete | |
| `service start/stop/restart` | Complete | |
| `service status` | Complete | |
| `service uninstall` | Complete | |
| `web` | Complete | |
| `reset` | Complete | |
| `uninstall` | Complete | NEW (--purge, --yes flags) |
| `completion` | Complete | bash/zsh/fish |

## Test Coverage

| Test Category | File | Tests |
|---------------|------|-------|
| Global flags | `cmd/cli_contract_test.go` | TestCLI_GlobalFlags |
| Top-level commands | `cmd/cli_contract_test.go` | TestCLI_TopLevelCommands |
| Aliases | `cmd/cli_contract_test.go` | TestCLI_Aliases |
| Subcommand sets | `cmd/cli_contract_test.go` | TestCLI_SubcommandSets |
| Update/Upgrade | `cmd/cli_contract_test.go` | TestCLI_UpdateAll |
| Schedule/Cron | `cmd/cli_contract_test.go` | TestCLI_ScheduleCronAlias |

## Deferred Items

| Item | Reason |
|------|--------|
| `keystore rekey` | Needs server-side passphrase change endpoint |
| `migrate import/export` | Legacy workspace format not yet documented |
| `plugins search` | No remote catalog API |
| `--json` output wrapping | Flag registered; progressive command-by-command adoption |
| `--quiet` output suppression | Flag registered; needs output layer refactor |
| `--color`/`--theme`/`--nerdmode` rendering | Flags registered; rendering in CLI output TBD |
