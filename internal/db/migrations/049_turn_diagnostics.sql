CREATE TABLE IF NOT EXISTS turn_diagnostics (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT 'api',
    status TEXT NOT NULL DEFAULT 'ok',
    final_model TEXT,
    final_provider TEXT,
    total_ms INTEGER NOT NULL DEFAULT 0,
    inference_attempts INTEGER NOT NULL DEFAULT 0,
    fallback_count INTEGER NOT NULL DEFAULT 0,
    tool_call_count INTEGER NOT NULL DEFAULT 0,
    guard_retry_count INTEGER NOT NULL DEFAULT 0,
    verifier_retry_count INTEGER NOT NULL DEFAULT 0,
    request_messages INTEGER NOT NULL DEFAULT 0,
    request_tools INTEGER NOT NULL DEFAULT 0,
    request_approx_tokens INTEGER NOT NULL DEFAULT 0,
    context_pressure TEXT,
    resource_pressure TEXT,
    primary_diagnosis TEXT,
    diagnosis_confidence REAL NOT NULL DEFAULT 0,
    user_narrative TEXT,
    operator_narrative TEXT,
    recommendations_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_turn_diagnostics_turn ON turn_diagnostics(turn_id);
CREATE INDEX IF NOT EXISTS idx_turn_diagnostics_session ON turn_diagnostics(session_id);
CREATE INDEX IF NOT EXISTS idx_turn_diagnostics_created ON turn_diagnostics(created_at);

CREATE TABLE IF NOT EXISTS turn_diagnostic_events (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    at_ms INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    parent_event_id TEXT,
    status TEXT NOT NULL DEFAULT 'ok',
    operator_summary TEXT,
    user_summary TEXT,
    details_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_turn_diagnostic_events_turn_seq ON turn_diagnostic_events(turn_id, seq);
