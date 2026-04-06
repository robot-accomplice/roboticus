```
       o╔═══╗o
        ║◉ ◉║
        ║ UU║
        ╚═╤═╝
      ╔═══╪═══╗       G O B O T I C U S
  ╔═══╣ ▓▓║▓▓ ╠═══╗   Autonomous Agent Runtime
  █   ║ ▓▓║▓▓ ║   █
      ╚══╤═╤══╝
         ║ ║
        ═╩═╩═
```

# Roboticus

![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white)
![CI](https://img.shields.io/github/actions/workflow/status/robot-accomplice/roboticus/ci.yml?style=flat-square&label=CI)
![Parity Audit](https://img.shields.io/github/actions/workflow/status/robot-accomplice/roboticus/parity-audit.yml?style=flat-square&label=parity%20audit)
![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)
![Go Version](https://img.shields.io/github/go-mod/go-version/robot-accomplice/roboticus?style=flat-square)
![Lines of Code](https://img.shields.io/badge/lines-17k%2B-blue?style=flat-square)
![Tests](https://img.shields.io/badge/tests-29%20files-green?style=flat-square)
![Channels](https://img.shields.io/badge/channels-7-purple?style=flat-square)
![Providers](https://img.shields.io/badge/providers-10-orange?style=flat-square)

**Autonomous AI agent runtime - idiomatic Go rewrite of
[roboticus](https://github.com/robot-accomplice/roboticus)**

Multi-model inference - 5-tier memory - 7 channels - On-chain wallet - Full dashboard SPA

---

## Overview

Roboticus is a self-contained AI agent runtime that manages its own context, memory, tools, scheduling, and multi-channel communication. It compiles to a single binary with an embedded web dashboard — no external dependencies beyond an LLM provider and SQLite.

```bash
roboticus serve --port 18789
```

## Architecture

```text
┌────────────────────────────────────────────────────────────────┐
│                        HTTP / WebSocket                        │
│  Dashboard SPA (embedded)  │  REST API  │  SSE Streaming       │
├────────────────────────────────────────────────────────────────┤
│                         Pipeline                               │
│  Validate → Injection Defense → Session → Decomposition →      │
│  Skill Match → Shortcut → ReAct Loop → Guards → Memory         │
├────────────────────────────────────────────────────────────────┤
│  Agent Loop      │  Memory (5-tier)   │  Tool Execution        │
│  ┌────────────┐  │  Working           │  read_file, write_file │
│  │ LLM Call   │  │  Episodic          │  bash, search_files    │
│  │ Tool Use   │  │  Semantic          │  web_search, http_fetch│
│  │ Policy     │  │  Procedural        │  introspect, echo      │
│  │ Guardrails │  │  Relationship      │  glob_files, edit_file │
│  └────────────┘  │                    │  list_directory        │
├────────────────────────────────────────────────────────────────┤
│  Channels        │  LLM Service       │  Scheduler             │
│  Telegram        │  Router + Cascade  │  Cron (durable)        │
│  Discord         │  Circuit Breaker   │  Lease-based locking   │
│  Signal          │  Semantic Cache    │  Interval + One-shot   │
│  WhatsApp        │  Dedup             │                        │
│  Voice (STT/TTS) │  Tiered Inference  │  Wallet                │
│  Email           │  10 Bundled        │  secp256k1 ECDSA       │
│  A2A (encrypted) │  Providers         │  x402 EIP-3009 signing │
├────────────────────────────────────────────────────────────────┤
│                      SQLite + FTS5                             │
└────────────────────────────────────────────────────────────────┘
```

## Features

### Inference Engine

- **Multi-model routing** with complexity-based tier selection (Small/Medium/Large/Frontier)
- **Cascade optimizer** — sliding-window expected utility to decide cheap-first vs direct
- **Tiered inference** — cache → local → cloud with confidence evaluation
- **Circuit breaker** per provider with exponential backoff
- **Semantic cache** with configurable similarity threshold
- **Request deduplication** for concurrent identical prompts
- **10 bundled providers** — Ollama, OpenAI, Anthropic, Google, OpenRouter, vLLM, sglang, llama-cpp, docker-model-runner, Moonshot

### Memory System

- **Working memory** — active session context (goals, notes, turn summaries)
- **Episodic memory** — past events with temporal decay re-ranking
- **Semantic memory** — structured knowledge (category/key/value with confidence scores)
- **Procedural memory** — tool usage statistics (success/failure rates per tool)
- **Relationship memory** — entity interaction tracking (trust scores, frequency)
- **Embedding client** — OpenAI, Google, Ollama formats with n-gram fallback
- **Hybrid retrieval** — FTS5 + cosine similarity with configurable blend weight

### Pipeline

- 12-stage inference pipeline: validation, injection defense, session resolution, dedup, decomposition, skill-first dispatch, shortcut handling, authority resolution, delegation, ReAct loop, guard chain, memory ingest
- **Guard chain** with retry support — empty response, system prompt leak, content classification, repetition detection
- **Context window management** — 5-stage progressive compaction (verbatim → selective trim → semantic compress → topic extract → skeleton)
- **Semantic compression** — IDF-scored sentence selection preserving high-information content

### Channels

| Channel | Send | Receive | Features |
| --------- | ------ | --------- | ---------- |
| **Telegram** | Markdown, reply keyboards | Long-poll + webhook | MarkdownV2 fallback, media |
| **Discord** | Embeds, reactions | Webhook ingest | Rich formatting |
| **Signal** | Styled text | JSON-RPC daemon | End-to-end encrypted |
| **WhatsApp** | Templates, media | Cloud API webhook | HMAC verification |
| **Voice** | OpenAI TTS | Whisper STT | Real-time transcription |
| **Email** | SMTP | IMAP polling | HTML + plain text |
| **A2A** | Encrypted | Encrypted | X25519 ECDH + AES-256-GCM |

### Dashboard

- **Embedded SPA** — 7,395-line single-file app served from the binary via `go:embed`
- **12 pages** — Overview, Sessions, Context, Memory, Skills, Agents, Scheduler, Metrics, Efficiency, Wallet, Workspace, Settings
- **4 themes** — AI Purple (default), CRT Orange, CRT Green, Psychedelic
- **Real-time** — WebSocket event streaming with ticket-based auth
- **Security** — CSP nonce injection, X-Frame-Options, HSTS headers
- **Canvas charts** — Sparkline cost/token graphs, SVG spider routing profiles

### Wallet & Payments

- **secp256k1 ECDSA** key generation and management
- **AES-256-GCM** encrypted key storage with HKDF key derivation
- **JSON-RPC 2.0** — ETH balance, ERC-20 balances, chain ID, nonce, transaction broadcast
- **x402 protocol** — EIP-712 domain separator, EIP-3009 `transferWithAuthorization` signing
- **Treasury policy** — per-payment caps, daily limits, minimum reserve enforcement

### Scheduling

- **Durable scheduler** — cron (5-field with timezone), interval, one-shot `at` expressions
- **Lease-based locking** — prevents double-fire across instances
- **Run history** — success/failure tracking with duration and error recording

### Skills & Plugins

- **Skill loader** — discovers `.md` (YAML frontmatter) and `.toml`/`.yaml` skills recursively
- **Hot-reload** — filesystem watcher with SHA-256 change detection
- **Trigger matching** — keyword-based skill activation injected as system context
- **Plugin registry** — allow/deny lists, permission enforcement, `InitAll`/`ExecuteTool`

## Quick Start

```bash
# Build
go build -o roboticus .

# Run with defaults (~/.roboticus/ data directory)
./roboticus serve

# Custom port and bind
./roboticus serve --port 8080 --bind 0.0.0.0

# Run parity audit against roboticus source
./roboticus parity-audit --roboticus-dir=../roboticus
```

Open `http://localhost:18789` for the dashboard.

## Configuration

Roboticus loads configuration from `~/.roboticus/roboticus.toml` with environment variable overrides (prefix `ROBOTICUS_`).

```toml
[agent]
name = "roboticus"
workspace = "~/.roboticus/workspace"

[server]
port = 18789
bind = "127.0.0.1"

[models]
primary = "claude-sonnet-4-20250514"
fallback = ["gpt-4o", "gemini-2.0-flash"]

[models.routing]
mode = "metascore"
confidence_threshold = 0.9
cost_aware = true

[memory]
working_budget = 40.0
episodic_budget = 25.0
semantic_budget = 15.0
procedural_budget = 10.0
relationship_budget = 10.0

[treasury]
daily_cap = 5.0
per_payment_cap = 1.0

[security]
workspace_only = true
deny_on_empty_allowlist = true

# Provider overrides (bundled defaults cover most cases)
[providers.openai]
api_key_env = "OPENAI_API_KEY"

[providers.anthropic]
api_key_env = "ANTHROPIC_API_KEY"
```

### Bundled Providers

These are pre-configured and available without any TOML entries:

| Provider | URL | Tier | Format |
| ---------- | ----- | ------ | -------- |
| Ollama | `localhost:11434` | T1 (local) | OpenAI |
| sglang | `localhost:30000` | T1 (local) | OpenAI |
| vLLM | `localhost:8000` | T1 (local) | OpenAI |
| docker-model-runner | `localhost:12434` | T1 (local) | OpenAI |
| OpenAI | `api.openai.com` | T3 (cloud) | OpenAI |
| Anthropic | `api.anthropic.com` | T3 (cloud) | Anthropic |
| Google | `generativelanguage.googleapis.com` | T3 (cloud) | Google |
| OpenRouter | `openrouter.ai/api` | T2 (proxy) | OpenAI |
| llama-cpp | `localhost:8080` | T1 (local) | OpenAI |

## API

### Core Endpoints

| Method | Path | Description |
| -------- | ------ | ------------- |
| `POST` | `/api/agent/message` | Send a message (non-streaming) |
| `POST` | `/api/agent/message/stream` | Send a message (SSE streaming) |
| `GET` | `/api/health` | Health check with provider status |
| `GET` | `/api/config` | Current configuration |
| `POST` | `/api/ws-ticket` | Get a WebSocket auth ticket |
| `GET` | `/ws` | WebSocket event stream |

### Sessions & Memory

| Method | Path | Description |
| -------- | ------ | ------------- |
| `GET` | `/api/sessions` | List sessions |
| `POST` | `/api/sessions` | Create session |
| `GET` | `/api/memory/working` | Working memory entries |
| `GET` | `/api/memory/episodic` | Episodic memory entries |
| `GET` | `/api/memory/semantic` | Semantic knowledge store |
| `GET` | `/api/memory/search?q=` | Cross-tier memory search |

### Scheduling & Tools

| Method | Path | Description |
| -------- | ------ | ------------- |
| `GET` | `/api/cron/jobs` | List scheduled jobs |
| `POST` | `/api/cron/jobs` | Create job |
| `POST` | `/api/cron/jobs/:id/run` | Trigger job immediately |
| `GET` | `/api/skills` | List loaded skills |
| `GET` | `/api/subagents` | List sub-agents |

### Wallet & Finance

| Method | Path | Description |
| -------- | ------ | ------------- |
| `GET` | `/api/wallet/balance` | ETH + token balances |
| `GET` | `/api/wallet/address` | Wallet address and chain |
| `GET` | `/api/stats/costs` | Inference cost tracking |
| `GET` | `/api/stats/efficiency` | Model efficiency metrics |

## Parity Tracking

Roboticus includes automated feature parity tracking against roboticus:

```bash
# Run locally
./roboticus parity-audit --roboticus-dir=../roboticus --output=report.md

# Automated via GitHub Actions
# See .github/workflows/parity-audit.yml
```

The parity audit compares subsystems (pipeline, memory, LLM, tools, channels, scheduler, wallet, config, guards, context, skills, dashboard), checks for key function coverage, identifies new Rust files needing Go equivalents, and diffs API endpoints.

## Project Structure

```text
roboticus/
├── cmd/                    # CLI commands (serve, migrate, parity-audit)
├── internal/
│   ├── agent/              # Agent loop, memory, retrieval, policy, skills, tools
│   │   └── tools/          # Built-in tools (filesystem, bash, web, introspect)
│   ├── api/                # HTTP server, dashboard, WebSocket, middleware
│   │   └── routes/         # Route handlers (agent, sessions, memory, cron, admin)
│   ├── browser/            # Browser automation (CDP)
│   ├── channel/            # Channel adapters (telegram, discord, signal, etc.)
│   ├── core/               # Config, types, errors, rate limiting, security
│   ├── daemon/             # Daemon lifecycle, signal handling
│   ├── db/                 # SQLite store, schema, migrations
│   ├── llm/                # LLM client, router, cascade, cache, embedding
│   ├── pipeline/           # 12-stage inference pipeline, guards, decomposition
│   ├── plugin/             # Plugin registry and script execution
│   ├── schedule/           # Durable cron scheduler with lease locking
│   └── wallet/             # Crypto wallet, treasury, x402, secp256k1
├── testutil/               # Test harness
├── .github/workflows/      # CI (parity audit)
└── main.go                 # Entry point
```

## Development

```bash
# Build
go build -o roboticus .

# Run tests
go test ./...

# Fuzz tests
go test -fuzz=FuzzInjectionScoring ./internal/agent/
go test -fuzz=FuzzSchedulerEvaluate ./internal/schedule/

# Lint (if golangci-lint installed)
golangci-lint run ./...
```

## License

See [LICENSE](LICENSE) for details.

---

Built with Go, SQLite, and idiomatic stubbornness.
