-- Tool description embedding cache.
-- Keyed by (tool_name, description_hash) so re-embedding only happens on description change.

CREATE TABLE IF NOT EXISTS tool_embeddings (
    tool_name TEXT NOT NULL,
    description_hash TEXT NOT NULL,
    embedding BLOB NOT NULL,
    dimensions INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (tool_name, description_hash)
);
