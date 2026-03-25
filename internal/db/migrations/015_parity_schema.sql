-- Phase 1A: Parity schema additions (pipeline traces, heartbeat results, delegation outcomes, context snapshot columns)

CREATE TABLE IF NOT EXISTS pipeline_traces (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL REFERENCES turns(id),
    channel TEXT NOT NULL DEFAULT 'api',
    total_ms INTEGER NOT NULL DEFAULT 0,
    stages_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_turn ON pipeline_traces(turn_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_created ON pipeline_traces(created_at);

CREATE TABLE IF NOT EXISTS heartbeat_task_results (
    id TEXT PRIMARY KEY,
    task_name TEXT NOT NULL,
    success INTEGER NOT NULL DEFAULT 1,
    message TEXT,
    metrics_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_heartbeat_results_task ON heartbeat_task_results(task_name, created_at);

CREATE TABLE IF NOT EXISTS delegation_outcomes (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL REFERENCES turns(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    task_description TEXT NOT NULL,
    subtask_count INTEGER NOT NULL DEFAULT 0,
    pattern TEXT NOT NULL DEFAULT 'none',
    assigned_agents_json TEXT NOT NULL DEFAULT '[]',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    success INTEGER NOT NULL DEFAULT 0,
    quality_score REAL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_delegation_turn ON delegation_outcomes(turn_id);
CREATE INDEX IF NOT EXISTS idx_delegation_session ON delegation_outcomes(session_id);

-- Additional columns on context_snapshots for retrieval analytics.
ALTER TABLE context_snapshots ADD COLUMN retrieval_hit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE context_snapshots ADD COLUMN retrieval_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE context_snapshots ADD COLUMN avg_similarity REAL NOT NULL DEFAULT 0.0;
ALTER TABLE context_snapshots ADD COLUMN budget_utilization REAL NOT NULL DEFAULT 0.0;
