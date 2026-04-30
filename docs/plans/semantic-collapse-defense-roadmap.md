# Semantic Collapse Defense Roadmap

Research attribution: this roadmap draws on the retrieval and agentic RAG
source set listed in [Research References](#research-references), especially
[ColBERTv2](https://arxiv.org/abs/2112.01488) for late interaction and
compression, the [Agentic RAG Survey](https://arxiv.org/abs/2501.09136), and
[A-RAG: Hierarchical Retrieval Interfaces](https://arxiv.org/html/2602.03442v1).

> **Purpose**: Progressive hardening of Roboticus memory retrieval against
> semantic collapse — the geometric degradation of retrieval precision as
> corpus size grows beyond ~10K entries in high-dimensional embedding space.
>
> **Status**: Draft — awaiting review
> **Date**: 2026-04-14
> **Thesis**: Defense-in-depth across 5 phases grouped into 3 chunky
> releases. Each release is a complete, measurable improvement — no partial
> deliveries.

---

## Release Schedule

| Release | Phases | Theme | Rationale |
|---------|--------|-------|-----------|
| **v1.0.5** | 1 + 2 | **Read-path overhaul** — BM25, real ANN, metrics, partitioning | Both phases touch the retrieval read path only. Phase 1's metrics become the baseline that validates Phase 2's ANN. No ingestion changes, no new external calls. Ship together with benchmark suite. |
| **v1.1.0** | 3 + 4 | **Intelligence layer** — reranking + persistent knowledge graph | Phase 2's sub-linear ANN creates the latency budget that Phase 3's reranker consumes. Phase 4 changes the write path (ingestion + consolidation) so it ships alongside the read-path refinement, not before it. |
| **v1.2.0** | 5 | **Agentic retrieval** — query decomposition, intent routing, iterative refinement | Needs real production data from v1.0.5 + v1.1.0 to tune decomposition heuristics and routing tables. Shipping this without that data means tuning in the dark. |

---

## Current State Assessment

### What Exists Today

| Component | File | Status |
|-----------|------|--------|
| Hybrid search (FTS5 + HNSW) | `internal/db/hybrid_search.go` | ✅ Implemented |
| Vector index (**misnomer: not HNSW**) | `internal/db/hnsw.go` | ⛔ Exhaustive k-NN (flat array + linear scan). Named `HNSWIndex` but contains zero graph structure — no layers, no navigable small world, no sub-linear search. See [Misnomer Warning](#misnomer-warning-hnswgo) below. |
| Retrieval strategy selector | `internal/agent/memory/strategy.go` | ✅ Mode switching: keyword / recency / hybrid / ANN |
| Configurable hybrid weight | `internal/agent/memory/retrieval.go` | ✅ `HybridWeight` in `RetrievalConfig` |
| Time-decay ranker | `internal/agent/memory/ranking.go` | ✅ Exponential decay with half-life |
| FTS5 full-text index | `internal/db/schema.go` | ✅ `memory_fts` table with triggers |
| Knowledge graph (in-memory) | `internal/agent/knowledge.go` | ⚠️ Subject/relation/object triples, not persisted |
| Five-tier memory system | `internal/db/memory_repo.go` | ✅ Working, episodic, semantic, procedural, relationship |
| Memory consolidation pipeline | `internal/agent/memory/consolidation.go` | ✅ 7-phase heartbeat cycle |

### Misnomer Warning: `hnsw.go`

> ⚠️ **`internal/db/hnsw.go` is NOT an HNSW implementation.** The struct is
> named `HNSWIndex` and the file is named `hnsw.go`, but the actual
> implementation is an exhaustive brute-force k-NN search: a flat
> `[]HNSWEntry` slice scanned linearly with `cosineSimilarity()` on every
> query. There are no graph layers, no navigable small world structure, no
> sub-linear search properties whatsoever.
>
> **Performance**: O(n) per query. At 768 dimensions × float64, each entry
> is ~6KB. A 10K corpus means scanning ~60MB per query; 50K means ~300MB.
>
> **Why this matters**: The name creates a false sense of security. A future
> contributor seeing `HNSWIndex` in use might assume ANN is handled and skip
> scaling concerns. Phase 2 of this roadmap renames the current
> implementation to `BruteForceIndex` and builds actual ANN alongside it.

### Key Vulnerabilities

1. **Vector search is exhaustive, not approximate**: `hnsw.go:Search()` does a linear scan with cosine similarity — O(n) per query. Despite the filename, there is no graph structure. Will degrade rapidly past a few thousand entries. See misnomer warning above.

2. **No corpus partitioning**: All memory tiers share one FTS index and one vector space. No namespace boundaries.

3. **No reranking stage**: Hybrid search returns weighted fusion results directly — no cross-attention refinement pass.

4. **FTS scoring is position-based, not relevance-based**: `HybridSearch()` uses `1.0 - (i * 0.05)` rank decay — a rough proxy, not BM25 scoring.

5. **Knowledge graph is ephemeral**: Facts live in-memory only, lost on restart. No graph traversal for retrieval augmentation.

6. **No query decomposition**: Complex queries hit the retrieval system as monolithic strings.

---

## Phase 1: Hybrid Search Hardening _(v1.0.5 — Part 1)_

> **Goal**: Maximize the value of the existing hybrid architecture.
> **Estimated effort**: 3-5 days
> **Impact**: 2-3x precision improvement on existing corpus sizes
> **Ships with**: Phase 2 (read-path overhaul release)

### 1.1 Replace Rank-Based FTS Scoring with BM25

**Problem**: `HybridSearch()` assigns `1.0 - (i * 0.05)` as FTS score — a positional proxy that ignores actual term relevance.

**Solution**: SQLite FTS5 natively supports BM25 ranking via `bm25()` function.

```sql
SELECT content, source_table, source_id, bm25(memory_fts) AS score
  FROM memory_fts
 WHERE memory_fts MATCH ?1
 ORDER BY score  -- bm25 returns negative values; lower = better
 LIMIT ?2
```

**Files**: `internal/db/hybrid_search.go`

### 1.2 Tune Hybrid Weight via Corpus Size

**Problem**: Fixed `HybridWeight` doesn't adapt. Small corpora benefit from more semantic weight; large corpora need more lexical anchor.

**Solution**: Make `HybridWeight` a function of `estimateCorpusSize()`:

| Corpus Size | Hybrid Weight (vector) | Rationale |
|-------------|----------------------|-----------|
| < 1,000 | 0.7 | Dense works well at small scale |
| 1,000 - 5,000 | 0.5 | Balanced |
| 5,000 - 10,000 | 0.4 | FTS starts winning |
| > 10,000 | 0.3 | Lexical anchor dominates |

**Files**: `internal/agent/memory/retrieval.go`, `internal/agent/memory/strategy.go`

### 1.3 Deduplicate Across Legs

**Problem**: The same document can appear in both FTS and vector results, inflating its score.

**Solution**: Merge by `(source_table, source_id)` key, taking the max score from each leg, then applying the weighted sum.

**Files**: `internal/db/hybrid_search.go`

### 1.4 Add Retrieval Quality Metrics

**Problem**: No observability into retrieval precision degradation.

**Solution**: Extend `RetrievalMetrics` with:
- `AvgFTSSimilarity` / `AvgVectorSimilarity` — track per-leg quality
- `CorpusSize` — log at query time
- `ScoreSpread` — distance between top-1 and top-k (collapse indicator: when spread → 0, collapse is happening)

**Files**: `internal/agent/memory/retrieval.go`

### Deliverables
- [ ] BM25 scoring in FTS leg
- [ ] Adaptive hybrid weight based on corpus size
- [ ] Deduplication across search legs
- [ ] Retrieval metrics with collapse detection signal
- [ ] Regression tests at 100, 1K, 10K synthetic corpus sizes

---

## Phase 2: Implement Actual ANN Indexing _(v1.0.5 — Part 2)_

> **Goal**: Build real sub-linear approximate nearest neighbor search.
> The current `hnsw.go` is exhaustive k-NN behind an aspirational name —
> this phase delivers what the name always promised.
> **Estimated effort**: 5-8 days
> **Impact**: Retrieval latency drops from O(n) to O(log n); enables scaling past 10K entries
> **Ships with**: Phase 1 (read-path overhaul release)

### 2.1 Rename and Preserve the Brute-Force Baseline

**First step**: Rename to eliminate the misnomer before building anything new.

| Current | Renamed |
|---------|---------|
| `HNSWIndex` | `BruteForceIndex` |
| `HNSWEntry` | `VectorEntry` |
| `HNSWSearchResult` | `VectorSearchResult` |
| `HNSWConfig` | `VectorIndexConfig` |
| `hnsw.go` | `vector_brute.go` |

The brute-force implementation stays as a fallback and test reference.
All callers (`hybrid_search.go`, `retrieval.go`) update to use the shared
`VectorSearchResult` type and a new `VectorIndex` interface:

```go
// VectorIndex abstracts over search implementations.
type VectorIndex interface {
    Search(query []float64, k int) []VectorSearchResult
    AddEntry(entry VectorEntry)
    IsBuilt() bool
    EntryCount() int
}
```

`BruteForceIndex` and the new ANN implementation both satisfy this
interface. Strategy selection chooses between them based on corpus size.

**Files**: `internal/db/hnsw.go` → `internal/db/vector_brute.go`, new `internal/db/vector_index.go` (interface)

### 2.2 Build Real HNSW (or Plug In sqlite-vec)

**Problem**: We have no sub-linear vector search. The current implementation
scans every entry on every query. At 768 dims × float64:

| Entries | Memory Scanned/Query | Est. Latency |
|---------|---------------------|-------------|
| 1,000 | ~6 MB | ~2ms |
| 10,000 | ~60 MB | ~20ms |
| 50,000 | ~300 MB | ~100ms+ |
| 100,000 | ~600 MB | ~200ms+ |

**Options** (in order of preference):

1. **`sqlite-vec` extension**: Vector search as a SQLite virtual table.
   Stays within the SQLite transaction boundary, no CGo complexity, actively
   maintained. Supports float32 and int8 quantization natively.

2. **Pure Go HNSW**: Implement the actual multi-layer navigable small world
   graph — layer probability assignment, greedy search with backtracking,
   neighbor selection heuristic. More control, no external dependency, but
   significant implementation effort (~800-1200 LOC).

3. **CGo binding to hnswlib**: Battle-tested C++ implementation via
   `github.com/nicholaskajoh/hnswlib-go` or similar. Proven at scale but
   adds CGo build complexity.

**Recommendation**: Start with Option 1 (`sqlite-vec`). Fall back to
Option 2 if platform availability is a concern. Option 3 only if we need
proven-at-million-scale performance.

### 2.3 Embedding Quantization

**Problem**: 768-dim float64 embeddings consume ~6KB per entry. At 50K entries = 300MB resident.

**Solution**: Store embeddings as float32 (halves memory), with optional
int8 quantization for the ANN index. ColBERTv2 research showed 6-10x
compression via residual quantization with minimal recall loss.

**Files**: `internal/db/vector_brute.go` (update types), `internal/db/schema.go` (migration)

### 2.4 Namespace-Partitioned Vector Indices

**Problem**: One global vector space → all tiers compete in the same semantic neighborhood.

**Solution**: Separate vector indices per memory tier (or tier group):
- **Hot index**: working + episodic (recent, frequently accessed)
- **Warm index**: semantic + procedural (stable knowledge)
- **Cold index**: relationship + archived

Each index stays well under the 10K collapse threshold. Query fans out to
relevant indices based on `SelectMode()`.

**Files**: New `internal/db/vector_ann.go`, modifications to `internal/agent/memory/retrieval.go`

### Deliverables
- [ ] Rename `HNSWIndex` → `BruteForceIndex`, extract `VectorIndex` interface
- [ ] Implement actual sub-linear ANN (sqlite-vec or pure Go HNSW)
- [ ] `BruteForceIndex` retained as fallback, satisfying same interface
- [ ] float32 storage, optional int8 quantization for index
- [ ] Tier-partitioned vector namespaces
- [ ] Benchmark suite: latency + precision at 1K/10K/50K/100K entries
- [ ] Strategy selector auto-promotes from BruteForce → ANN at threshold

---

## Phase 3: Cross-Encoder Reranking Stage _(v1.1.0 — Part 1)_

> **Goal**: Add a second-stage reranker that scores query-document pairs with
> full cross-attention, discarding noise that hybrid search lets through.
> **Estimated effort**: 5-7 days
> **Impact**: 30-50% precision improvement on top of hybrid search
> **Ships with**: Phase 4 (intelligence layer release)
> **Depends on**: Phase 2's sub-linear ANN (creates the latency budget this consumes)

### 3.1 Two-Stage Retrieval Pipeline

```
Query ──► Stage 1: Hybrid Search (fast, top-20)
              │
              ▼
          Stage 2: Cross-Encoder Rerank (slow, top-3)
              │
              ▼
          Context Assembly → LLM
```

**Stage 1** (existing): BM25 + vector hybrid, returns top-N candidates.
**Stage 2** (new): Cross-encoder model scores each (query, document) pair jointly.

### 3.2 Reranker Implementation Options

| Option | Latency | Quality | Dependency |
|--------|---------|---------|------------|
| Local cross-encoder (ONNX) | ~50ms/20 docs | High | ONNX runtime |
| LLM-as-reranker (small model) | ~200ms/20 docs | Very High | LLM router |
| Cohere Rerank API | ~100ms/20 docs | Very High | External API |
| MinHash similarity filter | ~1ms/20 docs | Medium | None (exists in `minhash.go`) |

**Recommendation**: Start with LLM-as-reranker via the existing LLM router — it can use the cheapest model in the fleet for reranking. No new dependencies. Later, add ONNX option for latency-sensitive paths.

### 3.3 Critical Design Principle: Discard, Don't Reorder

The reranker must **eliminate** low-relevance results, not just shuffle them. If Stage 1 returns 20 candidates and the reranker scores 15 of them below a threshold, only 5 go to the LLM. Less context = less noise = less semantic collapse in generation.

### 3.4 Confidence-Gated Assembly

Combine reranker score with existing decay/ranking to produce a final confidence:
```
final_score = reranker_score * decay_factor * tier_weight
```
Only entries above a dynamic threshold (based on score distribution) enter the context window.

### Deliverables
- [ ] Retriever interface split: `Retrieve()` → `Recall()` + `Rerank()` + `Assemble()`
- [ ] LLM-based reranker using cheapest available model
- [ ] Score threshold with dynamic cutoff (discard, don't reorder)
- [ ] Metrics: rerank latency, discard rate, precision@k before/after rerank
- [ ] Integration with existing `Ranker` decay scoring

---

## Phase 4: Knowledge Graph Persistence + Graph-Augmented Retrieval _(v1.1.0 — Part 2)_

> **Goal**: Persist the knowledge graph to SQLite; use graph traversal as a
> third retrieval channel alongside FTS and vector search.
> **Estimated effort**: 8-12 days
> **Impact**: Preserves relational structure that embeddings flatten; enables
> multi-hop reasoning
> **Ships with**: Phase 3 (intelligence layer release)
> **Note**: This phase changes the ingestion/write path. Shipping alongside
> Phase 3 (read-path refinement) is acceptable because the two are
> independent subsystems. Shipping it with Phases 1-2 would not be — it
> would mean changing read and write paths in the same release as the
> foundational index overhaul.

### 4.1 Persist Knowledge Graph to SQLite

**Problem**: `knowledge.go` stores facts in an in-memory map. Lost on restart. Can't be queried at scale.

**Schema**:
```sql
CREATE TABLE knowledge_facts (
    id         TEXT PRIMARY KEY,
    subject    TEXT NOT NULL,
    relation   TEXT NOT NULL,
    object     TEXT NOT NULL,
    source     TEXT,
    confidence REAL NOT NULL DEFAULT 0.8,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_facts_subject ON knowledge_facts(subject);
CREATE INDEX idx_facts_relation ON knowledge_facts(relation);
CREATE INDEX idx_facts_object ON knowledge_facts(object);
```

### 4.2 Dual-Channel Retrieval: Text + Graph

Add graph traversal as a retrieval channel:

```
Query ──► Entity Extraction (from query text)
              │
     ┌────────┴────────┐
     ▼                  ▼
  Text Channel       Graph Channel
  (BM25 + Vector)    (Entity → Subgraph)
     │                  │
     └────────┬────────┘
              ▼
          Context Merger
              ▼
          Cross-Encoder Rerank
```

**Graph channel**: Extract entities from query → look up as subjects/objects → traverse 1-2 hops → return connected facts with relationship context.

### 4.3 Entity Linking at Ingestion

During memory ingestion, extract entities and relationships to populate the knowledge graph automatically. This pairs with the existing consolidation pipeline.

### 4.4 Relationship-Aware Scoring

Graph results carry structural information (hop distance, relationship type) that vector results don't. Use this as a scoring signal:
- Direct match (0 hops): weight 1.0
- 1-hop neighbor: weight 0.7
- 2-hop neighbor: weight 0.4

### Deliverables
- [ ] SQLite-backed knowledge graph with subject/relation/object schema
- [ ] Migration from in-memory `KnowledgeGraph` to persistent store
- [ ] Graph traversal retrieval channel (1-2 hop BFS)
- [ ] Entity extraction at ingestion time
- [ ] Context merger: text + graph results unified for reranking
- [ ] Consolidation phase for graph maintenance (dedup, confidence decay)

---

## Phase 5: Agentic Retrieval + Query Intelligence _(v1.2.0)_

> **Goal**: The retrieval system reasons about what to retrieve, not just
> how to retrieve it. Query decomposition, adaptive routing, and iterative
> refinement close the final gap.
> **Estimated effort**: 10-15 days
> **Impact**: Handles complex multi-part queries; prevents semantic overload
> per retrieval call
> **Depends on**: Production data from v1.0.5 + v1.1.0 to tune
> decomposition heuristics, routing tables, and refinement thresholds.
> Shipping this without real score distributions from the reranker and
> per-tier precision metrics means tuning in the dark.

### 5.1 Query Decomposition

**Problem**: "What did we decide about the auth refactor and how does it affect the deployment timeline?" is a compound query that overloads a single embedding.

**Solution**: Decompose into sub-queries before retrieval:
1. "auth refactor decisions" → semantic + episodic tiers
2. "deployment timeline" → procedural + working memory tiers
3. Merge results, deduplicate, rerank holistically

Use the LLM (cheapest model) to decompose: single-sentence queries pass through unchanged; compound queries get split.

### 5.2 Adaptive Tier Routing

**Problem**: `SelectMode()` currently uses corpus size and session age, but ignores query intent.

**Solution**: Route sub-queries to the most relevant tier(s):

| Query Signal | Primary Tier | Secondary |
|-------------|-------------|-----------|
| "when did we..." | Episodic | Working |
| "how to..." | Procedural | Semantic |
| "who is..." | Relationship | Semantic |
| "what is the status..." | Working | Episodic |
| Factual/definitional | Semantic | Knowledge Graph |

Use the existing `intent.go` classifier to detect query type.

### 5.3 Iterative Refinement

If initial retrieval quality is below threshold (measured by reranker confidence):
1. Reformulate the query (broaden or narrow terms)
2. Try alternative tiers
3. Fall back to recency if semantic signals fail

Maximum 2 refinement rounds to bound latency.

### 5.4 Retrieval Budget Awareness

Each retrieval call consumes embedding API tokens and latency. The agentic layer must respect:
- Token budget (from existing `budgets` field in `Retriever`)
- Latency SLA (configurable per-channel priority)
- Diminishing returns detection (stop when reranker scores plateau)

### Deliverables
- [ ] Query decomposition via LLM (compound → sub-queries)
- [ ] Intent-based tier routing (extend `intent.go` + `strategy.go`)
- [ ] Iterative refinement with 2-round cap
- [ ] Budget-aware retrieval with early termination
- [ ] End-to-end integration tests: compound queries across 5 tiers

---

## Success Criteria

| Metric | Baseline (today) | v1.0.5 (Ph 1+2) | v1.1.0 (Ph 3+4) | v1.2.0 (Ph 5) |
|--------|------------------|------------------|------------------|----------------|
| Precision@3 (1K corpus) | ~70% | 87% | 93% | 95% |
| Precision@3 (10K corpus) | ~40% | 70% | 85% | 90% |
| Precision@3 (50K corpus) | ~15% | 50% | 75% | 85% |
| Retrieval latency (p99) | ~5ms | ~12ms | ~80ms | ~120ms |
| Memory overhead (50K entries) | ~300MB | ~150MB | ~170MB | ~170MB |
| Collapse detection signal | None | ScoreSpread metric | Reranker discard rate | Decomposition hit rate |

## Dependencies

### v1.0.5 (Phases 1 + 2)
- SQLite FTS5 `bm25()` function (already available)
- `sqlite-vec` extension OR pure Go HNSW implementation
- No new external API calls

### v1.1.0 (Phases 3 + 4)
- LLM router (already exists) for reranking
- Entity extraction (LLM-based or rule-based)
- **Requires**: v1.0.5 metrics baseline + ANN latency budget

### v1.2.0 (Phase 5)
- Intent classifier (exists in `intent.go`), query decomposition (LLM)
- **Requires**: production score distributions from v1.1.0 reranker

## Risk Register

| Risk | Release | Severity | Mitigation |
|------|---------|----------|------------|
| sqlite-vec not available on all platforms | v1.0.5 | Medium | Pure Go HNSW fallback; `BruteForceIndex` always available via `VectorIndex` interface |
| Phase 1 metrics show collapse isn't happening at *our* dev-scale corpus | v1.0.5 | Low | Does NOT justify deprioritizing v1.1.0. Our corpus is small — users' corpora could be orders of magnitude larger. The metrics validate our own system; they say nothing about user-scale safety. Ship all three releases regardless. |
| Reranker latency exceeds SLA when ANN is cold | v1.1.0 | Medium | Async reranking with timeout; fall back to Stage 1 results. ANN warmup on startup. |
| Entity extraction quality too low for graph channel | v1.1.0 | Medium | Start with rule-based extraction; upgrade to LLM later. Graph channel is additive — text channel still works if graph is sparse. |
| Query decomposition adds latency to simple queries | v1.2.0 | Low | Pass-through for single-intent queries; decompose only compound. Intent classifier gates the decomposition call. |
| Graph traversal returns too many nodes | v1.1.0 | Low | Hard cap at 2 hops + top-k per hop |
| v1.0.5 ships but v1.1.0 is delayed — system runs with ANN but no reranker | Any | Low | Acceptable intermediate state. ANN + hybrid search without reranking is still a major improvement over brute-force. |

---

## Research References

- [Stanford: RAG Systems Breaking at Scale](https://www.goml.io/blog/stanford-ai-research-rag-systems)
- [Semantic Collapse in RAG](https://aicompetence.org/semantic-collapse-in-rag/)
- [Hybrid Search: BM25 + SPLADE + Vector](https://blog.premai.io/hybrid-search-for-rag-bm25-splade-and-vector-search-combined/)
- [ColBERTv2: Lightweight Late Interaction](https://arxiv.org/abs/2112.01488)
- [Pinecone: Rerankers and Two-Stage Retrieval](https://www.pinecone.io/learn/series/rag/rerankers/)
- [Agentic RAG Survey](https://arxiv.org/abs/2501.09136)
- [A-RAG: Hierarchical Retrieval Interfaces](https://arxiv.org/html/2602.03442v1)
- [Embeddings + Knowledge Graphs for RAG](https://towardsdatascience.com/embeddings-knowledge-graphs-the-ultimate-tools-for-rag-systems-cbbcca29f0fd/)
- [Embedding Collapse in Recommender Systems](https://blog.reachsumit.com/posts/2024/11/embedding-collapse-recsys/)
- [Length-Induced Embedding Collapse](https://openreview.net/forum?id=jgISC1wdYy)
