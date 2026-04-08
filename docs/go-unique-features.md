# Go-Unique Features

32 features present in the Go implementation that are absent from the Rust
baseline. These are intentionally preserved -- they represent Go-specific
strengths or operator workflow improvements that emerged during development.

---

## Channel & Delivery

**DKIM email verification (RFC 6376)**
Validates DKIM signatures on inbound email messages, ensuring message
authenticity before processing. Prevents spoofed messages from reaching the
agent loop.

**HMAC-SHA256 trust boundaries**
Cross-component message signing using HMAC-SHA256. Internal messages between
pipeline stages carry a signature that the receiver verifies, preventing
tampering if components are distributed.

**Binary heap delivery queue (O(log n))**
Outbound message scheduling uses a binary heap indexed by `next_ready`
timestamp. This gives O(log n) insert and O(log n) pop-min, compared to a
linear scan of pending messages.

**Idempotency dedup on enqueue**
Content-hash deduplication at enqueue time prevents the same outbound message
from being queued twice. This is distinct from Rust's delivery-time dedup and
catches duplicates earlier in the pipeline.

**Phone/SMS channel adapter**
Twilio-based SMS channel adapter supporting inbound webhook parsing and
outbound message delivery. Includes phone number normalization and carrier
lookup for delivery optimization.

**Voice formatter**
SSML-aware output formatter for voice channel delivery. Converts markdown
responses to Speech Synthesis Markup Language with appropriate prosody hints,
pauses, and emphasis markers.

**Email threading headers (Message-ID, In-Reply-To, References)**
Full RFC 5322 threading support for email delivery. Outbound emails carry
proper Message-ID generation and In-Reply-To/References headers, maintaining
conversation threading in email clients.

**Matrix Megolm ratchet rotation**
Automatic Megolm session key rotation after 100 messages per room. Limits the
blast radius of a compromised session key by bounding the number of messages
decryptable with a single ratchet state.

**InboundMessage.ChatID field**
A cross-channel conversation identifier on inbound messages. Allows the
pipeline to correlate messages across channels (e.g., a user on Telegram and
Discord in the same logical conversation).

**Bot commands (/memory, /memory-stats, /memory-search)**
In-channel operator commands that give users direct access to memory subsystem
diagnostics without needing the API or dashboard. Useful for quick checks
during conversation.

---

## Agent & Personality

**Operator personality layer (3 layers vs Rust's 2)**
Go implements three personality layers: base identity, operator customization,
and per-session adaptation. Rust has base + operator only. The third layer
allows the agent to adjust tone and style based on conversation context.

**Anti-fade instruction reminder in loop**
Periodic re-injection of critical system prompt instructions during long
conversations. Prevents instruction drift as the context window fills and
older instructions get compressed away.

**Synthesis from tool results fallback**
When the LLM fails to produce a response after tool execution, Go synthesizes
a response from the tool results directly. This prevents dead-end turns where
tools succeed but the model returns empty content.

**Nickname refinement**
Progressive user identification that refines how the agent addresses users
over time. Starts with platform handle, learns preferred name through
conversation, and stores the preference per-user.

---

## Guards

**ConfigProtectionGuard**
Detects and blocks attempts to modify agent configuration through tool output
or model-generated commands. Prevents prompt injection attacks that target
the config file.

**FilesystemDenialGuard**
Path-level access control that enforces denied-path patterns before tool
execution. Blocks reads and writes to sensitive paths (e.g., keystore,
system files) regardless of tool authority level.

**ExecutionBlockGuard**
Authority-gated code execution guard. Blocks shell/eval tool calls when the
session authority level is below the tool's risk classification, even if the
policy engine would otherwise allow it.

**SystemPromptLeakGuard**
Scans model output for content that matches system prompt fragments. Detects
and blocks prompt exfiltration attempts where the model is tricked into
repeating its instructions.

**ContentClassificationGuard**
Content safety classification on model output. Flags harmful, deceptive, or
policy-violating content before delivery to the user channel.

**RepetitionGuard (v1)**
Detects repetitive output patterns across turns. Flags sessions where the
model is stuck in a loop producing the same or highly similar responses.

**GuardResult violation list accumulation**
Go's guard chain accumulates all violations across all guards before rendering
a final verdict. Rust short-circuits on the first failure. Go's approach
provides a complete diagnostic picture when multiple guards fail
simultaneously.

---

## Security & Injection

**Consent handling module**
Cross-channel consent management for operations that require user confirmation.
Tracks consent state per user per operation type with configurable expiry.

**Multi-class injection bonus (+0.15 for 3+ patterns)**
When an input matches patterns from 3 or more injection categories
simultaneously (e.g., instruction + authority + override), Go applies a +0.15
bonus to the threat score. This makes compound attacks harder to sneak past
the threshold.

**7 sanitization regexes (vs Rust's 5)**
Go uses 7 regex patterns for input sanitization, covering two additional
attack vectors beyond Rust's 5-pattern set: encoded instruction sequences
and multi-language directive patterns.

**Injection CheckInput: turn_summary filtering**
Input scanning also processes turn summaries before they enter the context
window. Prevents injection payloads that survive summarization from poisoning
subsequent turns.

**BestResult heuristic (longest content wins)**
When multiple LLM responses are available (e.g., from cache + fresh), Go
selects the longest substantive response. This heuristic favors more complete
answers over terse cached responses.

---

## Infrastructure

**Env var fallback in keystore Get()**
The keystore's `Get()` method checks `ROBOTICUS_KEY_{name}` environment
variables as a fallback when a key is not found in the encrypted store. Useful
for CI/CD and container deployments where secrets come from the environment.

**Cron TZ= and CRON_TZ= prefix support**
Cron expressions can be prefixed with `TZ=America/New_York` or
`CRON_TZ=America/New_York` to specify the evaluation timezone. Standard cron
only supports UTC; this extension matches the behavior of popular cron
implementations.

**CLI: channels, logs, metrics, service, tui, web, completion, uninstall**
Eight additional CLI subcommands not present in Rust: `channels` for channel
management, `logs` for log streaming, `metrics` for runtime stats, `service`
for system service lifecycle, `tui` for terminal UI, `web` for dashboard
launch, `completion` for shell completions, and `uninstall` for clean removal.

**OpenAPI spec generation**
Auto-generated OpenAPI 3.0 specification from route registration. Serves at
`/api/openapi.json` and powers the Swagger UI integration in the dashboard.

**Log buffer (ring)**
In-memory ring buffer holding the most recent N log entries. Accessible via
`/api/logs` without requiring log file access. Supports level filtering and
pattern search.

**Problem response (RFC 7807)**
All API error responses use RFC 7807 Problem Details format with `type`,
`title`, `status`, `detail`, and `instance` fields. Provides machine-readable
error classification for API consumers.
