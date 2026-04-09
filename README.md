# Roboticus

```text
        ╔═══╗
        ║◉ ◉║
        ║ ▬ ║
        ╚═╤═╝
      ╔═══╪═══╗       R O B O T I C U S
  ╔═══╣ ▓▓║▓▓ ╠═══╗   Autonomous Agent Runtime
  █   ║ ▓▓║▓▓ ║   █
      ╚══╤═╤══╝
         ║ ║
        ═╩═╩═
```

> **One binary. One database. One agent that remembers, reasons, and acts.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/robot-accomplice/roboticus/ci.yml?branch=develop&logo=github&label=CI)](https://github.com/robot-accomplice/roboticus/actions)
[![Tests](https://img.shields.io/badge/tests-287_files-brightgreen)](.)
[![Fuzz](https://img.shields.io/badge/fuzz-12_targets-blue)](.)
[![Channels](https://img.shields.io/badge/channels-8-purple)](.)
[![Providers](https://img.shields.io/badge/providers-10-orange)](.)
[![Guards](https://img.shields.io/badge/guards-25-red)](.)

Roboticus is an autonomous agent runtime that ships as a single Go binary backed by a single SQLite database. It manages its own personality, memory, tools, scheduling, financial operations, and multi-channel communication. No external dependencies beyond an LLM provider.

**Multi-model routing** | **5-tier memory** | **8 channels** | **25-guard output pipeline** | **On-chain wallet** | **Full dashboard SPA**

---

## What Makes Roboticus Different

Most agent frameworks are libraries you call. Roboticus is a **runtime you deploy**. It boots, connects to your channels, loads your personality, and runs autonomously. When you message it on Telegram at 3am, it remembers your last conversation, checks its memory for context, routes to the best available model, enforces its guardrails, and responds in character.

- **Personality system** with OS/Firmware/Operator/Directives layers (TOML-based, hot-reloadable)
- **Claim-based security** with `min(max(grants), min(ceilings))` authority composition
- **Metascore routing** across 6 dimensions (efficacy, cost, availability, locality, confidence, speed)
- **25 output guards** protecting against fabrication, repetition, identity confusion, protocol leaks, and more
- **Durable scheduling** with lease-based distributed locking
- **On-chain wallet** with EIP-3009 payment signing and yield optimization

---

## Architecture

```text
┌────────────────────────────────────────────────────────────────────┐
│                      HTTP / WebSocket / SSE                        │
│   Dashboard SPA (embedded)  │  REST API  │  SSE Streaming          │
├────────────────────────────────────────────────────────────────────┤
│                    Connector-Factory Pipeline                      │
│  Validate → Injection Defense → Session → Decomposition →          │
│  Skill Match → Shortcut → Authority → Delegation →                 │
│  ReAct Loop → Guard Chain (25 guards) → Memory Ingest              │
├────────────────────────────────────────────────────────────────────┤
│  Agent Loop         │  Memory (5-tier)     │  Tools (20+)          │
│  ┌───────────────┐  │  Working             │  bash, read_file      │
│  │ Think (LLM)   │  │  Episodic (decay)    │  write_file, edit_file│
│  │ Act (tools)   │  │  Semantic (UPSERT)   │  search_files, glob   │
│  │ Observe       │  │  Procedural (stats)  │  list_directory, cron │
│  │ Persist       │  │  Relationship        │  introspect, recall   │
│  │ Policy (7)    │  │  + FTS5 + ANN index  │  alter/drop_table     │
│  └───────────────┘  │                      │  get_runtime_context  │
├────────────────────────────────────────────────────────────────────┤
│  Channels (8)       │  LLM Service          │  Scheduler           │
│  Telegram           │  Metascore Router     │  Cron (5-field + TZ) │
│  Discord            │  Cascade Optimizer    │  Lease-based locking │
│  Signal             │  Circuit Breaker      │  Interval + One-shot │
│  WhatsApp           │  3-tier Semantic Cache│  Session rotation    │
│  Voice (STT/TTS)    │  Dedup + Compression  │                      │
│  Email (IMAP/SMTP)  │  Tiered Inference     │  Wallet              │
│  Matrix (E2E)       │  ML Router (logistic) │  secp256k1 ECDSA     │
│  A2A (X25519+AES)   │  10 Bundled Providers │  x402 EIP-3009       │
├────────────────────────────────────────────────────────────────────┤
│  Personality Layer       │  Security                               │
│  OS.toml (identity)      │  Claim-based RBAC                       │
│  FIRMWARE.toml (rules)   │  4-layer injection defense              │
│  OPERATOR.toml (context) │  7 policy rules + ConfigProtection      │
│  DIRECTIVES.toml (goals) │  Prompt HMAC-SHA256 trust boundaries    │
├────────────────────────────────────────────────────────────────────┤
│                    SQLite + FTS5 + WAL Mode                        │
│              30 migrations │ 25+ tables │ Pool(8)                  │
└────────────────────────────────────────────────────────────────────┘
```

**Connector-Factory pattern**: All business logic lives in the pipeline. Channel adapters and HTTP routes are thin connectors that parse input, call `pipeline.RunPipeline()`, and format output. Route handlers never import the agent package directly. This is enforced by architecture tests.

---

## Personality System

Roboticus agents have a layered personality defined in TOML files in the workspace directory:

| Layer | File | Purpose | Example |
| ------- | ------ | --------- | --------- |
| **OS** | `OS.toml` | Identity, voice, personality | "Be genuinely helpful. Have opinions. Earn trust through competence." |
| **Firmware** | `FIRMWARE.toml` | Hard rules and guardrails | "MUST: Disclose uncertainty honestly. MUST NOT: Fabricate sources." |
| **Operator** | `OPERATOR.toml` | Who you serve | "Operator: Jon. Role: Developer. Timezone: Europe/Amsterdam." |
| **Directives** | `DIRECTIVES.toml` | Goals and missions | "Monthly: Ship v0.12.0. Priority: Agent efficacy." |

The personality is injected as the **first section** of the system prompt (before rules), with explicit framing: "This is WHO YOU ARE, not optional guidance." Voice parameters (formality, verbosity, humor, warmth) are included as a structured profile alongside the narrative text.

---

## Inference Engine

### Metascore Routing

Every configured model gets a real-time **6-axis profile**:

| Axis | Range | Source |
| ------ | ------- | -------- |
| **Efficacy** | [0, 1] | Per-(model, intent-class) quality observations |
| **Cost** | [0, 1] | Sigmoid-normalized inverse cost (free=1.0) |
| **Availability** | [0, 1] | Circuit breaker health x capacity headroom |
| **Locality** | [0, 1] | Complexity-adjusted local preference |
| **Confidence** | [0.6, 1] | Cold-start penalty (below 10 observations) |
| **Speed** | [0, 1] | Measured latency score |

The **metascore** is a weighted combination of all 6 axes, with weights configurable per deployment. The router selects the highest-scoring available model, filtering out breaker-blocked and capacity-saturated providers.

### Other Routing Features

- **Cascade optimizer** — expected utility formula accounting for weak-model escalation cost
- **Tiered inference** — cache hit (free) > local model (fast) > cloud model (quality)
- **Circuit breaker** per provider with preemptive half-open for soft degradation
- **3-tier semantic cache** — exact hash > tool-aware TTL > cosine similarity
- **Request dedup** with concurrent call coalescing
- **Smart compression** — entropy-based token scoring (content words, punctuation, position bias)
- **ML router** — logistic regression with cross-entropy training from preference data
- **10 bundled providers** — Ollama, OpenAI, Anthropic, Google, OpenRouter, vLLM, sglang, llama-cpp, docker-model-runner, Moonshot

### Baseline & Exercise

```bash
roboticus models baseline -n 3   # Flush scores, exercise all models 3x with 20-prompt matrix
roboticus models exercise gemma4  # Exercise single model across 4 intent classes
```

The exercise matrix tests 20 prompts across 4 intent classes (Execution, Delegation, Introspection, Conversation) and 5 complexity levels, with per-intent latency scorecards (Avg/P50/P95).

---

## Memory System

| Tier | Storage | Retrieval | Decay |
| ------ | --------- | ----------- | ------- |
| **Working** | Per-session goals, notes, summaries | Session-scoped, importance-ranked | None (session-bound) |
| **Episodic** | Past events with classification | FTS5 + cosine similarity hybrid | `0.5^(age/halflife)`, floor 0.05 |
| **Semantic** | Category/key/value facts | UNIQUE(category, key) with upsert | Confidence x 0.995 per 24h |
| **Procedural** | Tool usage patterns | Name lookup | Failure rate monitoring |
| **Relationship** | Entity trust scores | Entity ID lookup | Interaction-based |

### Retrieval

Hybrid search combining FTS5 full-text matching with cosine similarity of embeddings, weighted by configurable `hybrid_weight` (default 0.5). Budget allocation per tier: working 30%, episodic 25%, semantic 20%, procedural 15%, relationship 10%.

### Consolidation

7-phase background pipeline:

1. Mark derivable tool outputs stale
2. Index backfill (batched to 500)
3. Within-tier dedup (Jaccard > 0.85)
4. Tier confidence sync (procedural failure rate, learned-skills priority)
5. Confidence decay (0.995 constant, gated to once per 24h)
6. Importance decay (episodic, after 7-day grace)
7. Orphan cleanup (FTS, embeddings, inactive working memory)

Quiescence gate skips dedup if a session was active in the last 5 seconds.

---

## Guard Chain

25 output guards organized in dependency order, with 3 chain variants:

| Chain | Guards | When |
| ------- | -------- | ------ |
| **Full** | 25 | Standard inference |
| **Cached** | 21 | Cache hits (excludes Perspective, DeclaredAction, UserEcho, ActionVerification) |
| **Streaming** | 6 | SSE (SubagentClaim, CurrentEventsTruth, PersonalityIntegrity, InternalJargon, NonRepetition, InternalProtocol) |

### Guard Categories

**Truthfulness**: ExecutionTruth (intent-gated, 11 intents), ModelIdentityTruth (length-based rewrite vs redact), CurrentEventsTruth (12 stale-knowledge markers), LiteraryQuoteRetry (refusal detection), ActionVerification (financial claim vs tool execution), FinancialActionTruth

**Behavioral**: SubagentClaim (15 markers + short-turn exemption), TaskDeferral (8 introspection tools), InternalJargon (line stripping + infrastructure markers), DeclaredAction (20 action verbs, 20 resolution indicators), Perspective (first-person narration), InternalProtocol (JSON + delegation + orchestration + XML markers)

**Quality**: EmptyResponse (retry, not rewrite), NonRepetition (cross-turn echo + fresh-delta detection), LowValueParroting (triple threshold: overlap 0.88, prefix 0.55, length 1.35), OutputContract (exact bullet count), UserEcho (8-word contiguous match)

**Safety**: SystemPromptLeak, ConfigProtection, FilesystemDenial, ExecutionBlock, DelegationMetadata, ContentClassification, PersonalityIntegrity (13 foreign identity markers)

Each guard can **Pass**, **Rewrite** (deterministic fix), or **RetryRequested** (re-run inference with guard-specific directives and token budgets).

---

## Channels

| Channel | Protocol | Send | Receive | Notable |
| --------- | ---------- | ------ | --------- | --------- |
| **Telegram** | Bot API | MarkdownV2 | Long-poll + webhook | 18-char escape set, media attachments |
| **Discord** | HTTP (webhooks) | Native Markdown | Webhook ingest | 2000-char chunking |
| **Signal** | JSON-RPC 2.0 | Plain text | signal-cli daemon | E2E encrypted, rate limited |
| **WhatsApp** | Cloud API v21.0 | Templates | Webhook + HMAC verify | Read receipts, E.164 validation |
| **Voice** | OpenAI API | TTS (tts-1) | STT (whisper-large-v3) | 16kHz sample rate |
| **Email** | IMAP/SMTP | Markdown (HTML) | 30s poll interval | Threading (In-Reply-To/References), 1MB body limit |
| **Matrix** | Client v3 | HTML subset | /sync polling | Optional E2E (Olm/Megolm), UUID transaction IDs |
| **A2A** | Custom | AES-256-GCM | X25519 ECDH | Nonce replay prevention, timestamp validation, 256 session cap |

### Delivery Queue

Binary heap (O(log n)) with exponential backoff: 0s, 1s, 5s, 30s, 5m, 15m+. 9 permanent error patterns (case-insensitive). Idempotency dedup on enqueue. Dead-letter with replay support.

---

## Security

### Claim-Based Authority

Every message entry point resolves a `SecurityClaim` using the composition algorithm:

```text
effective_authority = min(max(positive_grants...), min(negative_ceilings...))
```

Positive grants OR across authentication layers (any layer can grant). Negative ceilings AND (strictest restriction wins). Six claim sources: ChannelAllowList, TrustedSenderID, APIKey, WebSocket Ticket, A2A Session, Anonymous.

### Injection Defense

4-layer defense with gradient scoring:

1. **L1**: Input pattern scanning (instruction, encoding, authority, financial — 4 classes, +0.15 multi-class bonus)
2. **L2**: Content sanitization (7 regex patterns)
3. **L3**: Homoglyph folding (28 Cyrillic-to-Latin mappings) + HTML entity decoding + NFKC normalization
4. **L4**: Output scanning (6 gradient-scored patterns)

### Policy Engine

7 rules at ascending priority:

1. **Authority** — risk level vs sender authority (Creator/SelfGenerated/Peer/External)
2. **CommandSafety** — blocks Forbidden risk tools unconditionally
3. **Financial** — amount thresholds + drain/withdraw_all detection
4. **PathProtection** — workspace-only enforcement, protected paths, traversal detection
5. **RateLimit** — per-tool sliding window (30/min default)
6. **Validation** — argument size (100KB), shell injection patterns
7. **ConfigProtection** — denies write tools targeting config files with protected fields

---

## Wallet & Payments

- **secp256k1 ECDSA** with secure key zeroization
- **AES-256-GCM** encrypted storage (Argon2id KDF, byte-compatible with Rust keystore)
- **x402 protocol** — EIP-712 domain separator + EIP-3009 transferWithAuthorization signing
- **Treasury policy** — per-payment cap, daily limits, minimum reserve, inference budget
- **Yield engine** — Aave V3 supply/withdraw on Base L2 (USDC)
- **Keystore** — audit trail, panic recovery, file change detection, legacy passphrase migration

---

## Scheduling

- **Cron expressions** — 5-field with `TZ=` / `CRON_TZ=` prefix support (IANA + UTC fixed offsets)
- **Slot probing** — backward 61s probe prevents missed firings and false positives
- **Lease-based locking** — atomic SQL UPDATE prevents double-fire across instances
- **Session rotation** — `reset_schedule` cron for periodic session archival
- **Distributed heartbeat** — per-domain intervals (treasury 5m, yield 10m, memory 1m, maintenance 1m, session 1m, discovery 5m)
- **Survival-tier adaptation** — 2x slowdown on LowCompute/Critical, 10x on Dead

---

## Dashboard

Embedded SPA served from the binary via `go:embed`. No build step, no npm, no CDN.

- **12 pages** — Overview, Sessions, Context, Memory, Skills, Agents, Scheduler, Metrics, Efficiency, Wallet, Workspace, Settings
- **4 themes** — AI Purple (default), CRT Orange, CRT Green, Psychedelic
- **Real-time** — WebSocket event streaming with ticket-based auth (32-byte entropy, 30s TTL)
- **Security** — CSP nonce injection, X-Frame-Options, HSTS, RFC 6585 rate limit headers
- **Charts** — Sparkline cost/token graphs, SVG spider routing profiles

---

## Quick Start

```bash
# Build
go build -o roboticus .

# Interactive setup
./roboticus setup

# Start the agent
./roboticus serve

# Or with custom settings
./roboticus serve --port 8080
```

Open `http://localhost:18789` for the dashboard.

### First Run

```bash
# Set up personality
./roboticus setup personality

# Add a provider API key
./roboticus keystore set OPENAI_API_KEY sk-...

# Scan for available models
./roboticus models scan

# Baseline all models
./roboticus models baseline -n 2

# Check status
./roboticus status
```

---

## Configuration

Configuration lives at `~/.roboticus/roboticus.toml` with `ROBOTICUS_` environment variable overrides.

```toml
[agent]
name = "Roboticus"
workspace = "~/.roboticus/workspace"

[server]
port = 18789
bind = "localhost"

[models]
primary = "ollama/gemma4"
fallbacks = ["openai/gpt-4o-mini", "anthropic/claude-sonnet-4-20250514"]

[models.routing]
mode = "auto"                    # auto | primary | metascore
confidence_threshold = 0.9
local_first = true
cost_aware = true

[memory]
working_budget = 30.0
episodic_budget = 25.0
semantic_budget = 20.0
procedural_budget = 15.0
relationship_budget = 10.0

[cache]
similarity_threshold = 0.95

[treasury]
daily_cap = 5.0
per_payment_cap = 1.0

[security]
workspace_only = true
deny_on_empty_allowlist = true

[skills]
sandbox_env = true
hot_reload = true
script_timeout_seconds = 30
```

### Bundled Providers

Pre-configured and available without TOML entries:

| Provider | URL | Tier | Format |
| ---------- | ----- | ------ | -------- |
| Ollama | `localhost:11434` | T1 (local) | OpenAI |
| sglang | `localhost:30000` | T1 (local) | OpenAI |
| vLLM | `localhost:8000` | T1 (local) | OpenAI |
| docker-model-runner | `localhost:12434` | T1 (local) | OpenAI |
| llama-cpp | `localhost:8080` | T1 (local) | OpenAI |
| OpenAI | `api.openai.com` | T3 (cloud) | OpenAI |
| Anthropic | `api.anthropic.com` | T3 (cloud) | Anthropic |
| Google | `generativelanguage.googleapis.com` | T3 (cloud) | Google |
| OpenRouter | `openrouter.ai/api` | T2 (proxy) | OpenAI |
| Moonshot | `api.moonshot.cn` | T2 (proxy) | OpenAI |

---

## API

### Core

| Method | Path | Description |
| -------- | ------ | ------------- |
| `POST` | `/api/agent/message` | Send message (returns JSON with session_id, content, model, tokens, cost, react_turns) |
| `POST` | `/api/agent/message/stream` | SSE streaming inference |
| `GET` | `/api/agent/status` | Agent state, active model, provider health |
| `GET` | `/api/health` | Health check |
| `GET` | `/api/config` | Current configuration |
| `PUT` | `/api/config/apply` | Patch configuration at runtime |
| `POST` | `/api/ws-ticket` | WebSocket auth ticket (32-byte, 30s TTL) |

### Sessions & Memory

| Method | Path | Description |
| -------- | ------ | ------------- |
| `GET/POST` | `/api/sessions` | List / create sessions |
| `GET` | `/api/sessions/:id` | Session detail with messages |
| `GET` | `/api/memory/working` | Working memory (session-scoped) |
| `GET` | `/api/memory/episodic` | Episodic memory (importance-ranked) |
| `GET` | `/api/memory/semantic` | Semantic knowledge store |
| `GET` | `/api/memory/search?q=` | Cross-tier hybrid search |
| `POST` | `/api/memory/consolidate` | Trigger consolidation pipeline |

### Scheduling & Administration

| Method | Path | Description |
| -------- | ------ | ------------- |
| `GET/POST` | `/api/cron/jobs` | List / create scheduled jobs |
| `POST` | `/api/cron/jobs/:id/run` | Trigger job immediately |
| `GET` | `/api/skills` | Loaded skills |
| `GET` | `/api/subagents` | Sub-agent roster |
| `GET` | `/api/wallet/balance` | ETH + token balances |
| `GET` | `/api/stats/costs` | Inference cost tracking |
| `GET` | `/api/routing/profile` | Current metascore weights |
| `GET` | `/api/models/available` | Discovered models |

Error responses use **RFC 9457 Problem Details** (`application/problem+json`).

---

## Project Structure

```text
roboticus/
├── cmd/                    # CLI commands (~40 commands)
├── internal/
│   ├── agent/              # Agent loop, memory, retrieval, policy, skills, tools
│   │   ├── memory/         # 5-tier memory + consolidation (7 phases)
│   │   ├── orchestration/  # Multi-agent decomposition
│   │   ├── policy/         # 7-rule policy engine
│   │   ├── skills/         # Skill loader + hot-reload
│   │   └── tools/          # 20+ built-in tools
│   ├── api/                # HTTP server, dashboard, WebSocket, middleware
│   │   └── routes/         # Route handlers (thin connectors)
│   ├── browser/            # Browser automation (CDP + URL/selector validation)
│   ├── channel/            # 8 channel adapters + delivery queue + formatters
│   ├── core/               # Config, types, security claims, keystore, personality
│   ├── daemon/             # Daemon lifecycle, channel wiring, heartbeat
│   ├── db/                 # SQLite store, 25+ tables, 30 migrations
│   ├── llm/                # LLM client (5 formats), router, cache, embedding
│   ├── mcp/                # Model Context Protocol client
│   ├── pipeline/           # 12-stage pipeline, 25 guards, intent registry
│   ├── plugin/             # Plugin registry, script execution, catalog
│   ├── schedule/           # Durable scheduler, heartbeat, domain loops
│   ├── security/           # OS-level sandboxing
│   ├── session/            # Session types
│   ├── tui/                # Terminal UI (bubbletea)
│   └── wallet/             # Wallet, treasury, yield, x402, secp256k1
├── testutil/               # TempStore, MockLLMServer, seed helpers
├── docs/                   # Architecture, diagrams, parity audit
├── scripts/                # Soak/fuzz runner
└── main.go
```

---

## Testing

```bash
go test ./...                            # Full suite (287 test files, 23 packages)
go test -race ./...                      # With race detector
go test -v -run TestLiveSmokeTest .      # Smoke test (boots server, hits 60+ endpoints)
SOAK_ROUNDS=10 ./scripts/run-soak-fuzz.sh  # Soak + fuzz battery
```

### CI Pipeline

Lint (golangci-lint) > Test (race detector, ubuntu + macOS) > Smoke > Fuzz (12 targets) > Soak > Architecture > Build > Security (govulncheck)

### Test Infrastructure

- `testutil.TempStore(t)` — isolated SQLite per test
- `testutil.MockLLMServer(t, handler)` — mock LLM for integration tests
- 12 fuzz targets across injection, formatting, scheduling, and phone validation
- Soak tests with behavioral checks (no stale knowledge, no identity leaks, no metadata exposure)
- Architecture tests enforce Connector-Factory pattern

---

## Rust Parity

Roboticus is a Go implementation of the [Rust reference](https://github.com/robot-accomplice/roboticus-rust). Parity is tracked exhaustively:

- **101 behavioral gaps** identified through function-body-level profiling
- **61 gaps closed** (all critical, most high-priority)
- **18 documented as Go-unique** features (stricter injection detection, configurable thresholds, richer CLI)
- **22 deferred** structural items (embedding format migration, revenue DB modules, MCP HTTP/SSE transport, Discord WebSocket gateway)

See `docs/round4-exhaustive/gaps/gap-register.md` for the complete audit.

```bash
./roboticus parity-audit --rust-dir=../roboticus-rust
```

---

## License

See [LICENSE](LICENSE) for details.

---

Built with Go, SQLite, and an unreasonable commitment to getting every detail right.
