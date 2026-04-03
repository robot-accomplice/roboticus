-- Add memory_state to learned_skills for lifecycle management (pruning).
ALTER TABLE learned_skills ADD COLUMN memory_state TEXT NOT NULL DEFAULT 'active';

-- Add memory_index table for cross-tier memory lookup and the consolidation_log table.
CREATE TABLE IF NOT EXISTS memory_index (
    id TEXT PRIMARY KEY,
    source_table TEXT NOT NULL,
    source_id TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0.5,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(source_table, source_id)
);
CREATE INDEX IF NOT EXISTS idx_memory_index_source ON memory_index(source_table, source_id);

CREATE TABLE IF NOT EXISTS consolidation_log (
    id TEXT PRIMARY KEY,
    indexed INTEGER NOT NULL DEFAULT 0,
    deduped INTEGER NOT NULL DEFAULT 0,
    promoted INTEGER NOT NULL DEFAULT 0,
    confidence_decayed INTEGER NOT NULL DEFAULT 0,
    importance_decayed INTEGER NOT NULL DEFAULT 0,
    pruned INTEGER NOT NULL DEFAULT 0,
    orphaned INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
