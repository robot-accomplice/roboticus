# Configuration Reference

Roboticus loads configuration from `~/.roboticus/roboticus.toml` on startup. All settings can be overridden via environment variables with the `ROBOTICUS_` prefix (e.g., `ROBOTICUS_SERVER_PORT=8080`).

## Configuration File Location

The config file is searched in order:
1. `--config` CLI flag
2. `~/.roboticus/roboticus.toml`
3. `./roboticus.toml` (current directory)

If no file is found, sensible defaults are used.

---

## `[agent]` — Agent Identity

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | `"roboticus"` | Agent display name. Used in prompts and responses. |
| `id` | string | `""` | Unique agent identifier. Auto-generated if empty. |
| `workspace` | string | `~/.roboticus/workspace` | Root directory for agent file operations. All tool paths are sandboxed to this directory. |
| `autonomy_max_react_turns` | int | `25` | Maximum ReAct loop iterations before forcing a response. Prevents runaway tool loops. |
| `autonomy_max_turn_duration_seconds` | int | `120` | Maximum wall-clock time for a single turn. |

```toml
[agent]
name = "roboticus"
workspace = "~/.roboticus/workspace"
autonomy_max_react_turns = 25
```

---

## `[server]` — HTTP Server

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | `18789` | HTTP server listen port. Must be 1-65535. |
| `bind` | string | `"localhost"` | Bind address. Use `"0.0.0.0"` to expose to network. Must be a valid IP or `"localhost"`. |
| `log_dir` | string | `~/.roboticus/logs` | Directory for log file output. |
| `cron_max_concurrency` | int | `4` | Maximum concurrent cron job executions. Must be 1-16. |

```toml
[server]
port = 18789
bind = "localhost"
cron_max_concurrency = 4
```

---

## `[database]` — SQLite Storage

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | `~/.roboticus/roboticus.db` | Path to SQLite database file. Created automatically if it doesn't exist. |

The database uses WAL journal mode with foreign keys enabled and a 5-second busy timeout. Maximum 4 open connections.

---

## `[models]` — LLM Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `primary` | string | `"claude-sonnet-4-20250514"` | Primary model for inference. |
| `fallback` | string[] | `[]` | Ordered list of fallback models tried when the primary fails. |

### `[models.routing]` — Model Routing

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mode` | string | `"primary"` | Routing mode: `"primary"` (always use primary) or `"metascore"` (complexity-based tier selection). |
| `confidence_threshold` | float | `0.9` | Minimum confidence score for accepting a local model's response. [0.0, 1.0]. |
| `estimated_output_tokens` | int | `512` | Expected output tokens for cost estimation. |
| `accuracy_floor` | float | `0.7` | Minimum accuracy for a model to be considered. [0.0, 1.0]. |
| `accuracy_min_obs` | int | `10` | Minimum observations before accuracy is evaluated. |
| `cost_aware` | bool | `true` | Prefer cheaper models within the same tier. |
| `cost_weight` | float | — | Weight for cost vs quality trade-off. [0.0, 1.0]. |
| `canary_fraction` | float | `0.0` | Fraction of requests routed to the canary model for A/B testing. |
| `canary_model` | string | `""` | Model for canary testing. Required if `canary_fraction > 0`. |
| `blocked_models` | string[] | `[]` | Models that should never be selected. |
| `per_provider_timeout_seconds` | int | `30` | Timeout per provider attempt. Must be >= 5. |
| `max_total_inference_seconds` | int | `90` | Maximum total inference time including retries. |
| `max_fallback_attempts` | int | `3` | Maximum number of fallback provider attempts. |

```toml
[models]
primary = "claude-sonnet-4-20250514"
fallback = ["gpt-4o", "gemini-2.0-flash"]

[models.routing]
mode = "metascore"
confidence_threshold = 0.9
cost_aware = true
per_provider_timeout_seconds = 30
```

---

## `[providers.<name>]` — LLM Provider Configuration

Each provider is configured as a named section. User-defined providers override bundled defaults.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | — | Base URL for the provider API. |
| `tier` | string | — | Provider tier: `"T1"` (local), `"T2"` (proxy), `"T3"` (cloud). |
| `format` | string | `""` | Wire format: `"openai"`, `"anthropic"`, `"google"`, `"ollama"`. |
| `api_key_env` | string | `""` | Environment variable containing the API key. |
| `chat_path` | string | `""` | Override for the chat completions endpoint path. |
| `embedding_path` | string | `""` | Endpoint path for embeddings. |
| `embedding_model` | string | `""` | Model name for embeddings. |
| `embedding_dimensions` | int | `0` | Expected embedding vector dimensions. |
| `is_local` | bool | `false` | Whether the provider runs locally (affects routing priority). |
| `cost_per_input_token` | float | `0` | Cost per input token (USD). |
| `cost_per_output_token` | float | `0` | Cost per output token (USD). |
| `auth_header` | string | `""` | Custom authentication header name (e.g., `"x-api-key"` for Anthropic). |
| `extra_headers` | map | `{}` | Additional HTTP headers sent with every request. |
| `tpm_limit` | int | `0` | Tokens-per-minute rate limit. 0 = unlimited. |
| `rpm_limit` | int | `0` | Requests-per-minute rate limit. 0 = unlimited. |

```toml
[providers.openai]
url = "https://api.openai.com"
tier = "T3"
format = "openai"
api_key_env = "OPENAI_API_KEY"
embedding_path = "/v1/embeddings"
embedding_model = "text-embedding-3-small"

[providers.anthropic]
url = "https://api.anthropic.com"
tier = "T3"
format = "anthropic"
api_key_env = "ANTHROPIC_API_KEY"
auth_header = "x-api-key"

[providers.ollama]
url = "http://localhost:11434"
tier = "T1"
format = "openai"
is_local = true
```

---

## `[memory]` — Memory System

Budget percentages control how much of the context window is allocated to each memory tier. **Must sum to 100.0** (validated with 0.01 tolerance).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `working_budget` | float | `40.0` | % allocated to working memory (active session context). |
| `episodic_budget` | float | `25.0` | % allocated to episodic memory (past events with decay). |
| `semantic_budget` | float | `15.0` | % allocated to semantic memory (structured knowledge). |
| `procedural_budget` | float | `10.0` | % allocated to procedural memory (tool statistics). |
| `relationship_budget` | float | `10.0` | % allocated to relationship memory (entity tracking). |

```toml
[memory]
working_budget = 40.0
episodic_budget = 25.0
semantic_budget = 15.0
procedural_budget = 10.0
relationship_budget = 10.0
```

---

## `[cache]` — Semantic Cache

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ttl_seconds` | int | `3600` | Cache entry time-to-live. |
| `similarity_threshold` | float | `0.85` | Minimum similarity for cache hits (exact match currently). |

---

## `[treasury]` — Financial Policy

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `daily_cap` | float | `5.0` | Maximum daily spend in USD. |
| `per_payment_cap` | float | `1.0` | Maximum per-payment amount in USD. Must be > 0. |
| `transfer_limit` | float | `1.0` | Maximum single transfer amount in USD. |
| `minimum_reserve` | float | `0.0` | Minimum wallet balance to maintain. Must be >= 0. |

---

## `[session]` — Session Management

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `scope_mode` | string | `"agent"` | Session scoping: `"agent"` (one session per agent), `"peer"` (per sender), `"group"` (per chat). |

---

## `[channels]` — Channel Adapter Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `telegram_token_env` | string | `""` | Environment variable for Telegram Bot API token. |
| `whatsapp_token_env` | string | `""` | Environment variable for WhatsApp Cloud API token. |
| `discord_token_env` | string | `""` | Environment variable for Discord bot token. |
| `signal_account` | string | `""` | Signal phone number (E.164 format). |
| `signal_daemon_url` | string | `""` | URL of the signal-cli JSON-RPC daemon. |

---

## `[security]` — Security Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `workspace_only` | bool | `true` | Restrict file operations to the workspace directory. |
| `deny_on_empty_allowlist` | bool | `true` | **Must be true.** Deny all paths when the allowlist is empty. Setting to `false` is a validation error. |
| `allowed_paths` | string[] | `[]` | Additional paths allowed for file operations (beyond workspace). |
| `protected_paths` | string[] | `[]` | Patterns that are always blocked in tool arguments. |
| `interpreter_allow` | string[] | `[]` | Allowed script interpreters for the bash tool. |
| `script_allowed_paths` | string[] | `[]` | Paths allowed for script execution. Must be absolute paths. Auto-merged into `allowed_paths`. |

---

## `[wallet]` — Crypto Wallet

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | `~/.roboticus/wallet.enc` | Path to the encrypted wallet file. |

The wallet requires a passphrase set via `ROBOTICUS_WALLET_PASSPHRASE` environment variable. Plaintext wallet storage is rejected.

---

## `[skills]` — Skill Discovery

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `directory` | string | `""` | Directory to scan for skill files. Empty = no skill loading. |
| `watch_mode` | bool | `true` | Enable filesystem watching for skill hot-reload. |

---

## `[plugins]` — Plugin System

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `dir` | string | `~/.roboticus/plugins` | Plugin discovery directory. |

---

## Validation Rules

The configuration is validated on load. Validation errors prevent startup.

1. `models.primary` must be non-empty
2. `database.path` must be non-empty
3. `server.port` must be 1-65535
4. `server.bind` must be a valid IP or `"localhost"`
5. `server.cron_max_concurrency` must be 1-16
6. `agent.autonomy_max_react_turns` must be > 0
7. `agent.autonomy_max_turn_duration_seconds` must be > 0
8. `session.scope_mode` must be `"agent"`, `"peer"`, or `"group"`
9. Memory budgets must sum to 100.0 (± 0.01)
10. `treasury.per_payment_cap` must be > 0
11. `treasury.minimum_reserve` must be >= 0
12. `security.deny_on_empty_allowlist` must be `true`
13. All `security.script_allowed_paths` must be absolute paths
14. `models.routing.mode` must be `"primary"` or `"metascore"`
15. `models.routing.confidence_threshold` must be [0.0, 1.0]
16. `models.routing.accuracy_floor` must be [0.0, 1.0]
17. `models.routing.canary_fraction` must be [0.0, 1.0]
18. `canary_model` required when `canary_fraction > 0` (and vice versa)
19. `canary_model` must not appear in `blocked_models`
20. `per_provider_timeout_seconds` must be >= 5
21. `max_total_inference_seconds` must be >= `per_provider_timeout_seconds`

## Tilde Expansion

All path-valued fields support `~` prefix expansion to the user's home directory. Expansion is applied during config loading, before validation.
