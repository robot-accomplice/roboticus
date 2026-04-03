-- Remove the incorrect REFERENCES turns(id) FK from pipeline_traces.turn_id.
-- The column stores message IDs (from session_messages), not turn IDs.
-- SQLite cannot alter FK constraints, so we recreate the table.

CREATE TABLE IF NOT EXISTS pipeline_traces_new (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT 'api',
    total_ms INTEGER NOT NULL DEFAULT 0,
    stages_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO pipeline_traces_new
    SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at
    FROM pipeline_traces;

DROP TABLE IF EXISTS pipeline_traces;
ALTER TABLE pipeline_traces_new RENAME TO pipeline_traces;

CREATE INDEX IF NOT EXISTS idx_pipeline_traces_turn ON pipeline_traces(turn_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_created ON pipeline_traces(created_at);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_session ON pipeline_traces(session_id);
