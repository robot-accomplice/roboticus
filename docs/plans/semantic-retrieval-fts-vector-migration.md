# Semantic Retrieval FTS+Vector Migration — Scoping Document

> Status: **Scoping (no code yet)**
> Predecessor: agentic-memory-architecture-roadmap.md, M3 follow-on
> Date: 2026-04-15

---

## Why this slice is bigger than "rip out LIKE"

The roadmap framed this as "migrate semantic retrieval off the residual
`LIKE` path onto hybrid FTS+vector." Investigation surfaced that the LIKE
fallback is masking a **correctness gap**, not just a performance gap:

- **`semantic_memory` has NO INSERT trigger to `memory_fts`.** Migration
  038 added an UPDATE trigger; the INSERT trigger does not exist anywhere
  in the schema or migrations. Fresh semantic rows are FTS-discoverable
  only if their `value` is later mutated.
- **`procedural_memory` and `relationship_memory` have INSERT and DELETE
  triggers but no UPDATE triggers.** When a workflow's `steps` is updated
  by `RecordWorkflow` (version bump), the FTS row goes stale.
- The LIKE blocks in `retrieveSemanticEvidence`, `retrieveProceduralEvidence`,
  `retrieveRelationshipEvidence`, and `Manager.FindWorkflows` are doing
  real work today *because of those gaps*, not just as graceful degradation.

If we pull LIKE without first closing the trigger gaps, a non-trivial
fraction of recently-ingested semantic / workflow rows become unreachable
through retrieval until the next consolidation pass touches them.

---

## Surface inventory

### LIKE call sites in the memory layer

| File | Line | Tier | Migration disposition |
|------|------|------|-----------------------|
| `retrieval_tiers.go` | 169 | semantic | **migrate** (primary M3 follow-on target) |
| `retrieval_tiers.go` | 267 | procedural (tool stats) | **migrate** |
| `retrieval_tiers.go` | 324 | learned_skills | **defer** — table has no FTS coverage; needs separate work |
| `retrieval_tiers.go` | 405 | relationship | **migrate** |
| `workflow.go` | 211 | procedural workflows | **migrate** |
| `executive.go` | 201 | working memory (executive entries) | **keep** — small per-session table, FTS overkill |
| `manager.go` | 378 | semantic prefix-delete | **keep** — intentional prefix semantics, not search |
| `consolidation_phases.go` | 696 | obsidian scanner | **keep** — domain-specific import filter |

### LIKE call sites in tools

| File | Disposition |
|------|-------------|
| `tools/memory_recall.go` (multiple) | Already FTS-first with documented LIKE fallback. After trigger gaps close, the LIKE blocks for FTS-covered tiers can be deleted; LIKE blocks for not-yet-covered tiers (learned_skills) stay. |
| `tools/workflow_search.go` | Calls `Manager.FindWorkflows`, so gets fixed transitively. |

### Existing FTS / vector infrastructure

- `memory_fts` virtual table (FTS5) keyed by `(content, category, source_table, source_id)`.
- INSERT triggers exist for: episodic_memory, procedural_memory, relationship_memory, knowledge_facts. **Missing: semantic_memory.**
- UPDATE triggers exist for: episodic_memory, semantic_memory. **Missing: procedural_memory, relationship_memory.**
- DELETE triggers exist for every covered tier.
- `db.HybridSearch(ctx, store, queryText, queryEmbedding, limit, hybridWeight, vectorIndex)` already implements FTS5 BM25 + vector cosine blend with deduplication. **No new search primitive needed.**
- `db.SanitizeFTSQuery` handles FTS5 special-character escaping.
- `Manager.embedAndStore` ingests embeddings on row write when an embedder is configured.
- Vector index is optional; HybridSearch degrades to FTS-only when the index is nil or unbuilt.

---

## Proposed slice ordering

Three slices, sequenced by risk. Each slice is independently shippable.

### Slice 1 — Trigger gap fixes (correctness)

**Goal:** make `memory_fts` actually consistent with the underlying tables
so HybridSearch can replace LIKE without losing rows.

**Work:**
- New migration `048_fts_trigger_completeness.sql`:
  - Add INSERT trigger for `semantic_memory` (currently missing entirely).
  - Add UPDATE triggers for `procedural_memory` and `relationship_memory`
    so version bumps and interaction-summary updates refresh the FTS row.
  - Backfill `memory_fts` for any pre-existing rows whose triggers were
    not in place when they were written.
- Idempotency: every `CREATE TRIGGER IF NOT EXISTS`; backfill uses
  `INSERT INTO memory_fts ... SELECT ... WHERE source_id NOT IN (SELECT
  source_id FROM memory_fts WHERE source_table = ...)`.

**Acceptance:**
- After running the migration on a fresh test DB seeded with rows in
  every tier, `SELECT COUNT(*) FROM memory_fts` for each tier matches
  `SELECT COUNT(*) FROM <tier>` minus deleted rows.
- INSERT / UPDATE / DELETE round-trips on every covered tier produce
  matching FTS state — a regression test enforces this for all four
  tiers in one place.

**Risk:** Low. Triggers are additive; backfill is idempotent.

### Slice 2 — Migrate retrieval LIKE to HybridSearch (primary)

**Goal:** every retrieval read path that today falls back to LIKE uses
HybridSearch primary, with LIKE retained as a graceful fallback only.

**Work:**
- `retrieveSemanticEvidence`: extend HybridSearch path to be the primary
  for ALL modes (currently scoped to RetrievalHybrid / Semantic / ANN).
  LIKE block stays as a "FTS returned nothing AND vector returned nothing"
  safety net — but the safety net should only fire when both legs are
  empty, not as a separate code path.
- `retrieveProceduralEvidence`: introduce a HybridSearch-by-tier wrapper
  that filters results to `source_table = 'procedural_memory'`. Today
  the LIKE block scans `procedural_memory` directly.
- `retrieveRelationshipEvidence`: same wrapper, filtered to
  `source_table = 'relationship_memory'`.
- `Manager.FindWorkflows`: take an embedder as input via Manager state
  (already present), use HybridSearch with category filter for
  `procedural_memory`, fall back to LIKE only when both legs empty.

**Acceptance:**
- An FTS-discoverable row is returned for a relevant query without ever
  touching the LIKE block (instrumentable via a counter or trace
  annotation).
- A row whose embedding is missing AND whose value contains a
  needle-in-haystack token only matchable via substring (not FTS5
  tokenization) is still returned via the LIKE safety net.
- Existing tests (`TestRetrieveSemanticEvidence_PreservesAuthorityMetadata`,
  `TestRetrieveProceduralMemory_PrefersWorkflowsOverToolStats`,
  `TestRetrieveProceduralMemory_FallsBackToToolStatsWhenNoWorkflow`,
  `FindWorkflows_QuerySensitiveMatch`) continue to pass — but with
  HybridSearch as the actual code path, validated by trace.

**Risk:** Medium. Subtle behavioral changes in result ordering and per-leg
score weights. Existing tests assert content-level outcomes, not exact
ordering, which limits the regression surface — but ordering changes can
affect downstream reranker scoring.

### Slice 3 — LIKE removal (cleanliness)

**Goal:** delete LIKE blocks for FTS-covered tiers once Slice 2 has
run in production long enough to validate FTS coverage is complete.

**Work:**
- Remove LIKE fallback from each migrated retrieval path.
- Remove LIKE blocks from `tools/memory_recall.go` for FTS-covered tiers.
- Keep LIKE only where it protects an explicit gap (e.g., learned_skills
  if its FTS coverage is still pending).

**Acceptance:**
- `grep -n "LIKE" internal/agent/memory/retrieval_tiers.go` returns only
  comments or out-of-scope helpers.
- Trace shows zero LIKE-fallback events over a representative workload.

**Risk:** Low if Slice 2 hardened the trigger coverage. Punt this slice
until we have real telemetry from Slice 2 showing the LIKE blocks have
gone quiet.

---

## Open decisions for sign-off

These are the choices I want explicit guidance on before any code:

### 1. LIKE retention semantics

Two equally defensible end states for Slice 2:

- **(a) FTS-primary, LIKE as safety net.** Hybrid runs first; LIKE only
  when both FTS and vector return zero. Preserves the "offline-safe
  fallback" pattern we used for the n-gram classifier and the canonical
  graph relations. Risk surface narrower.
- **(b) FTS-primary, LIKE removed.** Cleaner, but one missed trigger
  scenario (e.g., a future schema migration that forgets to update FTS
  triggers) silently loses retrieval coverage.

My recommendation: **(a)** — LIKE stays as a documented safety net that
trips only when both retrieval legs are empty. Slice 3 then becomes
optional ("delete LIKE if telemetry confirms it never fires") rather
than load-bearing.

### 2. Workflow steps tokenisation

`procedural_ai` indexes `new.name || ': ' || new.steps`, where `steps`
is a JSON array string like `["build", "push"]`. FTS5 will tokenise
the brackets and quotes as part of the content. That's serviceable but
not great.

Two options if we want this cleaner:
- **(i) Leave as-is.** FTS5's default tokenizer copes; query terms still
  match the steps even with the JSON syntax noise.
- **(ii) Normalise the indexed text.** Update the trigger to write
  `new.name || ': ' || replace(replace(replace(new.steps, '[', ''), ']', ''), '"', '')`
  or similar. Cleaner FTS rows, slightly more SQL.

Recommendation: **(i)** for this slice. Don't expand scope.

### 3. Trace surface for retrieval-path attribution

Worth adding a small `retrieval.path` annotation per call (`fts`,
`vector`, `hybrid`, `like_fallback`) so we can measure LIKE-fallback
frequency in production traces and use that to decide whether Slice 3
ships? My preference is yes — without it, we're guessing about whether
the safety net is needed.

### 4. Test seeding helper

Once Slice 2 lands, tests that today seed a row directly with
`SeedSemanticMemory` (which inserts via raw SQL and bypasses the
INSERT trigger that Slice 1 will add) need to either:
- Trust the new INSERT trigger to fire (preferred — tests run against
  the same trigger production sees), or
- Seed via `Manager.storeSemanticMemory` to exercise the full ingestion
  path including embedding (also fine, but slower for tests that don't
  need embedding behavior).

Recommendation: **trust the trigger**, update `SeedSemanticMemory` doc
comment to note the row will land in FTS automatically.

### 5. Out-of-scope but worth flagging

- `learned_skills` table has no FTS coverage. The LIKE block at
  `retrieval_tiers.go:324` cannot be migrated until that table gets
  FTS triggers. Tracking it as a separate, smaller slice rather than
  inflating this one.
- Vector index lifecycle: `vectorIndex.IsBuilt()` gates the vector
  leg. If embeddings exist but the index hasn't been built (cold
  start), HybridSearch silently skips vector. Worth confirming the
  daemon builds the index on startup before we lean on it harder.

---

## Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| Slice 1's backfill on a large existing DB takes seconds at startup | Keep backfill `INSERT ... WHERE NOT IN ...` so it only touches missing rows; subsequent restarts are no-ops |
| Slice 2 changes result ordering and breaks downstream reranker assumptions | Reranker tests already cover the canonical-first contract; add a fitness comparing "hybrid-only result list contains the same rows as hybrid + LIKE result list" for the test corpora |
| FTS5 tokenizer treats some operator-meaningful strings poorly (URLs, paths) | `SanitizeFTSQuery` already exists; verify it covers edge cases the new call sites can hit |
| Removing LIKE in Slice 3 breaks a tier whose FTS coverage we didn't fully audit | Don't ship Slice 3 until trace telemetry from Slice 2 shows the LIKE block has been quiet across realistic workloads |

---

## Recommendation

Ship Slice 1 first as its own commit — it's a correctness fix that's
independently valuable and unblocks the rest. Then ship Slice 2 with the
trace surface from open decision #3. Hold Slice 3 until we have
production telemetry.

Slice 1 is small (one migration, one regression test). Slice 2 is
moderate (four call sites, fitness test). Slice 3 is trivial when its
prerequisites are met.

Total estimate: Slice 1 same session, Slice 2 next session after
sign-off on the open decisions, Slice 3 deferred.
