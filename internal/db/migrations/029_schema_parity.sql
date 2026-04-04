-- Migration 029: Full schema parity with roboticus v27.
-- Adds missing columns, tables, indexes, and fixes default divergences.

-- === Missing columns ===

-- sessions.non_interactive (roboticus ensure_optional_columns v0.11.3)
ALTER TABLE sessions ADD COLUMN non_interactive INTEGER NOT NULL DEFAULT 0;

-- session_messages.topic_tag (roboticus ensure_optional_columns v0.12.0)
ALTER TABLE session_messages ADD COLUMN topic_tag TEXT;

-- episodic_memory.owner_id (roboticus ensure_optional_columns)
ALTER TABLE episodic_memory ADD COLUMN owner_id TEXT;

-- skills.usage_count + last_used_at (roboticus ensure_optional_columns)
ALTER TABLE skills ADD COLUMN usage_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE skills ADD COLUMN last_used_at TEXT;

-- approval_requests.turn_id (roboticus migration 32)
ALTER TABLE approval_requests ADD COLUMN turn_id TEXT;

-- sub_agents.last_used_at (roboticus ensure_optional_columns)
ALTER TABLE sub_agents ADD COLUMN last_used_at TEXT;

-- hippocampus.access_level + row_count (roboticus ensure_optional_columns v0.9.2)
ALTER TABLE hippocampus ADD COLUMN access_level TEXT NOT NULL DEFAULT 'internal';
ALTER TABLE hippocampus ADD COLUMN row_count INTEGER NOT NULL DEFAULT 0;

-- pipeline_traces.react_trace_json + inference_params_json (roboticus ensure_optional_columns)
ALTER TABLE pipeline_traces ADD COLUMN react_trace_json TEXT;
ALTER TABLE pipeline_traces ADD COLUMN inference_params_json TEXT;

-- memory_index.last_verified (roboticus dynamic create)
ALTER TABLE memory_index ADD COLUMN last_verified TEXT;

-- === Missing tables ===

CREATE TABLE IF NOT EXISTS treasury_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    usdc_balance REAL NOT NULL DEFAULT 0.0,
    native_balance REAL NOT NULL DEFAULT 0.0,
    atoken_balance REAL NOT NULL DEFAULT 0.0,
    survival_tier TEXT NOT NULL DEFAULT 'Normal',
    last_deposit_at TEXT,
    last_withdrawal_at TEXT,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS session_model_performance (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    model TEXT NOT NULL,
    turn_count INTEGER NOT NULL DEFAULT 0,
    avg_tokens_out REAL NOT NULL DEFAULT 0,
    avg_latency_ms REAL NOT NULL DEFAULT 0,
    avg_quality REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_session_model_perf_session ON session_model_performance(session_id);

CREATE TABLE IF NOT EXISTS consent_requests (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    consent_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'granted', 'denied')),
    granted_by TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    resolved_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_consent_requests_session ON consent_requests(session_id, status);

CREATE TABLE IF NOT EXISTS installed_themes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    source TEXT NOT NULL DEFAULT 'catalog',
    version TEXT NOT NULL DEFAULT '1.0.0',
    active INTEGER NOT NULL DEFAULT 0,
    content TEXT NOT NULL,
    installed_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- === Missing indexes ===

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_enabled ON cron_jobs(enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_transactions_created ON transactions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inference_costs_turn ON inference_costs(turn_id);
CREATE INDEX IF NOT EXISTS idx_memory_index_confidence ON memory_index(confidence DESC);

-- === Fix default divergence: relationship_memory.interaction_count ===
-- Roboticus DEFAULT 1, goboticus was DEFAULT 0. Backfill existing 0 values.
UPDATE relationship_memory SET interaction_count = 1 WHERE interaction_count = 0;
