-- Milestone 3: upgrade semantic memory into a canonical knowledge layer.
--
-- Today semantic_memory carries memory_state / state_reason and the
-- consolidation pipeline flips entries to 'stale' when a newer value arrives,
-- but the schema cannot express *which* entry replaced a stale one, when a
-- fact became authoritative, or which revision of a key is current. This
-- migration adds the three missing columns the canonical knowledge layer
-- needs and an index that lets retrieval prefer the most recent
-- authoritative version without a full scan.
--
-- Changes:
--   - version: monotonically-increasing revision counter. Bumped by the
--     manager's upsert path when a key's value changes.
--   - effective_date: optional ISO-8601 timestamp marking when the fact
--     became authoritative. Distinct from created_at because some facts
--     have a natural effective-from date (policy updates, release dates)
--     that differs from when the row was inserted.
--   - superseded_by: when memory_state = 'stale', points at the
--     semantic_memory.id that replaced this row so retrieval and audit
--     tooling can follow the chain.

ALTER TABLE semantic_memory ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE semantic_memory ADD COLUMN effective_date TEXT;
ALTER TABLE semantic_memory ADD COLUMN superseded_by TEXT;

CREATE INDEX IF NOT EXISTS idx_semantic_state_effective
  ON semantic_memory(memory_state, effective_date DESC);
CREATE INDEX IF NOT EXISTS idx_semantic_superseded_by
  ON semantic_memory(superseded_by);
