# Lessons Learned: Roboticus → Goboticus Rewrite

Architectural findings from analyzing the Rust roboticus codebase and
recreating it in Go. These lessons can be applied back to improve the
original Rust project.

## Final Project Stats

| Metric | Roboticus (Rust) | Goboticus (Go) |
|--------|-----------------|----------------|
| Language | Rust 1.78 | Go 1.26 |
| Source Files | 366 (.rs) | 106 (.go) |
| Lines of Code | 169,769 | 16,342 |
| Lines (excl. generated/tests) | ~32,000 | ~13,700 |
| Crates / Packages | 15 crates | 12 packages |
| Unit Tests | 3,682 (`#[test]`) | 180 |
| Fuzz Targets | 18 | 6 |
| External Deps | 746 (Cargo.lock) | 38 (go.sum) |
| Build (clean) | ~45s (release) | ~3s |
| Service Manager | None | systemd / launchd / Windows SCM (kardianos/service) |

---

## What Roboticus Gets Right

### 1. Connector-Factory Pattern is Excellent
The unified pipeline owning all business logic with thin connectors is the
strongest architectural decision in the codebase. It prevents the most common
form of rot in multi-channel systems: logic duplication and channel-specific
feature divergence. The `PipelineConfig` preset system is elegant.

### 2. Strict Crate Hierarchy
The DAG dependency structure with zero circular dependencies is well-enforced.
The leaf crate (`roboticus-core`) having zero internal dependencies is clean.

### 3. 4-Layer Injection Defense is Production-Grade
The layered approach (regex gating → HMAC trust boundaries → authority-based
access → behavioral anomaly) is well-designed. Each layer catches different
threat vectors. The `ThreatScore` float with clean/caution/blocked thresholds
is a good abstraction.

### 4. Comprehensive CI Pipeline
15 stages including fuzz testing, mutation testing, soak tests, and coverage
gates demonstrates engineering maturity. The architecture enforcement tests
that verify connector compliance are particularly valuable.

---

## What Should Be Fixed in Roboticus

### Critical: In-Memory Delivery Queue (Data Loss)

**Problem:** `channels/delivery.rs` uses `VecDeque` in memory. A process
restart loses all pending messages.

**Fix:** The delivery_queue table already exists in the schema (lines 356-370
of schema.rs). The in-memory queue should be replaced with SQLite-backed
persistence using this existing table. The Rust code already has the DDL;
the runtime code just doesn't use it for the hot path.

**Go approach:** Goboticus reads/writes directly to `delivery_queue` table.
A `container/heap` provides O(log n) next-ready selection vs the O(n) scan
in Rust.

### Critical: Signal Adapter Async/Sync Mutex Conflict

**Problem:** `channels/signal.rs:202-204` uses `std::sync::Mutex` inside an
async context. This blocks the tokio runtime thread during recv(), which can
starve other tasks sharing the same executor.

**Fix:** Replace `std::sync::Mutex` with `tokio::sync::Mutex` for the message
buffer. Or better: replace the `VecDeque` buffer with a `tokio::sync::mpsc`
bounded channel, which provides backpressure and is natively async-safe.

**Go approach:** `chan InboundMessage` with bounded buffer — Go channels are
inherently concurrency-safe with no async/sync mismatch.

### High: Signal Adapter Unbounded Growth

**Problem:** The Signal adapter's message buffer (`VecDeque`) grows
indefinitely if `recv()` is never called. No cap, no backpressure.

**Fix:** Add a capacity bound. When full, either drop oldest messages or
block the listener goroutine (backpressure). The bounded `mpsc` channel
fix above also solves this.

### High: Signal Adapter Missing Rate Limiting

**Problem:** `channels/signal.rs:174-186` sends unlimited RPC calls to
signal-cli daemon. This can DoS the daemon and expose the account to
Signal's server-side rate limits.

**Fix:** Add a `governor::RateLimiter` (Rust) or similar token-bucket
limiter gating all outbound calls to signal-cli.

### Medium: Formatter Multi-Pass String Allocation

**Problem:** `channels/formatter.rs:299-303` chains 3 separate string
operations (`strip_internal_metadata` → `strip_bracket_citations` →
`collapse_blank_lines`), each allocating a new `String`.

**Fix:** Combine into a single-pass formatter using a state machine that
reads the input once and writes to a single output buffer. For messages
>100KB this eliminates ~2 unnecessary allocations.

**Go approach:** `strings.Builder` with a single scan loop handles metadata
stripping, citation removal, and whitespace normalization in one pass.

### Medium: No Phone Number Validation

**Problem:** `channels/signal.rs:158-168` accepts phone numbers without
E.164 validation. Combined with direct embedding in JSON-RPC payloads, this
creates a potential injection vector.

**Fix:** Add a validation function using a phone number library (e.g.,
`phonenumber` crate in Rust). Validate at adapter construction time and
reject invalid numbers at the API boundary.

### Medium: main.rs is 107KB

**Problem:** The `roboticus-server/src/main.rs` file is 107KB — far too large
for a single file. This makes it hard to navigate, test, and modify safely.

**Fix:** The v0.9.8 decomposition into `roboticus-cli` and `roboticus-api`
already helped. The remaining main.rs should be further split so that the
binary entry point is <500 lines, delegating to the CLI and API crates.

**Go approach:** `main.go` is 5 lines. All logic lives in `cmd/` subcommands
and `internal/` packages.

### Medium: Weak Permanent Error Detection in Delivery Queue

**Problem:** `channels/delivery.rs:78-91` classifies errors as permanent
via string matching (`"403 Forbidden"`, `"401 Unauthorized"`). This is fragile
and misclassifies rate-limit 429 responses.

**Fix:** Parse HTTP status codes from error context. Classify 4xx (except 429)
as permanent, 5xx and 429 as transient with exponential backoff.

### Low: Dead Letter Queue Lacks Alerts

**Problem:** `channels/delivery.rs:250-256` logs dead letters at warn level
but provides no external notification mechanism.

**Fix:** Add a metric counter for dead letters. Trigger a webhook or
notification when the dead letter count exceeds a configurable threshold.

### Low: Mutation Testing Coverage Gaps

**Problem:** Mutation survival rates are high in core (57%) and schedule (34%),
both targeting <15%. This indicates many code paths lack assertions that
would catch behavioral changes.

**Fix:** Add targeted property-based tests for the specific modules identified
by the mutation tester. Focus on boundary conditions and error paths.

---

## Architectural Patterns Improved in Go

### 1. Or-Done Pattern (New in Goboticus)

Roboticus uses ad-hoc shutdown coordination (signal handlers, manual channel
closing). Goboticus introduces a systematic **or-done pattern** where every
goroutine wraps channel reads with `core.OrDone(ctx.Done(), ch)`. This
guarantees no goroutine leaks on shutdown.

**Recommendation for Rust:** Adopt a similar pattern using `tokio::select!`
with a cancellation token uniformly across all async tasks. Consider creating
a `CancellableStream` wrapper that combines a `Stream` with a cancellation
`oneshot::Receiver`.

### 2. Persistent Delivery Queue (Fixing Rust Bug)

The delivery queue table already exists in the Roboticus schema but isn't
used by the runtime delivery queue code. Goboticus uses it from day one.
The O(log n) heap-based `next_ready` selection replaces O(n) linear scan.

### 3. Single-Pass Formatter (Performance Fix)

The Go formatter uses `strings.Builder` with a state machine that handles
all formatting transforms in a single pass. Roboticus should adopt a similar
approach — perhaps using a `Write` trait impl that filters on-the-fly.

### 4. Bounded Channel Buffers (Safety Fix)

Go's channels have natural backpressure. For Rust, the Signal adapter should
switch from `VecDeque` + `Mutex` to a bounded `tokio::sync::mpsc` channel.

---

## Technology Trade-Off Notes

| Concern | Rust (Roboticus) | Go (Goboticus) | Winner |
|---------|-----------------|----------------|--------|
| Compile time | ~2min release | ~30s | Go |
| Runtime performance | Faster (zero-cost abstractions) | Very good (GC overhead) | Rust |
| Concurrency model | async/await + tokio | goroutines + channels | Go (simpler, no Pin/Box/async-sync split) |
| Error handling | `Result<T, E>` with `?` | `(T, error)` multi-return | Rust (more ergonomic chaining) |
| Memory safety | Borrow checker at compile time | GC at runtime | Rust (no GC pauses) |
| Deployment | Single static binary | Single static binary | Tie |
| Cross-compilation | Via Cross.toml | Built-in `GOOS/GOARCH` | Go |
| SQLite binding | rusqlite (C FFI) | modernc.org/sqlite (pure Go) | Go (no CGo needed) |
| Learning curve | Steep (lifetimes, traits, async) | Moderate | Go |
| Test tooling | cargo test + proptest | go test + -fuzz -race | Tie |

---

## Postmortem: Full Rewrite Assessment

### Crate-to-Package Size Comparison

| Subsystem | Rust (LOC) | Go (LOC) | Ratio | Notes |
|-----------|-----------|---------|-------|-------|
| Channels (all adapters + delivery + formatter + router) | 11,764 | 3,228 | 3.6x | Biggest compression. Go's `net/http` + `encoding/json` + `chan` replace tokio + reqwest + serde + Arc\<Mutex\<VecDeque\>\>. |
| Agent (ReAct loop, tools, policy, injection, memory, retrieval, orchestration, skills) | 23,046 | 3,360 | 6.9x | Rust's async tool execution, Pin<Box<dyn Future>>, and trait object machinery dominate. Go interfaces + goroutines collapse it. |
| LLM (cache, router, circuit breaker, dedup, streaming, providers) | 10,124 | 1,940 | 5.2x | Go's `net/http` client + `sync.Map` replaces reqwest + DashMap + tower + hyper. SSE parsing is ~40 lines vs ~200. |
| Schedule (cron, heartbeat, worker) | 3,077 | 771 | 4.0x | Cron parsing in Go without a crate (hand-rolled 5-field matcher) vs `cron` + `chrono-tz` crates. |
| Database (store, schema, CRUD) | 18,122 | 1,180 | 15.4x | Most extreme. Rust's typed query builders, row mapping macros, and error conversion dominate. Go's `database/sql` + `Scan()` is terse. |

**Average compression: ~5x.** Not a Go superiority claim — Rust's explicitness buys things Go doesn't have (see below). But for an I/O-bound agent runtime, the ceremony-to-value ratio favors Go.

### What We Gained

**1. Concurrency bugs eliminated by construction (3 critical fixes).**
Go doesn't have Rust's async/sync mutex split. There's no way to accidentally use `std::sync::Mutex` in an async context because Go doesn't have async contexts — goroutines are uniformly preemptive. The Signal adapter's three bugs (sync mutex in async, unbounded buffer, no rate limiting) all stem from Rust async complexity that simply doesn't exist in Go.

**2. Service manager integration where none existed.**
`kardianos/service` — one `Start()`/`Stop()` interface, three platforms. Roboticus runs as a bare process with no OS integration. Goboticus installs as a systemd unit, launchd plist, or Windows Service with `goboticus service install`. This is a deployment maturity gap, not a language gap, but the Go ecosystem made it trivial to close.

**3. Dependency supply chain reduction: 746 → 38.**
Cargo.lock has 746 entries. go.sum has 38. Every transitive dependency is an attack surface for supply chain compromise. The Go standard library's coverage of HTTP, TLS, crypto, JSON, SQL, and testing means fewer external trust decisions.

**4. Build-test-iterate cycle: 15x faster.**
Clean build ~3s vs ~45s. Incremental <1s vs ~8s. `go test -fuzz` runs natively with zero setup. This compounds — across hundreds of daily rebuilds during development, it's the difference between flow state and context-switching.

**5. Persistent delivery queue (fixing a data-loss bug).**
The delivery queue table existed in roboticus's schema DDL but wasn't used by the runtime code. Goboticus uses it from day one. Combined with `container/heap` for O(log n) next-ready selection, this is both a correctness fix and a performance improvement.

### What We Lost

**1. Exhaustive pattern matching.**
Rust's `match` on `enum` variants forces handling every case at compile time. Go's `switch` on a `string` or `int` kind field silently falls through to `default`. When we add a new `BrowserAction` variant in Go, nothing warns us about the 4 switch statements that need updating. In Rust, the compiler catches all of them.

This is the single biggest correctness regression. Mitigated by architecture tests that can check for unhandled cases, but it's a discipline-over-compiler trade.

**2. Error chain provenance.**
Rust's `anyhow::Context` / `thiserror` gives you a full causal chain: `wallet encrypt → aes_gcm seal → invalid nonce length`. Go's `fmt.Errorf("wallet encrypt: %w", err)` achieves the same thing but only if every call site remembers to wrap. Unwrapped errors lose context silently. Roboticus's error chains are richer than goboticus's because Rust forces the issue.

**3. 3,682 tests → 180 tests.**
Roboticus has 20x more test assertions. Some of this is Rust's testing ergonomics (inline `#[test]` per function) vs Go's table-driven style (one `func TestX` with 10 subtests still counts as 1). But the coverage gap is real — goboticus has structural tests proving the architecture works; roboticus has exhaustive property tests proving edge cases. The fuzz targets partially compensate but don't replace handwritten edge-case coverage.

**4. Zero-cost abstractions for compute paths.**
The HNSW ANN index, vector cosine similarity, prompt token counting, and Keccak256 hashing are all CPU-bound. Rust's monomorphization and SIMD intrinsics give 2-5x throughput on these paths. Go's GC and runtime overhead are negligible for HTTP round-trips but measurable for tight loops over embeddings. The wallet package using `crypto/ecdh` with P256 as a secp256k1 placeholder is a concrete example — production would need `go-ethereum/crypto` (CGo) or accept the performance hit.

**5. Lifetime-enforced resource safety.**
Rust's borrow checker guarantees a `CdpSession` can't outlive its `Browser`. Go's `Browser.Stop()` must be called manually, and nothing prevents using a `CdpSession` after the browser is killed. We rely on runtime nil checks and `IsRunning()` guards instead of compile-time guarantees. Same pattern applies to database connections, file handles, and channel subscriptions.

### Production Readiness Gaps

Goboticus is architecturally complete but has gaps before production deployment:

| Gap | Severity | Fix |
|-----|----------|-----|
| Wallet uses P256 not secp256k1 | HIGH | Add `go-ethereum/crypto` or `btcsuite/btcd` |
| Browser CDP is stub (no real WebSocket) | MEDIUM | Integrate `chromedp` or implement CDP WebSocket client |
| No IMAP polling goroutine for email | MEDIUM | Add background IMAP listener with exponential backoff |
| No Discord gateway WebSocket | MEDIUM | Implement gateway connection for real-time events |
| Test coverage ~180 vs 3,682 | MEDIUM | Port critical property tests from roboticus |
| No mutation testing | LOW | Add `go-mutesting` or similar to CI |
| Race detector requires CGo | LOW | CI environment needs CGo-enabled Go for `-race` |
| Yield engine is mock (no real Aave calls) | LOW | Implement via `go-ethereum/ethclient` |

### Honest Assessment: Was the Rewrite Worth It?

**For the bugs alone: yes, unambiguously.** The in-memory delivery queue was a data-loss bug in production. The Signal async mutex was a latency time-bomb. These could be fixed in Rust, but the rewrite forced us to find them — and we found 10 total. A refactor-in-place tends to preserve existing assumptions; a rewrite questions all of them.

**For the code reduction: qualified yes.** 5x less code means 5x less surface area for bugs, 5x less code to review, 5x less to load into your head when debugging at 3am. But some of that compression comes from Go being less explicit — and explicitness prevents bugs that terseness introduces.

**For the cross-platform deployment: yes.** Three platforms, one binary, native service manager integration. This was a real gap in roboticus.

**For the long-term: depends on the team.** If the team is strong in Go and maintaining the runtime is the primary workload, goboticus is the better codebase to evolve. If the team is strong in Rust and the compute-heavy subsystems (HNSW, vector search, browser automation) become the bottleneck, roboticus's zero-cost abstractions pay off more.

### Key Takeaway

The biggest win wasn't Go vs Rust — it was the architectural audit. The rewrite forced a systematic review of every subsystem, every data flow, every error path. That review found 10 bugs, 3 of which were critical. A disciplined code review of the Rust codebase could have found the same bugs without rewriting anything. But it hadn't happened in 15 crates and 32K lines of code — the rewrite created the forcing function.

If you can afford a rewrite, do it for the audit. If you can't, do the audit anyway.

---

## v0.10.0 Addendum — Lessons Applied & New Findings

### Fixes Applied from This Document

The following issues identified in this document were fixed in Roboticus v0.10.0:

| Issue | Fix Applied |
|-------|------------|
| Signal async/sync mutex (Critical) | Replaced `std::sync::Mutex<VecDeque>` with bounded `tokio::sync::mpsc` channel (capacity 256) |
| Signal unbounded buffer (High) | Solved by bounded mpsc — `try_send` returns error when full |
| Signal missing rate limiting (High) | Added `governor::RateLimiter` (token bucket, 5 req/s default) on all outbound `json_rpc()` calls |
| Formatter multi-pass allocation (Medium) | New `clean_content()` single-pass function replaces 3-allocation chain |
| Weak permanent error detection (Medium) | `is_permanent_error()` now extracts HTTP status codes first (429=transient, other 4xx=permanent), falls back to pattern match |
| Dead letter queue alerts (Low) | Atomic `dead_letter_total` counter with configurable alert threshold, error-level log on threshold crossings |
| main.rs is 107KB (Medium) | Already fixed in v0.9.8 crate split; further decomposed in v0.10.0 |

### New Findings During v0.10.0 Implementation

#### 1. No Graceful Daemon Shutdown (NEW — Fixed)

**Problem:** All background daemons (cache flush, heartbeat, delivery drain, cron worker,
mechanic checks) were spawned with bare `tokio::spawn()` and ran infinite loops with no
cancellation mechanism. Shutdown relied entirely on tokio runtime drop, meaning daemons
could be mid-operation when the process exits.

**Fix:** Added `tokio_util::sync::CancellationToken` to `AppState`. All daemon loops now
use `tokio::select!` between `cancel.cancelled()` and their interval tick. A dedicated
shutdown listener task catches SIGINT/SIGTERM and cancels the token, giving all daemons
a clean exit path before the runtime drops.

**Go comparison:** Goboticus used the or-done pattern with `context.Context` cancellation
from day one. This fix brings Rust parity.

#### 2. Test State Construction Fragility (NEW — Observed)

**Problem:** Adding a single field to `AppState` (40+ fields) broke every test that
constructs one. Rust's struct literal syntax requires all fields, and there's no builder
pattern or `..Default::default()` fallback because `AppState` contains non-Default types.

**Recommendation:** Consider a `TestAppStateBuilder` or `#[cfg(test)] impl Default for AppState`
with mock defaults to prevent new fields from being a test-breaking change.

#### 3. Plugin Catalog Was Structurally Missing (NEW — Fixed)

**Problem:** `roboticus plugins install/search` failed with "No plugin catalog available"
because the registry `manifest.json` had no `plugins` section at all. The code used
`.ok_or("No plugin catalog available")` which hard-errored on `None`.

**Fix:** Added empty `plugins` section to manifest.json. Changed error handling to show
a helpful message suggesting `--path` for local installs when the catalog is empty.

#### 4. Parity Audit False Positives Are Persistent (NEW — Observed)

**Problem:** The parity audit uses keyword matching against file names, which creates
persistent false positives. `hybrid_search`, `cron_worker`, `eip3009`, `csp_nonce`, and
`plugin_registry` are all implemented in goboticus under different names/files but flagged
as missing every time.

**Recommendation:** The parity audit should use semantic matching or maintain an explicit
aliases map (e.g., `hybrid_search → retrieval.go`, `eip3009 → x402.go`) to suppress
known false positives.

#### 5. Go Log Capture Is Simpler Than Rust File-Based Approach (NEW — Go Improvement)

**Problem:** Roboticus reads `.log` files from disk for `/api/logs`, which requires
parsing JSON lines from files, handling file rotation, and is fragile to path changes.

**Go improvement:** Goboticus uses an `io.Writer`-based ring buffer injected into zerolog's
multi-writer. This captures logs directly in memory with no disk I/O, no file parsing,
and no rotation handling. The ring buffer is thread-safe and fixed-size (5000 entries).
This is a pattern worth adopting in roboticus — a `tracing::Subscriber` layer that writes
to a shared ring buffer would eliminate the file-reading approach entirely.

#### 6. Tiered Inference Was Already Partially Implemented (NEW — Observed)

**Problem:** During the parity audit, `tiered` was flagged as missing. Investigation
revealed that `internal/llm/tiered.go` already contained a full `ConfidenceEvaluator`
and `EscalationTracker` implementation — it just wasn't wired into the inference flow.
A duplicate implementation was created before discovering the existing one.

**Lesson:** Always grep for existing implementations before writing new code. The parity
audit's keyword matching can mask existing implementations under different file names.
This was documented in CLAUDE.md but still happened — check types and interfaces, not
just file names.

#### 7. Pipeline Trace Capture as First-Class Concern (NEW — Go Improvement)

**Problem:** Roboticus stores pipeline traces as JSON blobs with complex nested types
(`PipelineTrace`, `ReactTrace`, `TraceSpan` with `SpanOutcome` enums). The Go version
simplifies this to a flat `TraceRecorder` with `BeginSpan`/`EndSpan` that automatically
handles timing and nesting.

**Go improvement:** The trace recorder uses auto-closing of active spans (calling
`BeginSpan` automatically ends the previous one), which eliminates a common bug class
where spans are opened but not closed. The `Finish()` method handles cleanup. This is
more ergonomic than the manual namespace + annotate pattern in Rust.

#### 4. Sandbox Error Messages Were Opaque (NEW — Fixed)

**Problem:** When a skill script hit a sandbox boundary (path outside workspace, interpreter
not in whitelist, etc.), the error message said what was blocked but not why or how to fix it.
Operators had to read docs to find the relevant config keys.

**Fix:** All sandbox error messages now include: (a) what was blocked, (b) the specific
config key to adjust (e.g., `[security.filesystem].tool_allowed_paths`), and (c) where
to find it in roboticus.toml.
