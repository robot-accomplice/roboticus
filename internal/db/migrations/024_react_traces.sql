-- 016: React trace storage for flight recorder data.
-- Add session_id to pipeline_traces (existing table lacks it).
ALTER TABLE pipeline_traces ADD COLUMN session_id TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS react_traces (
    id TEXT PRIMARY KEY,
    pipeline_trace_id TEXT NOT NULL REFERENCES pipeline_traces(id),
    react_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_react_traces_pipeline ON react_traces(pipeline_trace_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_session ON pipeline_traces(session_id);
