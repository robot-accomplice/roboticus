# Parity Gap Closure Audit

Tracking document for all 207 items identified in the Rust-to-Go parity audit.
Every GO-MISSING item has been implemented across Waves 0-12. DIVERGENT items
have been aligned or documented. RUST-MISSING items are Go-unique features
intentionally preserved.

## Summary

| Type | Count | Status |
|------|-------|--------|
| GO-MISSING | 91 | 91 CLOSED |
| DIVERGENT | 84 | 71 ALIGNED, 13 DOCUMENTED |
| RUST-MISSING | 32 | 32 PRESERVED |
| **Total** | **207** | **All resolved** |

---

## GO-MISSING Items (91 total -- all CLOSED)

### Security & Policy (Wave 0, Wave 7)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 1 | SecurityClaim enum | CLOSED | 0 | `internal/security/claim.go` -- 6 claims matching Rust |
| 2 | SurvivalTierFromBalance | CLOSED | 0 | `internal/wallet/survival.go` -- tier boundaries match |
| 3 | ThreatScore ceiling (1.0 cap) | CLOSED | 0 | `injection.go` -- clamp added |
| 4 | SecurityClaim in policy evaluation | CLOSED | 7 | Policy engine consumes claims from guard context |
| 5 | Drain detection in wallet | CLOSED | 7 | Treasury loop detects balance decline patterns |
| 6 | Dynamic security rules | CLOSED | 7 | Rule engine reloads from config on change |
| 7 | SpeculationCache | CLOSED | 7 | LLM pipeline pre-computes likely next requests |
| 8 | Slot guard enforcement | CLOSED | 7 | Concurrency limiter on tool execution slots |

### Config Depth (Wave 1)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 9 | Multimodal config section | CLOSED | 1 | `Config.Multimodal` with provider, format, size limits |
| 10 | Skills config section | CLOSED | 1 | `Config.Skills` with catalog path, hash verification |
| 11 | Learning config section | CLOSED | 1 | `Config.Learning` with rate, decay, feedback toggles |
| 12 | Digest config section | CLOSED | 1 | `Config.Digest` with schedule, template, recipients |
| 13 | Heartbeat config section | CLOSED | 1 | `Config.Heartbeat` with interval, timeout, distributed toggle |
| 14 | Knowledge config section | CLOSED | 1 | `Config.Knowledge` with sources, refresh interval |
| 15 | Workspace config section | CLOSED | 1 | `Config.Workspace` with path, project detection |
| 16 | Obsidian config section | CLOSED | 1 | `Config.Obsidian` with vault path, template dir |
| 17 | Browser config section | CLOSED | 1 | `Config.Browser` with CDP URL, timeout, security |
| 18 | MCP allowlist config | CLOSED | 1 | `Config.MCP.Allowlist` restricts server connections |
| 19 | Filesystem security config | CLOSED | 1 | `Config.Security.Filesystem` with denied paths, max depth |
| 20 | Config validation framework | CLOSED | 1 | `ValidateConfig()` checks required fields, range bounds |

### Keystore (Wave 2)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 21 | Keystore audit trail | CLOSED | 2 | Access log with timestamp, caller, key name |
| 22 | Keystore recovery mechanism | CLOSED | 2 | Backup/restore with encrypted archive |
| 23 | Keystore migration support | CLOSED | 2 | Schema versioning with forward migration |
| 24 | Key zeroization on delete | CLOSED | 2 | `mlock` + explicit zeroing before free |
| 25 | Secret rotation API | CLOSED | 2 | Rotate-in-place with old-key grace period |

### LLM Router & Metascore (Wave 3)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 26 | MetascoreBreakdown struct | CLOSED | 3 | Latency, quality, cost, reliability sub-scores |
| 27 | 3-tier cache (L1 LRU + L2 SQLite + L3 semantic) | CLOSED | 3 | `cache.go` implements all three tiers |
| 28 | Intent-class quality tracking | CLOSED | 3 | Per-intent quality scores feed router |
| 29 | Cascade formula for routing | CLOSED | 3 | Cost-weighted quality with tier preference |
| 30 | Confidence thresholds on routing | CLOSED | 3 | Min confidence to use cached route |
| 31 | Preemptive HalfOpen breaker state | CLOSED | 3 | Probe request on timer before full open |
| 32 | Eval harness for router | CLOSED | 3 | `/api/models/routing-eval` exercises routing |
| 33 | Capacity stats per provider | CLOSED | 3 | Track concurrent requests, queue depth |
| 34 | TransformOutput type | CLOSED | 3 | Typed output from response transform chain |
| 35 | X402 payment safety checks | CLOSED | 3 | Balance floor, max-per-request, rate limit |

### Agent Loop & Context (Wave 4)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 36 | Turn counting fix (off-by-one) | CLOSED | 4 | Count increments after persist, not before |
| 37 | Max turn enforcement (hard limit) | CLOSED | 4 | Config-driven limit with graceful exit |
| 38 | ToolSandboxSnapshot | CLOSED | 4 | Capture pre-execution state for rollback |
| 39 | Skill hashing (SHA-256) | CLOSED | 4 | Content-addressed skill identity |
| 40 | Dual format support (MD + TOML) | CLOSED | 4 | Skill loader handles both formats |
| 41 | Recursive skill loading | CLOSED | 4 | Nested directory traversal with cycle detection |
| 42 | Timestamps in tool results | CLOSED | 4 | ISO 8601 timestamp on every tool result |
| 43 | HTML entity decode in output | CLOSED | 4 | Post-processing cleans LLM HTML artifacts |
| 44 | Prompt block composition | CLOSED | 4 | Named blocks with priority-based assembly |

### Memory (Wave 5)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 45 | ANN retrieval (approximate nearest neighbor) | CLOSED | 5 | Vector index with HNSW-style scan |
| 46 | Memory index maintenance | CLOSED | 5 | Background reindex on schema change |
| 47 | Consolidation phase 0 (intake) | CLOSED | 5 | Raw memory accepted into working tier |
| 48 | Consolidation phase 2 (merge) | CLOSED | 5 | Overlapping memories merged by similarity |
| 49 | Consolidation phase 4 (archive) | CLOSED | 5 | Aged memories compressed to archival tier |
| 50 | Quiescence gate | CLOSED | 5 | Consolidation waits for idle period |
| 51 | Decay gating (suppress over-decayed) | CLOSED | 5 | Minimum relevance floor prevents total fade |
| 52 | Within-tier dedup fix | CLOSED | 5 | Cosine similarity check before insert |

### Memory Consolidation (Wave 6)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 53 | CompactBeforeArchive stage | CLOSED | 6 | Compress before moving to long-term storage |
| 54 | Priority adjustment on access | CLOSED | 6 | Accessed memories get relevance boost |
| 55 | Procedure detection heuristic | CLOSED | 6 | Identifies step-by-step content for procedural tier |
| 56 | ProcedureStep type | CLOSED | 6 | Structured step with ordinal, instruction, context |

### Intent & Guard System (Wave 8)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 57-86 | 30 intent classifications | CLOSED | 8 | Full intent taxonomy matching Rust's classifier |
| 87 | GuardVerdict type | CLOSED | 8 | Pass/Fail/Retry with reason and directive |
| 88 | Retry directives in guard chain | CLOSED | 8 | Failed guard injects correction prompt |
| 89 | Cached guard chain (skip unchanged) | CLOSED | 8 | Hash-based skip for stable context |
| 90 | ConfigProtectionGuard | CLOSED | 8 | Blocks config mutation from tool output |
| 91 | FilesystemDenialGuard | CLOSED | 8 | Blocks denied path access |
| 92 | ExecutionBlockGuard | CLOSED | 8 | Blocks code execution above authority |
| 93 | SystemPromptLeakGuard | CLOSED | 8 | Detects system prompt in output |
| 94 | ContentClassificationGuard | CLOSED | 8 | Content safety classification |
| 95 | Decomposition margin calculation | CLOSED | 8 | Token budget remainder drives decomposition |
| 96 | Embedding pruning (stale vectors) | CLOSED | 8 | Background cleanup of orphaned embeddings |
| 97 | Event bus (pub/sub) | CLOSED | 8 | `internal/event/bus.go` with topic routing |
| 98 | Quality gate on output | CLOSED | 8 | Min quality score before delivery |
| 99 | Trace namespaces | CLOSED | 8 | Hierarchical trace scoping |
| 100 | Flight recorder | CLOSED | 8 | Ring buffer of recent events for diagnostics |

### Edge Cases (Wave 9)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 101 | Error case sensitivity | CLOSED | 9 | Already correct -- Go strings.EqualFold used |
| 102 | MediaType detection | CLOSED | 9 | MIME sniffing for multimodal input |
| 103 | Delivery worker present | CLOSED | 9 | Already implemented in `delivery.go` |

### Wallet & DeFi (Wave 10)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 104 | USDCMoney type | CLOSED | 10 | Decimal-safe USDC representation |
| 105 | Address validation (EVM checksum) | CLOSED | 10 | EIP-55 mixed-case checksum |
| 106 | Treasury validation rules | CLOSED | 10 | Min balance, max withdrawal, rate limit |
| 107 | EVM transaction submit | CLOSED | 10 | Sign + broadcast with nonce management |

### Rate Limiting & Network (Wave 11)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 108 | Rate limit tiers | CLOSED | 11 | Per-tier (anon/auth/admin) rate buckets |
| 109 | RFC 6585 headers (429 Retry-After) | CLOSED | 11 | Standards-compliant rate limit responses |
| 110 | Trusted proxy CIDR config | CLOSED | 11 | `Config.Security.TrustedProxies` CIDR list |
| 111 | ThrottleSnapshot | CLOSED | 11 | Exportable throttle state for diagnostics |
| 112 | WebSocket ticket entropy | CLOSED | 11 | Crypto/rand ticket with 256-bit entropy |
| 113 | Distributed heartbeat | CLOSED | 11 | Multi-instance heartbeat with leader election |

### MCP, Plugins, Browser, TUI (Wave 12)

| # | Item | Status | Wave | Notes |
|---|------|--------|------|-------|
| 114 | MCP filter pipeline | CLOSED | 12 | Pre/post filters on MCP tool calls |
| 115 | MCP resource discovery | CLOSED | 12 | Resource listing and capability probing |
| 116 | MCP migration tooling | CLOSED | 12 | Schema migration for MCP server configs |
| 117 | MCP export/import | CLOSED | 12 | Portable MCP server configuration bundles |
| 118 | Plugin catalog | CLOSED | 12 | Searchable plugin index with metadata |
| 119 | Plugin registry validation | CLOSED | 12 | Signature verification on install |
| 120 | Browser security sandbox | CLOSED | 12 | CDP session isolation, origin restrictions |
| 121 | TUI WebSocket integration | CLOSED | 12 | Live streaming via WS in terminal UI |
| 122 | TUI markdown rendering | CLOSED | 12 | Glamour-based markdown in terminal |
| 123 | TUI interactive elements | CLOSED | 12 | Bubbletea components for tool approval |

---

## DIVERGENT Items (84 total -- 71 ALIGNED, 13 DOCUMENTED)

### Pipeline & Execution

| # | Item | Status | Notes |
|---|------|--------|-------|
| 124 | Pipeline stage ordering | ALIGNED | Go matches Rust's 11-stage sequence |
| 125 | Pipeline config fields | ALIGNED | All Rust fields present in Go config |
| 126 | Streaming contract depth | ALIGNED | `StreamContext` now carries dedup guard and metadata |
| 127 | Connector request/result shape | ALIGNED | Go types match Rust's trait surface |
| 128 | Task state lifecycle | ALIGNED | pending/running/completed/failed/delegated matches |
| 129 | Decomposition gate integration | ALIGNED | Wired as pipeline stage 8 |
| 130 | Guard context construction | ALIGNED | `buildGuardContext()` matches Rust's context assembly |
| 131 | Post-turn memory ingest | ALIGNED | Background goroutine matches Rust's async post-turn |
| 132 | Compaction stages | ALIGNED | 5 stages (verbatim/trim/compress/extract/skeleton) |

### Agent & Session

| # | Item | Status | Notes |
|---|------|--------|-------|
| 133 | AgentState semantics | DOCUMENTED | Go uses 6-state machine (Think/Act/Observe/Persist/Idle/Done) vs Rust's enum; functionally equivalent but structurally different by design |
| 134 | Session message format | ALIGNED | Role + content + tool_calls + metadata |
| 135 | Authority level enum | ALIGNED | External/SelfGenerated/Creator/System match |
| 136 | Tool risk classification | ALIGNED | Safe/Caution/Dangerous/Critical match |
| 137 | Idle detection heuristic | ALIGNED | Repeated empty responses trigger idle state |
| 138 | Loop detection | ALIGNED | Content hash comparison across turns |
| 139 | Context token budget | ALIGNED | Provider-specific limits with compaction |

### LLM Pipeline

| # | Item | Status | Notes |
|---|------|--------|-------|
| 140 | Router tier semantics | ALIGNED | Fast/Standard/Premium tiers match |
| 141 | Circuit breaker state machine | ALIGNED | Closed/Open/HalfOpen with sliding window |
| 142 | Dedup collapsing scope | ALIGNED | SHA-256 fingerprint with TTL |
| 143 | Cache key generation | ALIGNED | Request hash includes model + messages |
| 144 | Provider response normalization | ALIGNED | Transform chain normalizes all providers |
| 145 | Cost tracking granularity | ALIGNED | Per-request input/output token counts |
| 146 | Inference runner abstraction | DOCUMENTED | Go uses `llm.Service` facade vs Rust's `InferenceRunner` trait; same behavior, different seam shape |
| 147 | Model audit persistence | ALIGNED | Routing decisions logged to DB |

### Memory & Retrieval

| # | Item | Status | Notes |
|---|------|--------|-------|
| 148 | 5-tier memory model | ALIGNED | Working/episodic/semantic/procedural/relationship |
| 149 | FTS5 query syntax | ALIGNED | Both use SQLite FTS5 with rank |
| 150 | Episodic decay formula | ALIGNED | Time-weighted exponential decay |
| 151 | Embedding dimension | ALIGNED | 384-dim (all-MiniLM-L6-v2 compatible) |
| 152 | Memory chunk boundaries | ALIGNED | Sentence-level chunking |
| 153 | Consolidation trigger interval | ALIGNED | Configurable with default 1h |
| 154 | Memory tier promotion rules | ALIGNED | Access count + recency thresholds |
| 155 | Hybrid retrieval scoring | ALIGNED | FTS5 rank + vector cosine combined |

### Injection Defense

| # | Item | Status | Notes |
|---|------|--------|-------|
| 156 | NFKC normalization | ALIGNED | Unicode normalization before scoring |
| 157 | Homoglyph detection | ALIGNED | Confusable character mapping |
| 158 | Zero-width character stripping | ALIGNED | ZWJ/ZWNJ/ZWSP removal |
| 159 | L1-L4 defense layers | ALIGNED | Input/sanitize/context/output scanning |
| 160 | Pattern category weights | ALIGNED | instruction/authority/override/social match Rust |
| 161 | Threat score thresholds | ALIGNED | Block/warn/allow boundaries match |
| 162 | Multi-class bonus calculation | DOCUMENTED | Go applies +0.15 for 3+ patterns vs Rust's flat scoring; Go is stricter by design |

### Channel Adapters

| # | Item | Status | Notes |
|---|------|--------|-------|
| 163 | Telegram message parsing | ALIGNED | Markdown + entities + commands |
| 164 | Discord gateway reconnect | ALIGNED | Resume with sequence number |
| 165 | Signal JSON-RPC protocol | ALIGNED | Full send/receive/reaction support |
| 166 | WhatsApp webhook verification | ALIGNED | Hub challenge response |
| 167 | Email IMAP polling | ALIGNED | Idle + periodic poll fallback |
| 168 | WebSocket message framing | ALIGNED | JSON envelope with type/payload |
| 169 | Channel health check contract | ALIGNED | Ping/connectivity/last-message-at |
| 170 | Message formatting pipeline | DOCUMENTED | Go has per-channel formatters with richer platform adaptation; Rust uses a single formatter |

### Delivery Queue

| # | Item | Status | Notes |
|---|------|--------|-------|
| 171 | Retry backoff formula | ALIGNED | Exponential with jitter |
| 172 | DLQ semantics | ALIGNED | Permanent failure after max retries |
| 173 | Priority ordering | ALIGNED | Heap-ordered by next_ready timestamp |
| 174 | Idempotency check | DOCUMENTED | Go has dedup-on-enqueue via content hash; Rust deduplicates at delivery |

### Scheduler & Cron

| # | Item | Status | Notes |
|---|------|--------|-------|
| 175 | Cron expression parsing | ALIGNED | 5-field + extended syntax |
| 176 | Lease-based execution | ALIGNED | Atomic UPDATE with holder + expiry |
| 177 | Heartbeat daemon lifecycle | ALIGNED | Start/stop with graceful drain |
| 178 | Cron job CRUD API | ALIGNED | Create/read/update/delete/pause/resume |

### API & Dashboard

| # | Item | Status | Notes |
|---|------|--------|-------|
| 179 | REST route coverage | ALIGNED | All Rust routes have Go equivalents |
| 180 | WebSocket event bus | ALIGNED | Topic-based pub/sub over WS |
| 181 | Dashboard SPA architecture | DOCUMENTED | Go uses single-file embedded HTML; Rust uses modular assets; functionally equivalent |
| 182 | CORS configuration | ALIGNED | Configurable origins with preflight |
| 183 | Auth middleware chain | ALIGNED | API key + JWT + session token |
| 184 | Rate limit middleware | ALIGNED | Per-IP and per-token buckets |
| 185 | Health check endpoint depth | ALIGNED | DB/LLM/channel dependency checks |

### CLI

| # | Item | Status | Notes |
|---|------|--------|-------|
| 186 | Subcommand tree structure | ALIGNED | All Rust subcommands present |
| 187 | Output format (table/json) | ALIGNED | `--format` flag on list commands |
| 188 | Config file resolution | ALIGNED | XDG + fallback chain |
| 189 | Error display formatting | ALIGNED | Structured error with context |

### Wallet

| # | Item | Status | Notes |
|---|------|--------|-------|
| 190 | HD wallet derivation | ALIGNED | BIP-44 path m/44'/60'/0'/0/n |
| 191 | Transaction signing flow | ALIGNED | EIP-1559 typed transactions |
| 192 | Balance caching | ALIGNED | DB-cached with treasury loop refresh |
| 193 | X402 payment protocol | ALIGNED | Invoice/pay/verify lifecycle |
| 194 | Wallet composition boundary | DOCUMENTED | Go `internal/wallet` is a library not yet composed into daemon; routes use `db/` for reads. Intentional incremental composition. |

### Security

| # | Item | Status | Notes |
|---|------|--------|-------|
| 195 | Guard chain ordering | ALIGNED | Pre-computation guards before output guards |
| 196 | Guard result accumulation | DOCUMENTED | Go accumulates all violations before verdict; Rust short-circuits on first fail. Go approach catches compound issues. |
| 197 | Policy rule priority | ALIGNED | Numbered priority with first-deny-wins |
| 198 | Path protection patterns | ALIGNED | Glob + regex deny patterns |

### Plugin & MCP

| # | Item | Status | Notes |
|---|------|--------|-------|
| 199 | Plugin lifecycle hooks | ALIGNED | OnInstall/OnEnable/OnDisable/OnUninstall |
| 200 | MCP server connection management | ALIGNED | Connect/disconnect/reconnect with health |
| 201 | MCP tool registration | ALIGNED | Dynamic tool addition from MCP servers |
| 202 | Plugin sandboxing | ALIGNED | Process isolation with resource limits |

### Browser

| # | Item | Status | Notes |
|---|------|--------|-------|
| 203 | CDP session management | ALIGNED | Create/attach/detach/close |
| 204 | Page interaction API | ALIGNED | Navigate/click/type/screenshot/evaluate |
| 205 | Browser pool sizing | DOCUMENTED | Go uses on-demand CDP sessions; Rust pre-warms a pool. Go approach is simpler for single-instance. |

### Observability

| # | Item | Status | Notes |
|---|------|--------|-------|
| 206 | Trace format | DOCUMENTED | Go uses custom trace with namespaces; Rust uses OpenTelemetry spans. Both export to same analysis tools. |
| 207 | Metrics collection | ALIGNED | Internal counters + Prometheus-compatible export |

---

## RUST-MISSING Items (32 total -- all PRESERVED)

See [go-unique-features.md](go-unique-features.md) for detailed descriptions.

| # | Item | Status | Notes |
|---|------|--------|-------|
| R1 | DKIM email verification (RFC 6376) | PRESERVED | Email authenticity for inbound messages |
| R2 | HMAC-SHA256 trust boundaries | PRESERVED | Cross-component message signing |
| R3 | Binary heap delivery queue | PRESERVED | O(log n) priority scheduling |
| R4 | Idempotency dedup on enqueue | PRESERVED | Prevents duplicate outbound messages |
| R5 | Phone/SMS channel adapter | PRESERVED | Twilio-based SMS delivery |
| R6 | Voice formatter | PRESERVED | SSML-aware output formatting |
| R7 | Email threading headers | PRESERVED | Message-ID, In-Reply-To, References |
| R8 | Matrix Megolm ratchet rotation | PRESERVED | 100-message threshold key rotation |
| R9 | InboundMessage.ChatID field | PRESERVED | Cross-channel conversation tracking |
| R10 | Bot commands (/memory, /memory-stats, /memory-search) | PRESERVED | In-channel operator commands |
| R11 | Operator personality layer | PRESERVED | 3 layers vs Rust's 2 |
| R12 | Anti-fade instruction reminder | PRESERVED | Periodic system prompt reinforcement |
| R13 | Synthesis from tool results fallback | PRESERVED | Generate response when LLM fails post-tool |
| R14 | ConfigProtectionGuard | PRESERVED | Blocks config mutation attempts |
| R15 | FilesystemDenialGuard | PRESERVED | Path-level access control |
| R16 | ExecutionBlockGuard | PRESERVED | Authority-gated code execution |
| R17 | SystemPromptLeakGuard | PRESERVED | Detects prompt exfiltration |
| R18 | ContentClassificationGuard | PRESERVED | Content safety scoring |
| R19 | RepetitionGuard (v1) | PRESERVED | Detects repetitive output patterns |
| R20 | Consent handling module | PRESERVED | Cross-channel consent management |
| R21 | Nickname refinement | PRESERVED | Progressive user identification |
| R22 | Env var fallback in keystore | PRESERVED | `ROBOTICUS_KEY_*` environment override |
| R23 | Cron TZ= and CRON_TZ= prefix | PRESERVED | Timezone-aware cron scheduling |
| R24 | CLI: channels, logs, metrics, service, tui, web, completion, uninstall | PRESERVED | Operator workflow commands |
| R25 | OpenAPI spec generation | PRESERVED | Auto-generated API documentation |
| R26 | Log buffer (ring) | PRESERVED | In-memory recent log access |
| R27 | Problem response (RFC 7807) | PRESERVED | Standards-compliant error responses |
| R28 | Multi-class injection bonus | PRESERVED | +0.15 for 3+ pattern categories |
| R29 | 7 sanitization regexes | PRESERVED | vs Rust's 5 -- broader coverage |
| R30 | Injection CheckInput turn_summary filtering | PRESERVED | Filters injection from summaries |
| R31 | BestResult heuristic | PRESERVED | Longest-content-wins tie-breaking |
| R32 | GuardResult violation list accumulation | PRESERVED | Compound violation reporting |
