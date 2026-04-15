# Memory Architecture Specification

> Authoritative reference for the Roboticus memory system.
> Updated for v1.0.5 agentic retrieval architecture.
> Derived from exhaustive analysis of the Rust reference implementation (v0.11.4)
> and extended with the 13-layer agentic AI reference architecture.
>
> **Guiding principle**: Retrieve broadly, reason narrowly, act cautiously, learn continuously.

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Storage Layer](#2-storage-layer)
3. [Ingestion Pipeline](#3-ingestion-pipeline)
4. [Consolidation Pipeline](#4-consolidation-pipeline)
5. [Retrieval & Injection](#5-retrieval--injection)
6. [Agent Tools & API](#6-agent-tools--api)
7. [Agentic Retrieval Architecture (v1.0.5)](#7-agentic-retrieval-architecture)
8. [Known Design Gaps](#8-known-design-gaps)
9. [Go Port Gap Audit](#9-go-port-gap-audit)
10. [Beyond-Parity Improvements](#10-beyond-parity-improvements-go-only)

---

## 1. System Overview

### Architecture Diagram

```
                         USER MESSAGE
                              |
                     +--------v--------+
                     |    Pipeline     |
                     | (context_builder)|
                     +--------+--------+
                              |
              +---------------+---------------+
              |                               |
     +--------v--------+            +--------v--------+
     |   RETRIEVAL      |            |   INGESTION     |
     | (pre-inference)  |            | (post-turn,     |
     |                  |            |  background)    |
     +--------+---------+            +--------+--------+
              |                               |
    +---------+---------+           +---------+---------+
    |                   |           |                   |
    v                   v           v                   v
 [Direct Inject]   [Index Only]  [Tier Storage]   [Index + FTS]
 - Working Memory  - Episodic    - Classify turn   - auto_index()
 - Recent Activity - Semantic    - Store per-tier   - FTS triggers
                   - Procedural  - Embed chunks     - Embed content
                   - Relationship
                   - Obsidian

              MODEL CONTEXT WINDOW
    +------------------------------------+
    | System Prompt (personality, etc.)  |
    +------------------------------------+
    | [Working Memory]                   |  <-- direct inject
    | - goal: finish deployment          |
    | - context: debugging auth          |
    +------------------------------------+
    | [Recent Activity]                  |  <-- direct inject
    | - [14:32] (observation) user asked |
    | - [14:28] (action) ran health check|
    +------------------------------------+
    | [Memory Index -- recall/search]    |  <-- index only (query-aware)
    | - [palm] PUSD project (id: idx1)   |  <-- query-matched entries first
    | - [maintenance] Cleanup (id: idx2) |
    | - [entity] Jon trust=0.9 (id: idx3)|
    +------------------------------------+
    | Conversation History               |
    +------------------------------------+
```

### Core Principles

1. **Memory = Index, Not Storage.** Only working memory and recent activity are
   injected directly. All other tiers appear as compact index entries. The model
   calls `recall_memory(id)` or `search_memories(query)` on demand.

2. **Five-Tier Memory System.** Working (session-scoped), Episodic (event log),
   Semantic (facts/knowledge), Procedural (tool statistics), Relationship
   (entity trust tracking).

3. **Background Ingestion.** Turn processing happens asynchronously after the
   response is sent. Ingestion failures are degraded silently -- they never
   block the response path.

4. **Continuous Consolidation.** A 60-second heartbeat runs 7 consolidation
   phases: index sync, obsidian scan, dedup, tier sync, confidence decay,
   pruning, orphan cleanup.

---

## 2. Storage Layer

### 2.1 Table Schema

#### working_memory (session-scoped scratchpad)
```sql
CREATE TABLE working_memory (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    entry_type  TEXT NOT NULL CHECK(entry_type IN
                  ('goal','note','turn_summary','decision','observation','fact')),
    content     TEXT NOT NULL,
    importance  INTEGER NOT NULL DEFAULT 5,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_working_memory_session ON working_memory(session_id);
```
- No `memory_state` column. Lifetime tied to session.
- Orphan cleanup deletes rows whose session is no longer active.

#### episodic_memory (event log / long-term experiences)
```sql
CREATE TABLE episodic_memory (
    id            TEXT PRIMARY KEY,
    classification TEXT NOT NULL,
    content       TEXT NOT NULL,
    importance    INTEGER NOT NULL DEFAULT 5,
    owner_id      TEXT,
    memory_state  TEXT NOT NULL DEFAULT 'active',
    state_reason  TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_episodic_importance ON episodic_memory(importance DESC, created_at DESC);
```
- Has `memory_state` + `state_reason` for lifecycle management.
- FTS5 trigger-backed (INSERT + DELETE triggers).

#### semantic_memory (facts / key-value knowledge)
```sql
CREATE TABLE semantic_memory (
    id            TEXT PRIMARY KEY,
    category      TEXT NOT NULL,
    key           TEXT NOT NULL,
    value         TEXT NOT NULL,
    confidence    REAL NOT NULL DEFAULT 0.8,
    memory_state  TEXT NOT NULL DEFAULT 'active',
    state_reason  TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(category, key)
);
```
- Has `memory_state` + `state_reason`.
- Upserts on `(category, key)` conflict.

#### procedural_memory (tool execution statistics)
```sql
CREATE TABLE procedural_memory (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    steps         TEXT NOT NULL,
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
```
- No `memory_state`. Lifecycle via success/failure ratios.

#### relationship_memory (entity trust tracking)
```sql
CREATE TABLE relationship_memory (
    id                  TEXT PRIMARY KEY,
    entity_id           TEXT NOT NULL UNIQUE,
    entity_name         TEXT,
    trust_score         REAL NOT NULL DEFAULT 0.5,
    interaction_summary TEXT,
    interaction_count   INTEGER NOT NULL DEFAULT 1,
    last_interaction    TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);
```
- No `memory_state`. Upserts on `entity_id` conflict.

#### memory_index (lightweight summary pointers)
```sql
CREATE TABLE memory_index (
    id            TEXT PRIMARY KEY,
    summary       TEXT NOT NULL,
    source_table  TEXT NOT NULL,
    source_id     TEXT NOT NULL,
    category      TEXT,
    last_verified TEXT,
    confidence    REAL NOT NULL DEFAULT 0.8,  -- Go: 0.8 (Rust: 1.0)
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_memory_index_source ON memory_index(source_table, source_id);
CREATE INDEX idx_memory_index_confidence ON memory_index(confidence DESC);
```
- ID format: `idx-{source_table}-{first 12 chars of source_id}`
- Uses **full** table names: `episodic_memory`, `semantic_memory`, etc.
- Go beyond-parity: default confidence 0.8 (was 1.0) for organic differentiation.

#### memory_fts (FTS5 virtual table)
```sql
CREATE VIRTUAL TABLE memory_fts USING fts5(
    content, category, source_table, source_id
);
```
- Uses **short** table names: `episodic`, `semantic`, `working`
- **NAME MISMATCH**: `memory_fts.source_table` != `memory_index.source_table`

#### embeddings (vector storage)
```sql
CREATE TABLE embeddings (
    id              TEXT PRIMARY KEY,
    source_table    TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    content_preview TEXT NOT NULL,
    embedding_blob  BLOB,
    dimensions      INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_embeddings_source ON embeddings(source_table, source_id);
```
- Stores f32 vectors as little-endian BLOBs.
- Paired with in-memory HNSW index (AnnIndex) for ANN search.

### 2.2 FTS5 Triggers

Only **two** SQL triggers exist, both on episodic_memory:

```sql
CREATE TRIGGER episodic_ai AFTER INSERT ON episodic_memory BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (new.content, new.classification, 'episodic', new.id);
END;

CREATE TRIGGER episodic_ad AFTER DELETE ON episodic_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'episodic' AND source_id = old.id;
END;
```

- **No UPDATE trigger**: If episodic content changes, FTS goes stale.
- **No triggers for semantic/working**: FTS is maintained by application code
  in `store_working()` and `store_semantic()` (DELETE + INSERT in transaction).
- **No FTS for procedural/relationship/learned_skills**: These tiers use
  LIKE fallback in `fts_search()`.

### 2.3 Table Name Inconsistency

| Context | episodic | semantic | working |
|---------|----------|----------|---------|
| memory_fts.source_table | `'episodic'` | `'semantic'` | `'working'` |
| memory_index.source_table | `'episodic_memory'` | `'semantic_memory'` | N/A |

This mismatch means JOINs between memory_fts and memory_index require
normalization (Rust has `normalizeTableName()`).

### 2.4 Memory States

Only `episodic_memory` and `semantic_memory` have `memory_state`:

| State | Description |
|-------|-------------|
| `active` | Default. Eligible for retrieval and indexing. |
| `stale` | Superseded or deduplicated. Excluded from retrieval. |
| `migrated` | Promoted to another tier. `state_reason` has target. |
| `inactive` | Referenced in filters but rarely set explicitly. |

No CHECK constraint -- any string accepted.

---

## 3. Ingestion Pipeline

### 3.1 Flow Diagram

```
Turn completes (response sent to user)
         |
         v  (background tokio::spawn)
    post_turn_ingest()
         |
    +----v----+
    | classify |  -> ToolUse / Financial / Social / Creative / Reasoning
    +---------+
         |
    +----v----+----+----+----+----+
    |    |    |    |    |    |    |
    v    v    v    v    v    v    v
  Work  Rel  Epi  Sem  Proc FTS  Embed
  Mem   Mem  Mem  Mem  Mem  Sync  Gen
   |    |    |    |    |    |     |
   +----+----+----+----+----+----+
                  |
             auto_index()
          (upsert memory_index)
```

### 3.2 Classification Rules

| Turn Type | Condition |
|-----------|-----------|
| `ToolUse` | `tool_results` non-empty (highest priority) |
| `Financial` | >= 2 financial keywords in user message |
| `Social` | Social keywords in combined text |
| `Creative` | Creative keywords in combined text |
| `Reasoning` | Default fallback |

### 3.3 Per-Tier Storage Rules

| Tier | When Stored | Content | Importance |
|------|-------------|---------|------------|
| Working | Always | Assistant response (truncated 200 chars) | 3 |
| Relationship | If session has peer scope | Trust by turn type (Social=0.8, Financial=0.75, other=0.65) | N/A |
| Episodic | ToolUse or Financial only | Non-derivable tool results (200 chars) or financial summary | 7-8 |
| Semantic | Reasoning/Creative + response >100 chars | Session-keyed summary, confidence=0.6 | N/A |
| Procedural | ToolUse only | Increment success/failure count per tool | N/A |

### 3.4 Derivable Tool Exclusion

These tools' outputs are NOT stored in episodic memory because they can be
re-queried:

```
list_directory, get_runtime_context, get_wallet_balance, read_file,
bash, search_files, glob_files, get_memory_stats, get_channel_health,
get_subagent_status, introspect, echo, recall_memory, cron
```

### 3.5 Auto-Indexing

Every `store_*` call triggers `auto_index() -> upsert_index_entry()`:
- Summary: first 150 chars of content
- Confidence: 0.8 (Go beyond-parity; Rust uses 1.0)
- ID: `idx-{source_table}-{source_id[:12]}`

---

## 4. Consolidation Pipeline

### 4.1 Seven Phases

```
  60-second heartbeat
        |
        v
  Phase 0: Legacy derivable cleanup (mark old tool outputs stale)
  Phase 1: Index sync (backfill missing memory_index entries, up to 500)
  Phase 2: Obsidian scan (index new vault notes, remove deleted)
  Phase 3: Within-tier dedup [GATED ON QUIESCENCE]
            - Jaccard similarity >= 0.85
            - Never crosses tiers
            - Losers: stale (if has state) or tombstone confidence=-1.0
  Phase 4: Tier-native index sync
            - Stale source -> confidence = 0.0
            - High-failure procedural (>80%) -> confidence = 0.1
            - Learned skills -> confidence = priority/100.0
  Phase 5: Confidence decay [GATED TO ONCE PER 24 HOURS]
            - factor = 0.995 (0.5% exponential)
            - Only entries with confidence > 0.1
            - ~596 days from 1.0 to prune threshold
  Phase 6: Prune low confidence
            - Threshold: 0.05
            - Preserves tombstones (confidence < 0) and system sentinels
  Phase 7: Orphan cleanup
            - Index entries with no source row
            - Working memory for inactive sessions
            - Orphaned embeddings
            - Orphaned FTS entries
```

### 4.2 Quiescence Detection

```sql
SELECT COUNT(*) FROM sessions
WHERE status = 'active' AND updated_at > datetime('now', '-5 seconds')
```

Only Phase 3 (dedup) is gated. All other phases run unconditionally.

### 4.3 Confidence Lifecycle

```
                          upsert_index_entry()
                               conf = 0.8 (Go) / 1.0 (Rust)
                                   |
                      +------------+------------+
                      |                         |
                 actively recalled          not recalled
                      |                         |
               recall_memory()             0.5%/day decay
               conf += 0.1 (Go)                |
               conf = 1.0 (Rust)           conf ~= 0.05
               last_verified = now              |
                                           Phase 6 prune
                                                |
                                            DELETED

  Special cases:
    stale source -> conf = 0.0 (Phase 4)
    dedup loser -> conf = -1.0 (tombstone, never pruned)
    high-failure procedural -> conf = 0.1
```

---

## 5. Retrieval & Injection

### 5.1 The Two-Stage Pattern

**Stage 1 (Proactive)**: At inference time, inject directly:
- Working Memory (session-scoped)
- Recent Activity (last 2 hours of episodic)
- Memory Index (top 20 entries, query-aware in Go)

**Stage 2 (On-Demand)**: Model calls `recall_memory(id)` or
`search_memories(query)` to fetch content from the memory store.

### 5.2 What the Model Sees

```
[Working Memory]
- context: Debugging auth flow for PUSD integration
- goal: Fix the middleware timeout issue

[Recent Activity]
- [14:32] (observation) User asked about deployment status
- [14:28] (action) Ran health check on production

[Memory Index -- call recall_memory(id) or search_memories(query) for details]
- [semantic|palm] Six months keeping the porch light on... (recall: 4a5ed6d7)
- [obsidian] Palm Promotion Bot Initiative Report (recall: idx-obsidian-Projects/Pal)
- [semantic] PUSD hosting for ClawDBots... (recall: idx-semantic_memory-28c50e05)
- [semantic] Workspace cleanup performed (recall: idx-episodic_memory-ep001)
- [entity] Jon trust=0.9 (recall: idx-relationship_me-rel003)
```

### 5.3 Memory Index Selection (Go: query-aware)

```
BuildMemoryIndex(ctx, store, 20, userQuery)

When query is present:
  Slots 1-7:  LIKE match on memory_index.summary + toolNoiseFilter()
              FTS5 MATCH on memory_fts -> JOIN memory_index + toolNoiseFilter()
  Slots 8-20: Tier-priority top-N (Rust parity)
              ORDER BY tier_priority, confidence DESC, created_at DESC

When query is empty:
  All 20 slots: Tier-priority top-N (Rust parity)
```

### 5.4 Tool Noise Filter

Applied to both query-aware and static index selection:
```sql
AND NOT (summary LIKE 'Executed %: {%' AND summary LIKE '%[]%')
AND NOT (summary LIKE 'Executed %: error:%')
AND NOT (summary LIKE 'Used tool %: Error:%')
AND NOT (summary LIKE 'bash: %')
AND NOT (summary LIKE 'get_runtime_context:%')
-- ... (see toolNoiseFilter() in memory_recall.go)
```

### 5.5 Hybrid Search (Internal Only -- NOT Tool-Exposed)

Used during retrieval for metrics/observability, NOT injected into context:

```
hybrid_score = fts_rank_score * (1 - hybrid_weight)    [FTS results]
             + cosine_similarity * hybrid_weight         [vector results]
```

Default `hybrid_weight = 0.5`. FTS query sanitization: strip non-alphanumeric,
take first 12 tokens, wrap each in quotes, join with OR.

### 5.6 Temporal Decay Re-Ranking

Applied to episodic results only:
```
decay_factor = max(0.5^(age_days / half_life_days), 0.05)
adjusted_similarity = original_similarity * decay_factor
```

Default half-life: 7 days.

| Age | Decay Factor | 0.8 similarity becomes |
|-----|-------------|----------------------|
| 0d  | 1.0         | 0.80                 |
| 7d  | 0.5         | 0.40                 |
| 14d | 0.25        | 0.20                 |
| 30d | 0.055       | 0.044                |
| 180d| 0.05 (floor)| 0.04                 |

### 5.7 Token Budget Allocation

| Tier | % of Total | At L2 (16K tokens) |
|------|-----------|-------------------|
| Working | 30% | 4,800 |
| Episodic | 25% | 4,000 |
| Semantic | 20% | 3,200 |
| Procedural | 15% | 2,400 |
| Relationship | 10% | 1,600 |

Ambient recency gets 1/3 of episodic budget.

---

## 6. Agent Tools & API

### 6.1 Agent Tools

| Tool | Parameters | What it Does | Parity |
|------|-----------|-------------|--------|
| `recall_memory` | `id: string` | Fetch full content from memory_index entry. Reinforces confidence +0.1. | Rust parity (Go: +0.1 vs Rust: =1.0) |
| `search_memories` | `query: string, limit?: int` | FTS5 + LIKE search across all tiers. Returns matches with IDs. | **Go beyond-parity** |

### 6.4 OpenAI-Compatible Tool Call Serialization

Assistant messages with tool_calls must serialize `content` as an explicit
empty string `""`, not omit the field (Go's `omitempty` behavior). Tool result
messages must include `tool_call_id`, `content`, and `name`.

This is enforced in `marshalOpenAI()` via explicit message construction
(not struct serialization), ensuring providers can correlate tool results
back to the originating tool call.

**File**: `internal/llm/client_formats.go`
| `get_memory_stats` | none | Returns 6-store memory counts + live health snapshot. | Go beyond-parity |
| `get_runtime_context` | none | Includes hippocampus table listing. | Rust parity |

### 6.2 API Routes

| Method | Path | Function |
|--------|------|----------|
| GET | `/api/memory/working` | All working memory |
| GET | `/api/memory/working/{session_id}` | Session working memory |
| GET | `/api/memory/episodic` | Episodic memory (with limit) |
| GET | `/api/memory/semantic` | All semantic memory |
| GET | `/api/memory/semantic/categories` | List semantic categories |
| GET | `/api/memory/semantic/{category}` | Category-filtered semantic |
| GET | `/api/memory/search?q=<text>` | FTS5 + LIKE fallback search |
| GET | `/api/memory/health` | Retrieval analytics |
| POST | `/api/memory/consolidate` | Trigger consolidation |
| POST | `/api/memory/reindex` | Backfill memory_index |

### 6.3 CLI Commands

```bash
roboticus memory working --session <id>
roboticus memory episodic --limit 50
roboticus memory semantic --session <category>
roboticus memory search --query "palm"
roboticus memory consolidate
roboticus memory reindex
```

---

## 7. Agentic Retrieval Architecture

> Added in v1.0.5. Replaces the previous "query all tiers with fixed budgets"
> approach with intent-driven, multi-layer retrieval.

### Design Principles

- **Semantic memory** answers: "What is true?"
- **Episodic memory** answers: "What happened before?"
- **Procedural memory** answers: "How do I do this?"
- **Relationship memory** answers: "Who interacts with whom, and how strongly?"
- **Graph facts** answer: "What explicitly depends on what?"
- **Working memory** answers: "What am I doing right now?" (NOT searched — direct injection)

### Retrieval Pipeline (v1.0.6)

```
Query → Decompose (compound → subgoals)
      → Route (intent-driven tier selection)
      → Retrieve (per-tier: BM25 + vector hybrid, mode-aware by tier, relationship evidence with age/provenance, graph facts with typed relations and traversal-aware chains)
      → Rerank (discard weak, boost authority, detect collapse)
      → Assemble (evidence + freshness risks + gaps + contradictions)
      → Working State (direct injection, not searched)
      → LLM Reasoning
      → Verify (unsupported certainty / stale-currentness / contradictions / coverage / unsupported answered subgoals)
```

### Components

| Layer | File | Purpose |
|-------|------|---------|
| Decomposer | `decomposer.go` | Splits compound queries into subgoals |
| Router | `router.go` | Selects tiers + modes based on intent/keywords |
| Retriever | `retrieval_episodic.go`, `retrieval_tiers.go`, `retrieval_path.go`, `retrieval_path_telemetry.go` | Per-tier retrieval with HybridSearch-first semantic/procedural/relationship/workflow paths, per-tier path attribution, and telemetry-gated LIKE retirement support |
| Reranker | `reranker.go` | Evidence filter: authority, recency, collapse |
| Assembler | `context_assembly.go` | Structured output: evidence + freshness risks + gaps + contradictions + provenance |
| Verifier | `internal/pipeline/verifier.go` | Detects unsupported certainty, stale-currentness overclaim, ignored contradictions, missed multi-part coverage, missing action plans, unanchored policy answers, and answered subgoals that lack supporting retrieved evidence |
| Reflection | `reflection.go` | Post-turn episode summaries |
| Persistence | `working_persistence.go` | Working memory across restarts |
| Graph Facts | `043_knowledge_facts.sql`, `manager.go`, `retrieval_tiers.go`, `graph.go`, `consolidation_distillation.go` | Persisted typed relations extracted from semantic knowledge and enriched episode distillation, surfaced as first-class evidence and traversable graph structure |

### Current Behavioral Notes

- Router intent signals are now propagated from production daemon retrieval into memory routing.
- Semantic evidence retains source identity, source label, canonical status, and authority score through reranking and context assembly.
- Relationship evidence now retains source identity, relationship summary, trust-derived score, and age through retrieval and assembly.
- Graph facts are now persisted in `knowledge_facts` with `subject` / `relation` / `object`, source provenance, confidence, and freshness metadata.
- Semantic ingestion now extracts typed facts such as `depends_on`, `owned_by`, `uses`, `blocks`, `causes`, and `version_of` into the graph-fact store.
- Semantic / procedural / relationship / workflow retrieval are now HybridSearch-first; residual `LIKE` paths remain only as telemetry-gated safety nets.
- Retrieval tier methods emit `retrieval.path.<tier>` annotations (`fts`, `vector`, `hybrid`, `like_fallback`, `empty`) through a per-call tracer carried on `context.Context`, so concurrent calls stay isolated without retriever-global state.
- Graph retrieval can now synthesize explicit path evidence between named entities and reverse dependency chains for impact / blast-radius queries.
- Enriched episode distillation now promotes recurring canonical `(subject, relation, object)` triples into `knowledge_facts` via the same canonical write gate used by direct graph ingestion.
- Context assembly surfaces explicit freshness risks when supporting evidence is stale instead of leaving recency buried in scores.
- The verifier now consumes pipeline-computed task hints (intent, subgoals, planned action) when available instead of reconstructing everything from the raw prompt.
- The verifier now parses structured `[Retrieved Evidence]` items from the assembled context and checks answered subgoals for explicit support before letting them stand as resolved.
- The verifier is currently heuristic, not model-based. It acts as a revision gate, not a final proof system.

### Working Memory Lifecycle

- **Shutdown**: persist all active entries with `persisted_at` timestamp
- **Startup**: vet entries (discard stale >24h, low importance ≤3, turn_summaries)
- **Consolidation**: entries surviving multiple cycles promote to episodic memory
- Not a retrieval tier — always injected directly into prompt as active state

---

## 8. Known Design Gaps

These exist in the **Rust reference implementation**. Items marked FIXED have
been addressed in the Go port as beyond-parity improvements.

### 7.1 ~~No Topic-Based Memory Search Tool~~ FIXED (Go beyond-parity)

The `memory_search` tool name appeared in the Rust speculative predictor but
was never implemented. The agent could only recall memories by ID.

**Go fix**: `search_memories(query)` tool in `internal/agent/tools/memory_recall.go`.
FTS5 MATCH + LIKE fallback across all tiers.

### 7.2 ~~Index Is Not Query-Aware~~ FIXED (Go beyond-parity)

Rust's `top_entries()` returns the same 20 entries regardless of query.

**Go fix**: `BuildMemoryIndex()` accepts an optional query. When present, runs
LIKE on summaries + FTS5 MATCH to fill the first 1/3 of slots with
query-matched entries; remaining slots use tier-priority ordering.

### 7.3 ~~Confidence Inflation~~ FIXED (Go beyond-parity)

Rust starts all entries at 1.0 and recall resets to 1.0.

**Go fix**: Default 0.8, recall reinforces +0.1 (capped at 1.0). Creates
organic differentiation over time.

### 7.4 ~~FTS Coverage Gaps~~ FIXED (Go v1.0.6)

All memory-backed stores now have FTS coverage: episodic, semantic, working
(pre-existing), procedural and relationship (added in v1.0.2 via migration 037),
plus `knowledge_facts` (added in v1.0.6 via migration 043). Migration 048
completed the missing trigger surface so INSERT/UPDATE/DELETE synchronization
now holds across the FTS-covered tiers used by HybridSearch-first retrieval.

### 7.5 No UPDATE Trigger on Episodic FTS (OPEN)

If episodic content is updated, the FTS entry becomes stale.

### 7.6 ~~Table Name Mismatch~~ FIXED (Go v1.0.2)

Migration 037 normalizes all existing `memory_fts` and `memory_index` rows to
full table names. Triggers recreated with full names. `normalizeTableName()`
retained for legacy data safety. JOIN simplified to direct match.

### 7.7 Episodic-to-Semantic Promotion (OPEN)

Consolidation doc comments mention promotion but Phase 4 is actually
tier-native index sync, not promotion.

### 7.8 Telemetry-Gated LIKE Retirement (OPEN, operator-driven)

HybridSearch-first retrieval is shipped for semantic, procedural,
relationship, and workflow tiers, but residual `LIKE` blocks remain as safety
nets until production trace telemetry shows they are dormant enough to retire
per tier. `AggregateRetrievalPaths()` is the operator-facing gate.

---

## 9. Go Port Gap Audit

### 8.1 All Fixes Applied (2026-04-11 / 2026-04-12)

| Issue | Status | Files Changed |
|-------|--------|---------------|
| FTS5 MATCH not used in episodic retrieval | FIXED | `internal/agent/memory/retrieval.go` |
| Full memory dump injected (should be index-only) | FIXED | `internal/daemon/daemon.go`, `internal/agent/memory/retrieval.go` |
| Anti-confabulation behavioral contract | FIXED | `internal/agent/prompt.go` |
| Memory index polluted with tool output noise | FIXED | `internal/agent/tools/memory_recall.go` |
| SanitizeFTSQuery exported for reuse | FIXED | `internal/db/hybrid_search.go` |
| `search_memories` tool (beyond-parity) | FIXED | `internal/agent/tools/memory_recall.go`, `internal/daemon/daemon.go` |
| Query-aware memory index (beyond-parity) | FIXED | `internal/agent/tools/memory_recall.go`, `internal/daemon/daemon.go` |
| Confidence normalization (beyond-parity) | FIXED | `internal/agent/tools/memory_recall.go`, `internal/db/schema.go` |
| OpenAI tool_call_id serialization | FIXED | `internal/llm/client_formats.go` |
| FTS trigger completeness + backfill (M3.1) | FIXED | `internal/db/migrations/048_fts_trigger_completeness.sql`, `internal/db/fts_trigger_completeness_test.go` |
| HybridSearch-first retrieval + per-tier path telemetry (M3.2) | FIXED | `internal/agent/memory/retrieval_tiers.go`, `internal/agent/memory/retrieval_path.go`, `internal/agent/memory/workflow.go` |
| Relational distillation into `knowledge_facts` (M8) | FIXED | `internal/agent/memory/reflection.go`, `internal/agent/memory/consolidation_distillation.go` |
| Telemetry-backed dormancy aggregator for LIKE retirement (M3.3) | FIXED | `internal/agent/memory/retrieval_path_telemetry.go` |

### 8.2 Remaining Gaps

| Priority | Gap | Notes |
|----------|-----|-------|
| **P1** | Model tool-calling reliability | `gemma4` doesn't reliably call `search_memories` -- addressed by IntentMemoryRecall baselining (router escalates) |
| **P2** | Telemetry-backed LIKE retirement still requires operator observation | `AggregateRetrievalPaths` shipped; tier-by-tier deletion is intentionally gated on production traces |
| ~~P1~~ | ~~FTS table name mismatch~~ | **CLOSED v1.0.2** -- migration 037 normalizes to full names |
| **P2** | Episodic-to-semantic promotion | Consolidation Phase 4 |
| ~~P2~~ | ~~No FTS for procedural/relationship~~ | **CLOSED v1.0.2** -- triggers added |
| ~~P2~~ | ~~Auto-indexing in ingest path~~ | **CLOSED v1.0.2** -- autoIndex() after every store_* |
| **P3** | No UPDATE trigger on episodic FTS | Stale FTS on content change |

### 8.3 Testing Results (2026-04-12)

**Framework verification**:
- `GET /api/memory/search?q=palm` returns 21 results with real Palm/PUSD content
- Query-aware index surfaces Palm entries when query contains "palm"
- `search_memories` tool correctly queries FTS5 + LIKE fallback
- Kimi K2 successfully completed a 10-turn ReAct loop with tool calls (after
  serialization fix), acknowledged Palm USD by name from memory index
- `gemma4` (local Ollama) has weak tool selection — confabulates instead of
  calling `search_memories`. This is a model capability issue, not framework.

**Regression test suite (15 new tests)**:
- `TestMemorySearchTool_*` (7 tests): search tool FTS, LIKE fallback, confidence
- `TestBuildMemoryIndex_*` (3 tests): query-aware, noise filter, instructions
- `TestRetrieveDirectOnly_*` (3 tests): two-stage injection isolation
- `TestRetrieveEpisodic_FTSUnionStrategy` (1 test): old memories found via FTS
- `TestMarshalOpenAI_*` (2 tests): tool_call_id serialization
- Full suite: 24 packages, all passing, zero regressions

---

## 10. Beyond-Parity Improvements (Go-Only)

These improvements exist in the Go port but NOT in the Rust reference.

### 9.1 `search_memories(query)` Tool

FTS5 + LIKE fallback search across all memory tiers. Returns matching entries
with source table and ID for follow-up `recall_memory(id)` calls.

```
Parameters: { query: string, limit?: int }
Strategy:
  1. FTS5 MATCH on memory_fts (episodic, semantic, working)
  2. LIKE fallback on relationship_memory (entity_name, interaction_summary)
  3. LIKE fallback on procedural_memory (name, steps)
Confidence reinforce: +0.1 per hit (capped at 1.0)
```

### 9.2 Query-Aware Memory Index

`BuildMemoryIndex(ctx, store, 20, userQuery)` -- the injected index is no
longer static. When a query is present:

```
Slot allocation: limit/3 for query-matched, remaining for tier-priority
Strategy 1: LIKE on memory_index.summary (catches all tiers including obsidian)
Strategy 2: FTS5 MATCH on memory_fts -> JOIN memory_index (catches content matches)
Both strategies apply toolNoiseFilter() to exclude tool-output noise.
Deduplication: seen map by entry ID.
```

### 9.3 Confidence Normalization

```
Initial confidence: 0.8 (was 1.0)
Recall reinforcement: MIN(1.0, confidence + 0.1) (was: confidence = 1.0)
Search reinforcement: MIN(1.0, confidence + 0.1)
```

This creates organic differentiation: frequently-recalled memories climb
toward 1.0 while rarely-used ones drift toward pruning via decay.

---

*Document generated 2026-04-12. Source: exhaustive analysis of roboticus-rust v0.11.4 codebase + Go port implementation.*
