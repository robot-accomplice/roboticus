CREATE TABLE IF NOT EXISTS baseline_runs (
    run_id TEXT PRIMARY KEY,
    initiator TEXT NOT NULL DEFAULT 'unknown',
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'completed', 'failed', 'canceled')),
    model_count INTEGER NOT NULL DEFAULT 0,
    models_json TEXT NOT NULL DEFAULT '[]',
    iterations INTEGER NOT NULL DEFAULT 1,
    config_fingerprint TEXT,
    git_revision TEXT,
    notes TEXT,
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_baseline_runs_started ON baseline_runs(started_at DESC);
