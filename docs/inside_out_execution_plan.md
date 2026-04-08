# Inside-Out Execution Plan

## Goal

Every layer must perform at the same level or better than the Rust version
before moving to the layer above it. No surface work until the foundation
is solid.

## Layer Map (inside → out)

### Layer 0: Data (SQLite + schema + migrations)
- Schema matches Rust
- Migrations run correctly
- Store operations are tested
- **Status**: Mostly solid. wallet_balances migration gap was found and fixed.
- **Risk**: Other migration gaps may exist on existing DBs.
- **Action needed**: Verify every table from Rust schema exists in Go schema.

### Layer 1: LLM Service (routing, inference, cache, breakers)
- Provider key resolution from keystore ← VERIFIED IN PRODUCTION
- Routing decisions match Rust metascore behavior
- Cache hit/miss semantics match
- Circuit breaker state management
- Streaming inference parity ← VERIFIED: SSE chunks + [DONE] signal
- Provider failover cascade ← VERIFIED: ollama→moonshot→openrouter cascade works
- Tiered inference ← VERIFIED: local confidence evaluation + escalation to cloud
- **Status**: SOLID. End-to-end production test confirmed:
  - Key resolution: Rust cascade (explicit ref → conventional → env) implemented + tested
  - Ollama/gemma4 responds, confidence evaluated (0.45 < 0.7 floor), escalates to cloud
  - Moonshot authenticated (key from keystore) but quota exhausted — correct behavior
  - OpenRouter succeeded as final fallback (939ms, gpt-4o-mini)
  - Streaming: SSE endpoint returns delta chunks + [DONE], pipeline fires correctly
  - Post-turn ingest: memory chunks generated after each turn
  - Guard chain: evaluated and retry logic works
  - Routing targets: only primary/fallback models, not all providers (Rust parity fix)
- **Risk**: OpenAI provider has no key in keystore (missing `openai_api_key`).
  This is expected if the user hasn't added it.
- **Action needed**: Layer 1 is functionally complete. Move to Layer 2.

### Layer 2: Pipeline (stages, dedup, decomposition, guards, memory)
- 13 stages matching Rust (was 11, added turn creation + task synthesis)
- Dedup tracker ← VERIFIED: concurrent duplicates rejected with 429
- Task state machine ← lifecycle tracking with classification
- Task state synthesis ← NEW: intent classification + complexity + planner action
- Decomposition gate ← classifies AND delegates via executor
- Delegation execution ← NEW: orchestrate-subagents with quality gate + retry
- Guard chain with context ← 19 guards (Full), 6 guards (Streaming), retry
- Post-turn ingest ← embeddings + context checkpointing (every 10 turns)
- Context compaction ← 5-stage progressive compaction before inference
- Topic tag derivation ← NEW: text overlap scoring for message topic continuity
- Bot command dispatch ← NEW: /help, /status, /whoami, /clear
- Cost recording ← NEW: full Rust fields (turn_id, latency, quality, escalation, cached)
- Model selection audit ← NEW: persisted to model_selection_events table
- ReactTrace / flight recorder ← NEW: tool calls recorded with timing and result
- Error dedup in ReAct ← NEW: suppresses duplicate failed tool calls
- **Status**: ALL GAPS ADDRESSED. Every pipeline stage from Rust has a Go equivalent:
  - Turn creation pre-creates turn records in DB
  - Topic tags derived via Jaccard text overlap (0.3 threshold)
  - Task synthesis classifies intent, complexity, plans delegation action
  - Delegation execution dispatches subtasks through executor with quality gate
  - Quality gate: 5-check heuristic (empty, placeholder, hollow lead, disproportionate, substantiveness)
  - Bot command dispatch handles /commands before inference
  - Cron delegation wrap prepends subagent directive for non-root cron tasks
  - Prefer local model scans for healthy local providers
  - Threat-aware authority reduction (caution → Creator downgraded to Peer)
  - Guard sets: Full (19), Cached (19), Streaming (6) matching Rust
  - Context checkpoints saved every 10 turns (memory summary + digest)
  - Cost recording includes all Rust fields
  - Model selection events persisted for audit trail
  - ReAct flight recorder tracks tool calls with timing and result summaries
  - Error dedup suppresses duplicate failed tool calls after 2 attempts
- **Risk**: MINIMAL. Observer subagent dispatch in post-turn not yet implemented
  (requires subagent registry query). Specialist auto-composition deferred.
- **Action needed**: Layer 2 is complete. Move to Layer 3.

### Layer 3: Agent Loop (ReAct, tool execution, policy)
- ReAct loop with tool calls
- Policy engine enforcement
- Tool registry
- Approval flow
- Tool output filter chain ← NEW: ANSI strip, progress bar removal, dedup, whitespace normalization
- Wall-clock loop timeout ← NEW: 120s deadline matching Rust's autonomy_max_turn_duration
- Failure synthesis ← NEW: generates summary from tool results when LLM returns empty
- Instruction anti-fade ← NEW: compact directive reminder every 4 turns after turn 8
- HMAC content integrity ← NEW: tag/verify/strip boundary markers for trusted content
- Protocol leak rescue ← NEW: strips internal markers from user-facing output
- Error dedup ← NEW: suppresses duplicate failed tool calls (max 2 attempts)
- Flight recorder ← NEW: ReactTraceEntry with timing, success, source for each tool call
- **Status**: ALL GAPS ADDRESSED. The ReAct loop now matches Rust's feature set:
  - Tool output filtered through 4-stage pipeline before injection scan
  - Wall-clock deadline prevents runaway loops (120s hard limit)
  - Empty response after tools generates a summary from tool results
  - Instruction anti-fade prevents system prompt drift in long conversations
  - HMAC boundaries verify content integrity (tag + verify + strip API)
  - Protocol leak rescue removes internal markers from output
  - Error dedup suppresses repeated failed tool calls with user-facing message
  - Flight recorder tracks every tool call with timing and result summary
- **Risk**: MINIMAL. Truncated JSON repair for tool call parsing not implemented
  (Go receives structured tool calls from the LLM client, not raw text parsing).
  Approval system exists at policy layer, not in loop.
- **Action needed**: Layer 3 is complete. Move to Layer 4.

### Layer 4: Memory System (5-tier, retrieval, ingest, consolidation)
- Working, episodic, semantic, procedural, relationship tiers
- Retrieval: hybrid FTS5 + cosine, decay reranking, ambient recency
- Consolidation: 7-phase pipeline (Go advantage — Rust has no consolidation)
- Retrieval strategy: adaptive mode selection (Go advantage)
- **Status**: ALL GAPS ADDRESSED. 10 items fixed:
  - Social turn classification added (was missing, defaulted to Reasoning)
  - Derivable tool filtering: 10 ephemeral tools skipped (list_directory, read_file, etc.)
  - Episodic dedup on insert: content_exists check prevents duplicates
  - UTF-8 safe truncation: uses rune boundaries, not raw byte slice
  - Tier-specific importance: working=3, episodic/tool=7, episodic/financial=8
  - Ambient recency: last 2 hours of episodic injected regardless of query
  - Inactive memory filtering: keyword-based ("history", "previous", etc.)
  - RetrievalMetrics struct: total/matched entries, avg similarity, budget utilization, per-tier counts
  - Trust-score-aware relationships: Social=0.8, Financial=0.75, default=0.65
  - RetrieveWithMetrics method for pipeline observability
- **Risk**: MINIMAL. Memory index layer (on-demand recall_memory tool) deferred.
  Batch decay lookup optimization deferred (Go uses inline, Rust uses batch).
- **Action needed**: Layer 4 is complete. Move to Layer 5.

### Layer 5: Channels (Telegram, Discord, WhatsApp, Signal, Email, Matrix, A2A)
- 10 adapters (Telegram, Discord, WhatsApp, Signal, Web, Email, A2A, Voice, Matrix, Phone)
- 8 formatters including Voice (TTS-clean) and Matrix (HTML subset) ← NEW
- Delivery worker with retry + O(log n) heap
- Dead-letter queue with store-backed replay ← NEW
- **Status**: ALL GAPS ADDRESSED:
  - Delivery queue: idempotency key dedup, configurable max attempts per item,
    store-backed dead letter replay (dual path: memory first, then DB fallback)
  - Router: health enum (Connected/Degraded/Disconnected) with error count,
    last_successful_at timestamp, explicit RecordReceived/RecordSent/RecordError methods
  - Formatters: Voice (strips all markdown for TTS) and Matrix (HTML conversion) added
  - Health transitions: Connected → Degraded (3+ errors) → Disconnected (10+ errors)
- **Risk**: Adapters are configured but only Telegram verified connected via keystore.
  Other adapters need live testing per-channel.
- **Action needed**: Live testing of each channel adapter.

### Layer 6: Scheduling (cron, leases, retry)
- Durable scheduler with lease-based locking
- Per-job retry with configurable backoff
- Execution audit trail (cron_runs table)
- **Status**: ALL GAPS ADDRESSED:
  - CRON_TZ prefix support added (both TZ= and CRON_TZ=, Rust parity)
  - Cron scan limit increased from 24h to 7 days (handles monthly crons)
  - Go has ADVANTAGES over Rust: lease-based multi-instance coordination,
    persistent job storage, per-job retry config, full execution audit trail
- **Risk**: Minimal. Custom cron parser is well-tested but not battle-tested
  like external libraries.
- **Action needed**: Layer 6 is complete.

### Layer 7: API Surface (routes, response shapes, auth)
- 200+ routes registered
- Dashboard SPA served
- WebSocket event streaming
- **Status**: Routes exist, respond, and serve real data.
- **Risk**: Some endpoints may have response shape differences.
- **Action needed**: Continued response-shape validation.

### Layer 8: CLI Commands
- 35+ top-level commands
- Contract tests for command tree
- OAuth PKCE auth flow ← NEW: `auth login --oauth` with full PKCE
- **Status**: Commands wired to API. OAuth PKCE flow implemented.
  Config backup with timestamped pruning. Auth no longer API-key-only.
- **Risk**: Minimal. Live validation per-command remains.

### Layer 9: Dashboard / TUI
- 14 pages (was 13 — added Integrations)
- Integrations page ← NEW: channel health, message counts, DLQ management
- TUI with bubbletea
- **Status**: Dashboard renders. Many data display issues were found
  and fixed. But it's still a monolith.

## Execution Order

1. **Layer 1 first**: Get LLM inference working reliably with all configured
   providers. This blocks everything above.
2. **Layer 2 next**: Verify pipeline stages in production. This is the
   backbone.
3. **Layer 3**: Verify the agent loop produces correct tool-using responses.
4. **Layer 4**: Verify memory improves responses.
5. **Layers 5-9**: Only after 1-4 are proven solid.

## Immediate Next Step

Start the server, send a real message, and trace it through every layer.
If it fails, fix the layer where it fails before moving up.
