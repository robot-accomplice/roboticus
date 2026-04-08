# CLI Parity Checklist: Go Roboticus vs Rust CLI.md

Updated: 2026-04-07

## Global Flags (9/9)

| Flag | Status | Notes |
|------|--------|-------|
| `--url` | ✅ | env ROBOTICUS_URL |
| `--profile` | ✅ | env ROBOTICUS_PROFILE |
| `--color` | ✅ | auto/always/never |
| `--theme` | ✅ | env ROBOTICUS_THEME |
| `--no-draw` | ✅ | |
| `--nerdmode` | ✅ | env ROBOTICUS_NERDMODE |
| `-q/--quiet` | ✅ | Wired into printJSON + outputResult |
| `--json` | ✅ | Wired into printJSON + outputResult |
| `-c/--config` | ✅ | |

## Commands (38/38)

| Command | Subcommands | Status |
|---------|-------------|--------|
| serve | (start, run aliases) | ✅ |
| setup | (onboard alias) | ✅ |
| status | | ✅ |
| config | show, set, unset, lint, backup, validate | ✅ |
| sessions | list, show, create, delete, backfill-nicknames | ✅ |
| models | list, suggest, baseline, scan, exercise | ✅ |
| auth | status, login (--oauth), logout | ✅ |
| keystore | set, get, remove, import, rekey | ✅ |
| channels | status, dead-letter, replay, test | ✅ |
| cron | list, show, create, delete, pause, resume | ✅ |
| schedule | (cron alias) | ✅ |
| agents | list, start, stop | ✅ |
| skills | list, show, catalog, catalog-list, catalog-install, catalog-activate, import, export | ✅ |
| plugins | list, info, install, uninstall, enable, disable, search, pack | ✅ |
| mcp | list, show, test | ✅ |
| memory | working, episodic, semantic, search, consolidate | ✅ |
| metrics | costs, cache, capacity, transactions | ✅ |
| wallet | show, balance, address | ✅ |
| security | audit | ✅ |
| mechanic | (doctor alias) | ✅ |
| daemon | (service alias) | ✅ |
| service | install, start, stop, restart, status, uninstall | ✅ |
| update | check, apply, all | ✅ |
| upgrade | all | ✅ |
| version | | ✅ |
| check | config, database, providers | ✅ |
| logs | | ✅ |
| tui | | ✅ |
| web | | ✅ |
| init | | ✅ |
| reset | | ✅ |
| defrag | | ✅ |
| ingest | | ✅ |
| migrate | import, export | ✅ |
| profile | | ✅ |
| circuit | status, reset | ✅ |
| admin | | ✅ |
| completion | bash, zsh, fish | ✅ |

## Output Normalization

| Feature | Status |
|---------|--------|
| `--json` compact output | ✅ All 60+ printJSON calls |
| `--quiet` suppression | ✅ All 60+ printJSON calls |
| `outputResult()` wrapper | ✅ |
| `outputTable()` helper | ✅ |
| `outputMessage()` helper | ✅ |

## Auth Parity

| Feature | Status |
|---------|--------|
| API key login | ✅ |
| OAuth PKCE flow | ✅ `auth login --oauth` |
| Token refresh during update | ✅ |

## Update Ceremony

| Feature | Status |
|---------|--------|
| Config backup (timestamped + pruning) | ✅ |
| Legacy config migration | ✅ |
| Security config migration | ✅ |
| Firmware rules migration (TOML normalize) | ✅ |
| OAuth token refresh | ✅ |
| Post-update health check | ✅ |

## Remaining QA Tasks

- [ ] End-to-end smoke test with live server
- [ ] Per-command output comparison vs Rust
- [ ] Dashboard page-by-page workflow validation
- [ ] Memory retrieval semantic equivalence testing
- [ ] Channel adapter live testing per platform
