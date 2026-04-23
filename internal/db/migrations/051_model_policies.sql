CREATE TABLE IF NOT EXISTS model_policies (
    model TEXT PRIMARY KEY,
    state TEXT NOT NULL CHECK(state IN ('enabled', 'niche', 'disabled', 'benchmark_only')),
    primary_reason_code TEXT,
    reason_codes_json TEXT NOT NULL DEFAULT '[]',
    human_reason TEXT,
    evidence_refs_json TEXT NOT NULL DEFAULT '[]',
    source TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_model_policies_state ON model_policies(state);
