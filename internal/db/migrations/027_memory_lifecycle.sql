-- Add memory lifecycle state tracking to episodic and semantic memory.
-- Enables staleness detection, consolidation, and memory pruning.
ALTER TABLE episodic_memory ADD COLUMN memory_state TEXT NOT NULL DEFAULT 'active';
ALTER TABLE episodic_memory ADD COLUMN state_reason TEXT;
ALTER TABLE semantic_memory ADD COLUMN memory_state TEXT NOT NULL DEFAULT 'active';
ALTER TABLE semantic_memory ADD COLUMN state_reason TEXT;
