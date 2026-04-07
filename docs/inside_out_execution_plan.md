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
- Provider key resolution from keystore ← JUST FIXED, untested in production
- Routing decisions match Rust metascore behavior
- Cache hit/miss semantics match
- Circuit breaker state management
- Streaming inference parity
- Provider failover cascade
- **Status**: Partially solid. Key resolution was broken until this session.
  The router has metascore support. Cache exists. Breakers exist.
- **Risk**: Provider auth still fragile (moonshot "Incorrect API key" in logs).
  Fallback cascade may not resolve keys for all providers. Streaming may
  diverge from standard inference context.
- **Action needed**:
  1. Verify every provider resolves keys correctly with keystore
  2. Test actual inference end-to-end with each configured provider
  3. Verify streaming produces identical output to standard inference
  4. Verify cache hits are semantically correct

### Layer 2: Pipeline (stages, dedup, decomposition, guards, memory)
- 11 stages matching Rust
- Dedup tracker ← NEW, untested in production
- Task state machine ← NEW, untested in production
- Decomposition gate ← NEW, wired but not executing delegation
- Guard chain with context ← NEW, retry logic untested
- Post-turn ingest ← NEW, untested
- Context compaction ← NEW, untested
- **Status**: Structure matches Rust. Behavior largely untested in
  production. The decomposition gate classifies but doesn't actually
  delegate work to subagents — it logs the decision and continues.
- **Risk**: HIGH. All six new pipeline features were added in this session
  and have unit tests but zero production validation.
- **Action needed**:
  1. Send real messages through the pipeline and verify each stage fires
  2. Verify dedup actually rejects concurrent duplicates
  3. Verify guard chain retry works end-to-end
  4. Verify post-turn ingest produces real embeddings
  5. Verify compaction keeps context within budget

### Layer 3: Agent Loop (ReAct, tool execution, policy)
- ReAct loop with tool calls
- Policy engine enforcement
- Tool registry
- Approval flow
- **Status**: Exists and mostly matches Rust. But the pipeline-to-loop
  boundary is where many subtle bugs live.
- **Risk**: Tool execution errors, policy denials, and loop detection
  were invisible at Info level until the logging fix.
- **Action needed**:
  1. Verify tool calls work end-to-end (bash, file operations)
  2. Verify policy denials are enforced correctly
  3. Verify loop detection terminates correctly

### Layer 4: Memory System (5-tier, retrieval, ingest, consolidation)
- Working, episodic, semantic, procedural, relationship tiers
- Retrieval with FTS5
- Consolidation and reindex
- **Status**: Data flows in. Retrieval works at a basic level. But
  Rust's retrieval is ANN-backed with decay reranking and structured
  metrics — Go's is simpler FTS-based search.
- **Risk**: Memory quality affects every inference decision.
- **Action needed**:
  1. Verify memory retrieval actually improves response quality
  2. Verify consolidation deduplicates correctly
  3. Add retrieval metrics to pipeline traces

### Layer 5: Channels (Telegram, Discord, WhatsApp, Signal, Email, Matrix, A2A)
- Adapters exist for all channels
- Delivery worker with retry
- Dead-letter queue
- **Status**: Adapters exist. Telegram was detected as connected
  (keystore token found). Others are configured but untested.
- **Risk**: Channel-specific formatting, webhook validation, and
  rate limiting may not match Rust behavior.
- **Action needed**:
  1. Send a real message through Telegram and verify it works
  2. Verify each channel adapter's format output

### Layer 6: Scheduling (cron, leases, retry)
- Durable scheduler with FTS5
- Lease-based locking
- Retry with backoff
- **Status**: Scheduler runs. Cron jobs execute through the pipeline.
- **Risk**: Lease contention, retry semantics, and job recovery may
  differ from Rust.
- **Action needed**:
  1. Verify cron job execution end-to-end
  2. Verify lease prevents double-fire

### Layer 7: API Surface (routes, response shapes, auth)
- 200+ routes registered
- Dashboard SPA served
- WebSocket event streaming
- **Status**: Routes exist and respond. Many response shape fixes were
  made during this session. But behavioral depth varies.
- **Risk**: Some routes return plausible-looking data that doesn't match
  Rust's behavior under the surface.
- **Action needed**: Response-shape validation tests for all
  dashboard-critical endpoints.

### Layer 8: CLI Commands
- 35+ top-level commands
- Contract tests for command tree
- **Status**: Commands exist and are wired to API. Several were upgraded
  from thin wrappers to real operator workflows in this session.
- **Risk**: Some commands still have behavioral gaps vs Rust.

### Layer 9: Dashboard / TUI
- Monolithic SPA (should be modular like Rust)
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
