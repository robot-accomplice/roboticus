-- Milestone 3 follow-on: policy ingestion surface.
--
-- Until now, semantic_memory.is_canonical was inferred at RETRIEVAL time
-- from substring matches on category/key ("policy", "architecture",
-- "procedure", "canonical"). That is the "canonical by filename heuristic"
-- anti-pattern: authority should be an explicit asserted fact, not a
-- guess driven by how a row happens to be named.
--
-- This migration makes canonical status a persisted, caller-asserted
-- column and adds two pieces of provenance the ingestion tool needs:
--
--   * is_canonical — persisted boolean. Defaults to 0 (not canonical)
--     so pre-existing rows keep the semantics of "was not explicitly
--     asserted as canonical." The new ingestion path sets this to 1
--     only when the caller explicitly asserts it.
--
--   * source_label — the opaque source identifier the caller supplies
--     at ingest time (e.g. "policy/refund-v3", "docs/ops/runbook"). The
--     retrieval tier no longer synthesises this from category+key; it
--     reads the stored label and falls back to the synthesised form
--     only when the column is null.
--
--   * asserter_id — who (or what) claimed the row is canonical. This is
--     the provenance of the authority claim itself: if
--     is_canonical = 1 the ingest path requires asserter_id to be set,
--     so canonical status can never be asserted anonymously. When
--     is_canonical = 0 this column is typically null.
--
-- Indexes support the common retrieval predicates (canonical-first,
-- canonical-by-category) without full scans.

ALTER TABLE semantic_memory ADD COLUMN is_canonical INTEGER NOT NULL DEFAULT 0;
ALTER TABLE semantic_memory ADD COLUMN source_label TEXT;
ALTER TABLE semantic_memory ADD COLUMN asserter_id TEXT;

CREATE INDEX IF NOT EXISTS idx_semantic_canonical
  ON semantic_memory(is_canonical, memory_state, category);
