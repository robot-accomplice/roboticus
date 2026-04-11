# Changelog

All notable changes to Roboticus are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-04-11

### Theme: The Go Rewrite

Complete ground-up rewrite in Go achieving exact feature parity with the Rust
v0.11.4 reference implementation. 140,000 lines of Go, 329 test files, 3,232
test functions, 24/24 packages passing. This is the cutover release.

### Added

- **Go runtime**: Full rewrite from Rust to Go with modernc.org/sqlite (no CGO)
- **ReAct agent loop**: 25-turn state machine with idle detection and loop prevention
- **25-guard output safety pipeline**: Behavioral, truthfulness, quality, and protocol guards
- **Semantic classifier**: Embedding-based guard scoring with 5 exemplar categories (NARRATED_DELEGATION, CAPABILITY_DENIAL, TASK_DEFERRAL, FALSE_COMPLETION, FINANCIAL_ACTION_CLAIM)
- **10+ LLM providers**: OpenAI, Anthropic, Google Gemini, Ollama, OpenRouter, Moonshot, vLLM, llama-cpp, sglang, docker-model-runner
- **6-axis metascore routing**: Efficacy, Cost, Availability, Locality, Confidence, Speed with ML router
- **3-tier semantic cache**: Exact hash, tool-aware TTL, cosine similarity
- **5-tier memory system**: Working, Episodic, Semantic, Procedural, Relationship with FTS5 + HNSW
- **9 channel adapters**: Telegram, Discord (WebSocket gateway), Signal, WhatsApp, Email (OAuth2), Voice, Matrix (E2E), A2A (X25519+AES), Web (WebSocket)
- **Discord WebSocket gateway**: Full lifecycle with heartbeat, identify/resume, MESSAGE_CREATE dispatch, reconnection with backoff
- **Email OAuth2**: XOAUTH2 SASL authentication for Gmail IMAP
- **MCP client/server**: stdio + SSE transports, live-tested with Playwright (21 tools)
- **Revenue scoring algorithm**: 3-component scoring (confidence/effort/risk) with feedback signals
- **Hybrid FTS5+vector search**: FTS5 MATCH combined with cosine similarity via weighted merge
- **Embedded SPA dashboard**: 9,155-line single-page app with routing profile persistence
- **38 CLI commands**: Full operator surface including models exercise/baseline/reset
- **EVM wallet**: secp256k1, EIP-3009, x402 payments, treasury policy
- **Plugin system**: Managed registry with skill pairing and archive packaging
- **TUI**: bubbletea + lipgloss terminal interface
- **4-layer personality**: OS, FIRMWARE, OPERATOR, DIRECTIVES (TOML, hot-reloadable)
- **Parity test suite**: internal/parity/ package verifying resolved gaps against Rust behavior
- **Release footprint document**: docs/releases/v1.0.0-footprint.md tracking all 101 gap closures

### Changed

- **Prompt ordering**: Firmware before personality (matching Rust prompt.rs)
- **Injection defense**: 5 markers (Rust set), full content replacement with flagging instead of silent strip
- **Money type**: Microdollars replaced with cents (i64, Rust parity), saturating arithmetic
- **Embedding format**: JSON text replaced with 4-byte LE IEEE 754 BLOB (Rust parity)
- **N-gram hash**: Byte trigrams replaced with rune trigrams, removed signed projection
- **Stop word list**: Aligned to Rust's 77-word set (was 63 with wrong mix)
- **Shell validation**: Blanket pattern blocking replaced with Rust's specific compound checks
- **Guard behavioral alignment**: TaskDeferral (7 tools + semantic), ExecutionTruth (11 intents + FALSE_COMPLETION), InternalJargon (NARRATED_DELEGATION > 0.8), InternalProtocol (3-category, no bracket markers), DeclaredAction (removed Go-unique indicators)
- **Consolidation constants**: Extracted magic numbers to named constants (DedupJaccardThreshold, DecayFactor, DecayFloor, PromotionGroupThreshold)
- **Config defaults**: Treasury limits aligned to Rust (daily_transfer=2000, hourly=500, reserve=5, inference=50)
- **A2A rate limit**: Zero value means unlimited (was default to 30)
- **Matrix timestamp**: Server TS replaced with local clock (Rust parity: Utc::now())
- **MCP notifications**: Fixed JSON-RPC notification ID bug (notifications must not have "id" field)
- **Skill formatting**: Flat list replaced with nested subsections (### Skill N)

### Fixed

- **Routing profile persistence**: 6-axis profile now stored directly as RoutingProfileData, load prefers persisted values over lossy-derived defaults
- **HNSW index**: BuildFromStore reads embedding_blob (binary LE) instead of nonexistent embedding_json column
- **Post-turn embedding**: Writes binary BLOB format instead of JSON text into BLOB column
- **Consolidation quiescence gate**: Data-moving phases skip when session active within 5 seconds
- **Cron pipeline compliance**: Cron executor uses RunPipeline (was already correct, verified)
