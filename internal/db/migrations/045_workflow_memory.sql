-- Milestone 4: turn procedural memory into workflow memory.
--
-- Background: procedural_memory was originally a flat success/failure
-- counter per tool. The consolidation pipeline and retrieval layer have
-- both evolved to expect richer fields (confidence, memory_state,
-- structured success / failure evidence, category), but the schema lagged
-- behind — so consolidation's confidence sync silently skipped because
-- the column "may not exist".
--
-- This migration brings the schema in line with what the runtime already
-- consumes and adds the missing fields needed to represent real reusable
-- workflows:
--   - confidence: blended success signal, defaulted to 1.0 so existing
--     rows stay retrievable.
--   - memory_state / state_reason: standard lifecycle columns matching
--     episodic_memory / semantic_memory, so consolidation's active/stale
--     transitions apply uniformly.
--   - version: supports workflow revisions.
--   - category: discriminates workflow records (reusable SOPs) from bare
--     tool statistics so retrieval can prefer workflows.
--   - success_evidence / failure_evidence: JSON arrays of session/turn
--     IDs that confirmed the outcome, so operators can audit the
--     basis for the confidence score.
--   - last_used_at already exists from migration 040 — we only add an
--     index on it here to keep recency retrieval tight.

ALTER TABLE procedural_memory ADD COLUMN confidence REAL NOT NULL DEFAULT 1.0;
ALTER TABLE procedural_memory ADD COLUMN memory_state TEXT NOT NULL DEFAULT 'active';
ALTER TABLE procedural_memory ADD COLUMN state_reason TEXT;
ALTER TABLE procedural_memory ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE procedural_memory ADD COLUMN category TEXT NOT NULL DEFAULT 'tool';
ALTER TABLE procedural_memory ADD COLUMN success_evidence TEXT NOT NULL DEFAULT '[]';
ALTER TABLE procedural_memory ADD COLUMN failure_evidence TEXT NOT NULL DEFAULT '[]';

CREATE INDEX IF NOT EXISTS idx_procedural_category ON procedural_memory(category, memory_state);
CREATE INDEX IF NOT EXISTS idx_procedural_last_used ON procedural_memory(last_used_at DESC);
