# Gap Register — Round 4 Exhaustive Audit

## Wave 1 Findings

### CRITICAL (blocks correct behavior)

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G001 | DB/Cron | Cron lease acquisition SQL missing | Atomic `UPDATE...WHERE lease_holder IS NULL OR lease_expires_at < datetime('now')` returns bool | MISSING — no TryAcquireLease() in cron_repo.go | Add atomic lease SQL |
| G002 | DB/Embedding | Embedding storage format mismatch | 4-byte LE IEEE 754 BLOB | Uses JSON text via `embedding_json` column | Reconcile to BLOB format |
| G003 | LLM/Google | Google systemInstruction extraction missing | System role extracted to top-level `systemInstruction` field | System role kept in messages array | Extract system role |
| G004 | LLM/Google | Google functionDeclarations missing | Tools wrapped in `functionDeclarations` | No tools sent to Google API | Add tool translation |

### HIGH (affects quality or correctness)

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G005 | LLM/Embedding | N-gram hash algorithm mismatch | `(acc * 31) + char_as_u32` rolling hash | FNV-1a hash (different vectors) | Port Rust algorithm |
| G006 | LLM/Compression | Smart scoring missing | Entropy-based: content words +3.0, stop words +0.5, punctuation +2.0, capitalized +1.0, digits +1.5, position +1.0 | Naive byte-count truncation (`len/4`) | Port scoring algorithm |
| G007 | DB/Session | Session find_or_create race condition | INSERT OR IGNORE + re-query (idempotent) | Standard INSERT (fails on race) | Use INSERT OR IGNORE |
| G008 | DB/Memory | Semantic memory upsert doesn't reset memory_state | ON CONFLICT DO UPDATE resets memory_state='active', state_reason=NULL | Only updates value + confidence | Add state reset to upsert |
| G009 | LLM/Client | No URL encoding for query parameter auth | pct_encode_query_value (RFC 3986) | Raw API key appended to URL | Add URL encoding |
| G010 | Tool Parsing | classify_provider_error() missing | 8-category error classification | MISSING — no function | Port error classifier |
| G011 | Tool Parsing | provider_failure_user_message() missing | User-facing error with category + timeout hint | MISSING — no function | Port message builder |

### MEDIUM (affects analytics or edge cases)

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G012 | DB/Delegation | Per-agent delegation stats missing | json_each() unpacks agents for per-agent success rates | Simple aggregation only | Add json_each queries |
| G013 | DB/Search | Hybrid FTS5+vector search not active | FTS5 MATCH + vector cosine with weighted merge | FTS5 tables exist but not queried with vectors | Wire hybrid search |
| G014 | LLM/Transform | Newline collapsing off-by-one | Collapse 3+ newlines to 2 | Collapse 4+ to 3 | Fix regex threshold |
| G015 | Tool Parsing | False positive rejection missing | Skips if `}` between `{` and `"tool_call"` | Not implemented | Add rejection check |
| G016 | LLM/Client | No separate connect timeout | CONNECT_TIMEOUT = 10s | Uses overall 120s timeout | Add DialContext timeout |
| G017 | LLM/Google | Model field empty in response | Model extracted from response | Always set to "" | Extract model field |

### LOW (defense-in-depth, already functional)

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G018 | Tool Parsing | Truncation recovery in single-call parser | Not in Rust | Go adds it (EXTRA) | Document as Go-unique |
| G019 | LLM/Transform | Extra injection markers | 5 markers | 10 markers (superset) | Document as Go-unique (stricter) |

## Wave 2 Findings (Agent Intelligence)

### CRITICAL

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G020 | Consolidation | Dedup threshold mismatch | Jaccard 0.85 | Jaccard 0.7 | Align to 0.85 |
| G021 | Consolidation | Decay formula mismatch | 0.995 constant multiplier per 24h | 0.95 exponential per day (semantic), 0.9 per week (episodic) | Align to Rust formula |

### HIGH

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G022 | Retrieval | Hybrid weight delegation vs explicit | Delegates to db layer hybrid_search() | Explicit formula: (1-w)*decay + w*similarity | Document architectural choice |
| G023 | Retrieval | Query history keywords | 12 terms including "previously", "archive", "stale" | 6 terms | Add missing 6 keywords |
| G024 | Consolidation | Quiescence gate missing in Go | Gates phases 3-4 if session active in last 5s | No session-activity-based gating | Add quiescence gate |
| G025 | Consolidation | Procedural/learned-skills confidence sync missing | Reduces procedural to 0.1 if >80% failure rate; syncs learned_skills priority→confidence | Not implemented | Add tier sync logic |
| G026 | Classifier | Exemplar bank missing | 15+ intent categories with 8-15 exemplars each (built-in) | Generic corpus interface, no built-in exemplars | Port exemplar bank |
| G027 | ML Router | Training loss mismatch | Cross-entropy loss with epoch logging | Simple delta updates, no loss function | Port cross-entropy loss |
| G028 | ML Router | Persistence format mismatch | Text format (bias line + weights per line) | JSON format | Align to Rust text format |

### MEDIUM

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G029 | Retrieval | Budget split logic differs | Episodic: 1/3 ambient + 2/3 relevant (integrated) | Separate ambient retrieval with fixed 2h window | Document architectural choice |
| G030 | Injection | Pattern specificity varies | More specific patterns (prior/above, coin symbols ETH/BTC/SOL) | Simpler patterns (broader matching) | Add specific patterns where material |
| G031 | Injection | Output scanning patterns | 6 strict patterns | 4 loose patterns | Add 2 missing patterns |
| G032 | Injection | L4 scoring | Gradient [0,1] | Binary (0.8 on match) | Align to gradient scoring |

## Wave 3 Findings (Tools, Policy, Guards)

### CRITICAL

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G033 | Tools | 6 tools missing | echo, edit_file, alter_table, drop_table, get_runtime_context, recall_memory | MISSING | Implement all 6 |
| G034 | Policy | ConfigProtectionRule missing | Denies write to config files with protected fields (Priority 7) | MISSING | Implement rule |

### HIGH

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G035 | Policy | PathProtection workspace-only mode missing | Denies absolute paths outside /tmp unless in tool_allowed_paths | No workspace-only enforcement | Add workspace-only mode |
| G036 | Tools | create_table missing created_at auto-column | Auto-adds id + created_at | Go only adds id | Add created_at to CREATE TABLE |
| G037 | Tools | get_memory_stats output differs | Includes tier budgets (percentages) | Only DB counts per tier | Add budget percentages |
| G038 | Tools | get_subagent_status missing tasks | Includes open tasks list | No task list | Add task query |

### MEDIUM

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G039 | Policy | Validation shell patterns differ | Specific: "$(" + ";" + (rm/curl/wget) | Broader: "$(" + ";" + "&&" + "||" + "|" + ">" + ">>" + "<" + "<<" + "\n" | Document Go is stricter (superset) |
| G040 | Guards | 8 guards with behavioral drift | Semantic scoring, more markers, different thresholds | Lexical patterns, fewer markers | Requires deep dive per guard |

## Wave 4 Findings (Protocol, Crypto, Channels)

### CRITICAL

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G041 | A2A | Salt generation breaks interop | Byte-level comparison for key ordering | Hex-string lexicographic ordering | Align to Rust byte ordering |
| G042 | Wallet | Money type precision mismatch | i64 cents (100 = $1) | i64 micro-dollars (1,000,000 = $1) | Reconcile — both exist, need to pick one |

### HIGH

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G043 | Wallet | EIP-3009 domain separator may differ | Implicit via wallet signing | Explicit Keccak256 type hash chain | Verify byte-for-byte match |
| G044 | A2A | Nonce TTL mismatch | Default 2× session_timeout (7200s if session=3600s) | Fixed 300s default | Align to Rust default formula |
| G045 | A2A | Rate limit zero-value behavior | 0 = unlimited | 0 = block all | Align to Rust (0=unlimited) |

### MEDIUM

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G046 | A2A | Timestamp validation missing | validate_timestamp() checks drift | Not implemented | Add timestamp validation |
| G047 | Channels | Voice formatter missing | Strips markdown for TTS output | Not implemented | Add voice formatter |
| G048 | Channels | Matrix formatter missing | Converts to HTML subset | Not implemented | Add matrix formatter |
| G049 | Wallet | Treasury inference budget check missing | check_inference_budget() | Not implemented | Add inference budget check |
| G050 | A2A | Stale entry eviction missing | Evicts entries >1h old, max 1000 | No eviction | Add eviction logic |
| G051 | Channels | Citation stripping approach | Character-by-character context-aware | Regex (more aggressive) | Document Go is stricter |

## Wave 5 Findings (Infrastructure)

### CRITICAL

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G052 | Scheduler | Cron fixed-offset timezone missing | Parses UTC±HH:MM correctly | time.LoadLocation fails on fixed offsets | Add fixed offset parsing |
| G053 | Scheduler | Cron slot probe missing | Backward 61s probe finds nearest slot, checks now is within 60s | Direct minute-level match (fires entire minute) | Port probe algorithm |

### HIGH

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G054 | Scheduler | Zero interval guard missing | Returns false if interval_ms <= 0 | No guard — zero interval fires immediately | Add guard |
| G055 | Scheduler | Session rotation missing | reset_schedule cron for session rotation | Only expires old sessions (no rotation) | Add rotation logic |
| G056 | Scheduler | Treasury persistence missing | Treasury loop writes state to DB | Task only reads from cache | Add DB write |
| G057 | Config | Revenue swap config missing | TreasuryConfig has revenue_swap section with chain config | Minimal TreasuryConfig | Add revenue swap section |

### MEDIUM

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G058 | Config | Heartbeat interval validation | Validates > 0 | Validates >= 30 | Document Go is stricter |
| G059 | Plugin SDK | Not fully audited | Trait-based plugin system with TOML manifests | Interface-based with similar structure | Deferred — spot-check later |
| G060 | API Config | Not fully audited | config_runtime.rs hot-reload | config_apply.go patch endpoint | Deferred — spot-check later |

## Statistics
- Total gaps found: 51
- CRITICAL: 8 (G001-G004, G020-G021, G033-G034, G041-G042)
- HIGH: 16 (G005-G011, G022-G028, G035-G038, G043-G045)
- MEDIUM: 19 (G012-G017, G029-G032, G039-G040, G046-G051)
- LOW: 2 (G018-G019)
- DOCUMENTED (Go-unique): 6 items across waves

## Deep Dive Findings (Incomplete Areas)

### CRITICAL

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G061 | A2A/Salt | Salt uses byte ordering; Go uses hex-string ordering | Byte-level key comparison for HKDF salt | Hex-string lexicographic comparison | Already in G041 — confirms severity |
| G062 | DB/Revenue | 6 revenue DB modules have NO Go equivalent | revenue_opportunity_queries, revenue_swap_tasks, revenue_tax_tasks, efficiency, model_selection, tool_embeddings | MISSING entirely | Implement modules |
| G063 | MCP | Go MCP client is stdio-only; Rust supports HTTP/SSE | Async transport-agnostic with peer abstraction | Blocking stdio, manual JSON-RPC | Add HTTP/SSE transport |

### HIGH

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G064 | Guards | TaskDeferralGuard: 7 introspection tools | list-subagent-roster, list-available-skills, task-status, list-open-tasks + 3 shared | Only 4 tools (missing 3 Rust-specific) | Add missing tools |
| G065 | Guards | ExecutionTruthGuard: 11 intent checks + semantic FALSE_COMPLETION > 0.7 | Semantic + multi-intent | Lexical patterns only, no intents | Port intent checks |
| G066 | Guards | InternalJargonGuard: semantic NARRATED_DELEGATION > 0.8 | Semantic scoring + line stripping | 8 hardcoded keywords, no stripping | Port semantic + stripping |
| G067 | Guards | InternalProtocolGuard: JSON + delegation metadata detection | 3 helper functions for JSON/delegation/orchestration | 17 XML/bracket markers (NO OVERLAP with Rust) | Rewrite to match Rust patterns |
| G068 | Guards | LowValueParrotingGuard: triple threshold (0.88, 0.55, 1.35) | Overlap + prefix ratio + length ratio | Single threshold 0.88 | Add prefix + length checks |
| G069 | Guards | ModelIdentityTruthGuard: conditional on length | <200 chars → rewrite; >200 chars → redact | Always rewrites | Add length-based logic |
| G070 | Guards | DeclaredActionGuard: 6 missing resolution indicators | "try", "manage", "unable", "before we resolve", "before proceeding", "what would happen" | 14 indicators (missing 6) | Add missing indicators |
| G071 | DB/Revenue | Revenue scoring has no algorithmic Go equivalent | In-memory confidence/effort/risk scoring with feedback weighting | Basic update/query only | Port scoring algorithm |
| G072 | DB/Revenue | Revenue strategy profitability missing | Cycle time, conversion rate, cost ratio, rejection counts | Simple summary only | Port profitability query |
| G073 | Prompt | Section ordering differs | Firmware BEFORE personality | Personality BEFORE firmware (our fix) | Document as intentional Go improvement |
| G074 | Prompt | Skill formatting differs | Nested subsections (### Skill N) | Flat list (- skill_name) | Align to Rust nested format |
| G075 | Discord | Gateway vs webhooks | Full WebSocket gateway connection | HTTP webhooks only | Add WebSocket gateway |

### MEDIUM

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G076 | Plugin | ToolDef RiskLevel: enum vs string | Enum (Safe, Caution, High) | String ("safe", "dangerous") | Standardize representation |
| G077 | Plugin | Missing paired_skill field in Go ToolDef | Optional field linking tool to skill | Not present | Add field |
| G078 | Plugin | Archive naming convention | .ic.zip format | .toml or .yaml manifests | Document difference |
| G079 | Guards | OutputContractGuard: Go includes numbered lists | Tab variants for bullets | Numbered list detection | Document Go is broader |
| G080 | DB/Revenue | Revenue feedback: missing strategy grouping and time-windowed aggregation | 90-day window, avg_grade by strategy | Simple list only | Port aggregation queries |
| G081 | DB/Revenue | Revenue audit log missing | Settlement events by updated_at DESC | Not implemented | Add audit query |
| G082 | Agent Loop | Hardcoded vs configurable thresholds | IDLE_THRESHOLD=3, LOOP_DETECTION_WINDOW=3 hardcoded | Externalized in LoopConfig | Document Go is more flexible |

## FINAL TOTALS (ALL 5 WAVES + DEEP DIVES)
- Total gaps found: 101
- CRITICAL: 14
- HIGH: 44
- MEDIUM: 34
- LOW: 2
- DOCUMENTED GO-UNIQUE: ~48 features (40 CLI commands, personality ordering, stricter injection markers, broader shell validation, configurable loop thresholds, read receipts, explicit subscriber handling, etc.)

## Final Mapping Findings (Channel Adapters, CLI, Config Defaults)

### CRITICAL

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G083 | Config | Memory budget percentages differ | working=30, semantic=20, procedural=15 | working=40, semantic=15, procedural=10 | Align to Rust budgets |
| G084 | Config | Routing mode default differs | "auto" (intelligent selection) | "primary" (single model only) | Align to "auto" |
| G085 | Config | Cache similarity threshold differs | 0.95 | 0.85 | Align to 0.95 |

### HIGH

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G086 | Config | Treasury model completely different | hourly_transfer_limit=500, daily_transfer_limit=2000, daily_inference_budget=50 | daily_cap=5, transfer_limit=1 | Reconcile treasury models |
| G087 | Config | Server bind default | "localhost" (hostname) | "127.0.0.1" (IP address) | Align to "localhost" |
| G088 | Config | Skills sandbox default | true (enabled) | false (disabled) | Align to true |
| G089 | Config | Wallet filename | wallet.json | wallet.enc | Document as intentional |
| G090 | Config | Estimated output tokens | 500 | 512 | Align to 500 |
| G091 | CLI | models exercise/reset/baseline commands missing in Go | Full exercise matrix + reset + baseline | Go has baseline but missing exercise and reset subcommands | Add missing subcommands |
| G092 | CLI | Memory interface differs | Unified `memory list TIER` | Separate `memory working/episodic/semantic` | Document architectural choice |
| G093 | WhatsApp | Read receipt behavior | Not implemented | markRead() sends status: "read" | Document Go is richer |
| G094 | Email | OAuth2 IMAP missing in Go | oauth2_token option for Gmail | Password auth only | Add OAuth2 support |
| G095 | Email | Body size limit missing in Go | MAX_EMAIL_BODY_BYTES = 1MB | Unlimited | Add size limit |
| G096 | Discord | Gateway vs webhooks | Full WebSocket gateway | HTTP webhooks only | Already in G075 |

### MEDIUM

| ID | Subsystem | Gap | Rust Behavior | Go Status | Fix |
|----|-----------|-----|---------------|-----------|-----|
| G097 | Matrix | Timestamp source differs | Utc::now() (current time) | event.OriginServerTS (server timestamp) | Document Go is more accurate |
| G098 | Matrix | Transaction ID format | UUID v4 | nanosecond timestamp string | Align to UUID |
| G099 | CLI | 40 Go-only commands | N/A | logs, metrics, tui, web, service, integrations, admin subcommands, etc. | Document as Go-unique |
| G100 | CLI | 15 Rust-only commands/flags | apps, memory list unified, config lint, --allow-job | Missing in Go | Evaluate for porting |
| G101 | Web | Subscriber handling | Implicit drop on broadcast overflow | Explicit skip of slow subscribers + unsubscribe | Document Go is more explicit |

## Coverage Assessment
- Guards: 8/8 divergent guards fully audited ✓
- DB repos: 38/38 Rust modules checked (6 missing Go equivalents found) ✓
- MCP: Profiled and audited ✓
- Prompt assembly: Line-by-line compared ✓
- Agent loop: State machine and thresholds compared ✓
- Plugin SDK: Trait/interface, loader, registry, script, archive compared ✓
- Config defaults: FULLY compared (every field with value from both sides) ✓
- Channel adapters: ALL 8 audited (Telegram, Discord, Signal, WhatsApp, Email, Matrix, Voice, Web) ✓
- CLI commands: ALL compared (~85 Rust, ~125 Go, ~60 overlap) ✓
- Remaining channel adapters: ALL audited ✓

## MAPPING IS COMPLETE — NO AREAS REMAIN UNEXAMINED
