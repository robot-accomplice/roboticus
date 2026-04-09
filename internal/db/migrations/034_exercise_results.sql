-- Per-prompt exercise results persisted during baseline runs.
-- Enables resume-on-interrupt and --new-only mode.
CREATE TABLE IF NOT EXISTS exercise_results (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,          -- groups prompts from the same baseline run
    model TEXT NOT NULL,
    intent_class TEXT NOT NULL,
    complexity TEXT NOT NULL,
    prompt TEXT NOT NULL,
    content TEXT,                  -- model's response (nullable for failures)
    quality REAL NOT NULL DEFAULT 0.0,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    passed INTEGER NOT NULL DEFAULT 0,
    error_msg TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_exercise_results_model ON exercise_results(model);
CREATE INDEX IF NOT EXISTS idx_exercise_results_run ON exercise_results(run_id);
