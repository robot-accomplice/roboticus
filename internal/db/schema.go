// Package db provides the SQLite database layer.
//
// Schema management follows a two-phase model:
//   1. Baseline DDL (schemaDDL constant below): CREATE TABLE IF NOT EXISTS statements
//      that establish the initial schema for fresh installs. This is "version 0".
//   2. Numbered migrations (internal/db/migrations/*.sql): ALTER TABLE, CREATE INDEX,
//      and other incremental changes applied on top of the baseline.
//
// Rules for contributors:
//   - NEVER modify the baseline DDL to change existing tables (add/drop columns, etc.)
//   - To add a NEW table: add it to schemaDDL AND create a migration for existing installs
//   - To modify an existing table: create a numbered migration only
//   - Migration filenames: NNN_description.sql (e.g., 040_add_foo_index.sql)
//   - Migrations are embedded via go:embed and applied in filename order
//   - ensureOptionalColumns handles backward-compatible column additions
package db

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// embeddedSchemaVersion matches the Rust EMBEDDED_SCHEMA_VERSION.
// The base schema incorporates all migrations through this version.
const embeddedSchemaVersion = 30

// schemaDDL is the full initial schema (ported from schema.rs SCHEMA_SQL).
const schemaDDL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    scope_key TEXT NOT NULL DEFAULT 'agent',
    status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'archived', 'expired')),
    model TEXT,
    nickname TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    metadata TEXT,
    cross_channel_consent INTEGER NOT NULL DEFAULT 0,
    non_interactive INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_sessions_scope ON sessions(agent_id, scope_key, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_active_scope_unique ON sessions(agent_id, scope_key) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_sessions_status_updated ON sessions(status, updated_at);

CREATE TABLE IF NOT EXISTS session_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    parent_id TEXT,
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system', 'tool')),
    content TEXT NOT NULL,
    usage_json TEXT,
    topic_tag TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_session_messages_session ON session_messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS turns (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    thinking TEXT,
    tool_calls_json TEXT,
    tokens_in INTEGER,
    tokens_out INTEGER,
    cost REAL,
    model TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id, created_at);

CREATE TABLE IF NOT EXISTS tool_calls (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL REFERENCES turns(id),
    tool_name TEXT NOT NULL,
    input TEXT NOT NULL,
    output TEXT,
    skill_id TEXT,
    skill_name TEXT,
    skill_hash TEXT,
    status TEXT NOT NULL,
    duration_ms INTEGER,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_tool_calls_turn ON tool_calls(turn_id);

CREATE TABLE IF NOT EXISTS policy_decisions (
    id TEXT PRIMARY KEY,
    turn_id TEXT,
    tool_name TEXT NOT NULL,
    decision TEXT NOT NULL CHECK(decision IN ('allow', 'deny')),
    rule_name TEXT,
    reason TEXT,
    context_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_policy_decisions_session ON policy_decisions(turn_id);
CREATE INDEX IF NOT EXISTS idx_policy_decisions_created ON policy_decisions(created_at);

CREATE TABLE IF NOT EXISTS working_memory (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    entry_type TEXT NOT NULL CHECK(entry_type IN ('goal', 'note', 'turn_summary', 'decision', 'observation', 'fact')),
    content TEXT NOT NULL,
    importance INTEGER NOT NULL DEFAULT 5,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_working_memory_session ON working_memory(session_id);

CREATE TABLE IF NOT EXISTS episodic_memory (
    id TEXT PRIMARY KEY,
    classification TEXT NOT NULL,
    content TEXT NOT NULL,
    importance INTEGER NOT NULL DEFAULT 5,
    owner_id TEXT,
    memory_state TEXT NOT NULL DEFAULT 'active',
    state_reason TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_episodic_importance ON episodic_memory(importance DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS semantic_memory (
    id TEXT PRIMARY KEY,
    category TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 0.8,
    memory_state TEXT NOT NULL DEFAULT 'active',
    state_reason TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(category, key)
);

CREATE TABLE IF NOT EXISTS procedural_memory (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    steps TEXT NOT NULL,
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS relationship_memory (
    id TEXT PRIMARY KEY,
    entity_id TEXT NOT NULL UNIQUE,
    entity_name TEXT,
    trust_score REAL NOT NULL DEFAULT 0.5,
    interaction_summary TEXT,
    interaction_count INTEGER NOT NULL DEFAULT 1,
    last_interaction TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    content,
    category,
    source_table,
    source_id
);

-- Episodic FTS triggers (uses full table names for consistency with memory_index).
CREATE TRIGGER IF NOT EXISTS episodic_ai AFTER INSERT ON episodic_memory BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (new.content, new.classification, 'episodic_memory', new.id);
END;

CREATE TRIGGER IF NOT EXISTS episodic_ad AFTER DELETE ON episodic_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'episodic_memory' AND source_id = old.id;
END;

-- Procedural FTS triggers (v1.0.2: was missing — only LIKE fallback before).
CREATE TRIGGER IF NOT EXISTS procedural_ai AFTER INSERT ON procedural_memory BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (new.name || ': ' || new.steps, 'procedural', 'procedural_memory', new.id);
END;

CREATE TRIGGER IF NOT EXISTS procedural_ad AFTER DELETE ON procedural_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'procedural_memory' AND source_id = old.id;
END;

-- Relationship FTS triggers (v1.0.2: was missing — only LIKE fallback before).
CREATE TRIGGER IF NOT EXISTS relationship_ai AFTER INSERT ON relationship_memory BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (COALESCE(new.entity_name, '') || ': ' || COALESCE(new.interaction_summary, ''),
            'relationship', 'relationship_memory', new.id);
END;

CREATE TRIGGER IF NOT EXISTS relationship_ad AFTER DELETE ON relationship_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'relationship_memory' AND source_id = old.id;
END;

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    source TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS cron_jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    schedule_kind TEXT NOT NULL,
    schedule_expr TEXT,
    schedule_every_ms INTEGER,
    schedule_tz TEXT DEFAULT 'UTC',
    agent_id TEXT NOT NULL,
    session_target TEXT NOT NULL DEFAULT 'main',
    payload_json TEXT NOT NULL,
    delivery_mode TEXT DEFAULT 'none',
    delivery_channel TEXT,
    last_run_at TEXT,
    last_status TEXT,
    last_duration_ms INTEGER,
    consecutive_errors INTEGER NOT NULL DEFAULT 0,
    next_run_at TEXT,
    last_error TEXT,
    lease_holder TEXT,
    lease_expires_at TEXT
);

CREATE TABLE IF NOT EXISTS cron_runs (
    id TEXT PRIMARY KEY DEFAULT (hex(randomblob(16))),
    job_id TEXT NOT NULL REFERENCES cron_jobs(id),
    status TEXT NOT NULL,
    duration_ms INTEGER DEFAULT 0,
    error_msg TEXT DEFAULT '',
    timestamp TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_cron_runs_job ON cron_runs(job_id, timestamp);

CREATE TABLE IF NOT EXISTS transactions (
    id TEXT PRIMARY KEY,
    tx_type TEXT NOT NULL,
    amount REAL NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USD',
    counterparty TEXT,
    tx_hash TEXT,
    metadata_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS service_requests (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL,
    requester TEXT NOT NULL,
    parameters_json TEXT NOT NULL,
    status TEXT NOT NULL,
    quoted_amount REAL NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USDC',
    recipient TEXT NOT NULL,
    quote_expires_at TEXT NOT NULL,
    payment_tx_hash TEXT,
    paid_amount REAL,
    payment_verified_at TEXT,
    fulfillment_output TEXT,
    fulfilled_at TEXT,
    failure_reason TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_service_requests_status ON service_requests(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_service_requests_service ON service_requests(service_id, created_at DESC);

CREATE TABLE IF NOT EXISTS revenue_opportunities (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    strategy TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    expected_revenue_usdc REAL NOT NULL,
    status TEXT NOT NULL,
    qualification_reason TEXT,
    confidence_score REAL NOT NULL DEFAULT 0,
    effort_score REAL NOT NULL DEFAULT 0,
    risk_score REAL NOT NULL DEFAULT 0,
    priority_score REAL NOT NULL DEFAULT 0,
    recommended_approved INTEGER NOT NULL DEFAULT 0,
    score_reason TEXT,
    plan_json TEXT,
    evidence_json TEXT,
    request_id TEXT,
    settlement_ref TEXT UNIQUE,
    settled_amount_usdc REAL,
    attributable_costs_usdc REAL NOT NULL DEFAULT 0,
    net_profit_usdc REAL,
    tax_rate REAL NOT NULL DEFAULT 0,
    tax_amount_usdc REAL NOT NULL DEFAULT 0,
    retained_earnings_usdc REAL,
    tax_destination_wallet TEXT,
    settled_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_revenue_opportunities_status ON revenue_opportunities(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_revenue_opportunities_strategy ON revenue_opportunities(strategy, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_revenue_opportunities_request ON revenue_opportunities(request_id);

CREATE TABLE IF NOT EXISTS revenue_feedback (
    id TEXT PRIMARY KEY,
    opportunity_id TEXT NOT NULL,
    strategy TEXT NOT NULL,
    grade REAL NOT NULL,
    source TEXT NOT NULL,
    comment TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_revenue_feedback_opportunity ON revenue_feedback(opportunity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_revenue_feedback_strategy ON revenue_feedback(strategy, created_at DESC);

CREATE TABLE IF NOT EXISTS inference_costs (
    id TEXT PRIMARY KEY,
    model TEXT NOT NULL,
    provider TEXT NOT NULL,
    tokens_in INTEGER NOT NULL,
    tokens_out INTEGER NOT NULL,
    cost REAL NOT NULL,
    tier TEXT,
    cached INTEGER NOT NULL DEFAULT 0,
    latency_ms INTEGER,
    quality_score REAL,
    escalation INTEGER NOT NULL DEFAULT 0,
    turn_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_inference_costs_time ON inference_costs(created_at DESC);

CREATE TABLE IF NOT EXISTS semantic_cache (
    id TEXT PRIMARY KEY,
    prompt_hash TEXT NOT NULL,
    embedding BLOB,
    response TEXT NOT NULL,
    model TEXT NOT NULL,
    tokens_saved INTEGER NOT NULL DEFAULT 0,
    hit_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_cache_hash ON semantic_cache(prompt_hash);

CREATE TABLE IF NOT EXISTS identity (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS os_personality_history (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS metric_snapshots (
    id TEXT PRIMARY KEY,
    metrics_json TEXT NOT NULL,
    alerts_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS discovered_agents (
    id TEXT PRIMARY KEY,
    did TEXT NOT NULL UNIQUE,
    agent_card_json TEXT NOT NULL,
    capabilities TEXT,
    endpoint_url TEXT NOT NULL,
    chain_id INTEGER NOT NULL DEFAULT 8453,
    trust_score REAL NOT NULL DEFAULT 0.5,
    last_verified_at TEXT,
    expires_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_discovered_agents_did ON discovered_agents(did);

CREATE TABLE IF NOT EXISTS paired_devices (
    id TEXT PRIMARY KEY,
    public_key_hex TEXT NOT NULL,
    device_name TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending', 'verified')),
    paired_at TEXT NOT NULL DEFAULT (datetime('now')),
    verified_at TEXT,
    last_seen TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_paired_devices_state ON paired_devices(state, paired_at DESC);

CREATE TABLE IF NOT EXISTS skills (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL CHECK(kind IN ('structured', 'instruction', 'scripted', 'builtin')),
    description TEXT,
    source_path TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    triggers_json TEXT,
    tool_chain_json TEXT,
    policy_overrides_json TEXT,
    script_path TEXT,
    risk_level TEXT NOT NULL DEFAULT 'Caution' CHECK(risk_level IN ('Safe', 'Caution', 'Dangerous', 'Forbidden')),
    enabled INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    last_used_at TEXT,
    last_loaded_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    version TEXT NOT NULL DEFAULT '0.0.0',
    author TEXT NOT NULL DEFAULT 'local',
    registry_source TEXT NOT NULL DEFAULT 'local'
);
CREATE INDEX IF NOT EXISTS idx_skills_kind ON skills(kind);

CREATE TABLE IF NOT EXISTS delivery_queue (
    id TEXT PRIMARY KEY,
    channel TEXT NOT NULL,
    recipient_id TEXT NOT NULL,
    content TEXT NOT NULL,
    idempotency_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'in_flight', 'delivered', 'dead_letter')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    next_retry_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_delivery_queue_status ON delivery_queue(status, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_delivery_queue_idem ON delivery_queue(idempotency_key);

CREATE TABLE IF NOT EXISTS approval_requests (
    id TEXT PRIMARY KEY,
    tool_name TEXT NOT NULL,
    tool_input TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'approved', 'denied', 'timed_out')),
    decided_by TEXT,
    decided_at TEXT,
    timeout_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_approvals_status ON approval_requests(status);

CREATE TABLE IF NOT EXISTS plugins (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    version TEXT NOT NULL,
    description TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    manifest_path TEXT NOT NULL,
    permissions_json TEXT,
    installed_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS embeddings (
    id TEXT PRIMARY KEY,
    source_table TEXT NOT NULL,
    source_id TEXT NOT NULL,
    content_preview TEXT NOT NULL,
    embedding_blob BLOB,
    dimensions INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_embeddings_source ON embeddings(source_table, source_id);

CREATE TABLE IF NOT EXISTS tool_embeddings (
    tool_name TEXT NOT NULL,
    description_hash TEXT NOT NULL,
    embedding BLOB NOT NULL,
    dimensions INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (tool_name, description_hash)
);

CREATE TABLE IF NOT EXISTS sub_agents (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL UNIQUE,
    display_name TEXT,
    model TEXT NOT NULL DEFAULT '',
    fallback_models_json TEXT NOT NULL DEFAULT '[]',
    role TEXT NOT NULL DEFAULT 'specialist',
    description TEXT,
    skills_json TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    session_count INTEGER NOT NULL DEFAULT 0,
    last_used_at TEXT,
    status TEXT NOT NULL DEFAULT 'registered',
    error_message TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS context_checkpoints (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    system_prompt_hash TEXT NOT NULL,
    memory_summary TEXT NOT NULL,
    active_tasks TEXT,
    conversation_digest TEXT,
    turn_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session ON context_checkpoints(session_id, created_at DESC);

CREATE TABLE IF NOT EXISTS hippocampus (
    table_name TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    columns_json TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT 'system',
    agent_owned INTEGER NOT NULL DEFAULT 0,
    access_level TEXT NOT NULL DEFAULT 'internal',
    row_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_hippocampus_agent ON hippocampus(created_by, agent_owned);

CREATE TABLE IF NOT EXISTS turn_feedback (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL UNIQUE REFERENCES turns(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    grade INTEGER NOT NULL CHECK (grade BETWEEN 1 AND 5),
    source TEXT NOT NULL DEFAULT 'dashboard',
    comment TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_turn_feedback_session ON turn_feedback(session_id);

CREATE TABLE IF NOT EXISTS context_snapshots (
    turn_id TEXT PRIMARY KEY REFERENCES turns(id),
    complexity_level TEXT NOT NULL CHECK(complexity_level IN ('L0', 'L1', 'L2', 'L3')),
    token_budget INTEGER NOT NULL,
    system_prompt_tokens INTEGER,
    memory_tokens INTEGER,
    history_tokens INTEGER,
    history_depth INTEGER,
    memory_tiers_json TEXT,
    retrieved_memories_json TEXT,
    model TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS model_selection_events (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    channel TEXT NOT NULL,
    selected_model TEXT NOT NULL,
    strategy TEXT NOT NULL,
    primary_model TEXT NOT NULL,
    override_model TEXT,
    complexity TEXT,
    user_excerpt TEXT NOT NULL,
    candidates_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    schema_version INTEGER NOT NULL DEFAULT 1,
    attribution TEXT,
    metascore_json TEXT,
    features_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_model_selection_events_turn ON model_selection_events(turn_id);
CREATE INDEX IF NOT EXISTS idx_model_selection_events_created ON model_selection_events(created_at DESC);

CREATE TABLE IF NOT EXISTS shadow_routing_predictions (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL,
    production_model TEXT NOT NULL,
    shadow_model TEXT,
    production_complexity REAL,
    shadow_complexity REAL,
    agreed INTEGER NOT NULL DEFAULT 0,
    detail_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_shadow_routing_created ON shadow_routing_predictions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shadow_routing_turn ON shadow_routing_predictions(turn_id);

CREATE TABLE IF NOT EXISTS abuse_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    origin TEXT NOT NULL,
    channel TEXT NOT NULL,
    signal_type TEXT NOT NULL,
    severity TEXT NOT NULL CHECK(severity IN ('low', 'medium', 'high')),
    action_taken TEXT NOT NULL,
    detail TEXT,
    score REAL NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_abuse_events_actor ON abuse_events(actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_abuse_events_origin ON abuse_events(origin, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_abuse_events_created ON abuse_events(created_at DESC);

CREATE TABLE IF NOT EXISTS learned_skills (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL UNIQUE,
    description       TEXT NOT NULL DEFAULT '',
    trigger_tools     TEXT NOT NULL DEFAULT '[]',
    steps_json        TEXT NOT NULL DEFAULT '[]',
    source_session_id TEXT,
    success_count     INTEGER NOT NULL DEFAULT 1,
    failure_count     INTEGER NOT NULL DEFAULT 0,
    priority          INTEGER NOT NULL DEFAULT 50,
    skill_md_path     TEXT,
    memory_state      TEXT NOT NULL DEFAULT 'active',
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_learned_skills_priority ON learned_skills(priority DESC);

CREATE TABLE IF NOT EXISTS memory_index (
    id TEXT PRIMARY KEY,
    source_table TEXT NOT NULL,
    source_id TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    category TEXT DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0.8,
    last_verified TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
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

CREATE TABLE IF NOT EXISTS hygiene_log (
    id                             TEXT PRIMARY KEY,
    sweep_at                       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    stale_procedural_days          INTEGER NOT NULL,
    dead_skill_priority_threshold  INTEGER NOT NULL,
    proc_total                     INTEGER NOT NULL DEFAULT 0,
    proc_stale                     INTEGER NOT NULL DEFAULT 0,
    proc_pruned                    INTEGER NOT NULL DEFAULT 0,
    skills_total                   INTEGER NOT NULL DEFAULT 0,
    skills_dead                    INTEGER NOT NULL DEFAULT 0,
    skills_pruned                  INTEGER NOT NULL DEFAULT 0,
    avg_skill_priority             REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX IF NOT EXISTS idx_hygiene_log_sweep ON hygiene_log(sweep_at DESC);

CREATE TABLE IF NOT EXISTS pipeline_traces (
    id TEXT PRIMARY KEY,
    turn_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT 'api',
    total_ms INTEGER NOT NULL DEFAULT 0,
    stages_json TEXT NOT NULL DEFAULT '[]',
    react_trace_json TEXT,
    inference_params_json TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_turn ON pipeline_traces(turn_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_created ON pipeline_traces(created_at);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_session ON pipeline_traces(session_id);

CREATE TABLE IF NOT EXISTS react_traces (
    id TEXT PRIMARY KEY,
    pipeline_trace_id TEXT NOT NULL REFERENCES pipeline_traces(id),
    react_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_react_traces_pipeline ON react_traces(pipeline_trace_id);

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

CREATE TABLE IF NOT EXISTS agent_tasks (
    id TEXT PRIMARY KEY,
    phase TEXT NOT NULL DEFAULT 'pending',
    parent_id TEXT,
    goal TEXT NOT NULL DEFAULT '',
    current_step INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_agent_tasks_phase ON agent_tasks(phase);
CREATE INDEX IF NOT EXISTS idx_agent_tasks_parent ON agent_tasks(parent_id);

CREATE TABLE IF NOT EXISTS task_steps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES agent_tasks(id),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    output TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_task_steps_task ON task_steps(task_id);

CREATE TABLE IF NOT EXISTS task_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    parent_task_id TEXT,
    assigned_to TEXT,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id);
CREATE INDEX IF NOT EXISTS idx_task_events_parent ON task_events(parent_task_id);
CREATE INDEX IF NOT EXISTS idx_task_events_assigned_to ON task_events(assigned_to);
CREATE INDEX IF NOT EXISTS idx_task_events_created ON task_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_events_type ON task_events(event_type);

CREATE TABLE IF NOT EXISTS agent_delegation_outcomes (
    id TEXT PRIMARY KEY,
    parent_task_id TEXT,
    subagent_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    result_summary TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_agent_delegation_parent ON agent_delegation_outcomes(parent_task_id);

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
    origin_channel TEXT NOT NULL DEFAULT '',
    origin_recipient TEXT NOT NULL DEFAULT '',
    requesting_channel TEXT NOT NULL DEFAULT '',
    requesting_recipient TEXT NOT NULL DEFAULT '',
    consent_type TEXT NOT NULL DEFAULT 'cross_channel',
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'fulfilled', 'granted', 'denied')),
    granted_by TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT,
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

CREATE TABLE IF NOT EXISTS runtime_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS wallet_balances (
    symbol     TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    balance    REAL NOT NULL DEFAULT 0.0,
    contract   TEXT NOT NULL DEFAULT '',
    decimals   INTEGER NOT NULL DEFAULT 18,
    is_native  INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_enabled ON cron_jobs(enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_transactions_created ON transactions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inference_costs_turn ON inference_costs(turn_id);
CREATE INDEX IF NOT EXISTS idx_memory_index_confidence ON memory_index(confidence DESC);
`

// optionalColumn describes a column that may be missing from older databases.
type optionalColumn struct {
	Table   string
	Column  string
	ColType string
	Default string
}

// optionalNullableColumn describes a nullable column that may be missing from older databases.
type optionalNullableColumn struct {
	Table   string
	Column  string
	ColType string
}

// ensureOptionalColumns checks for columns that may be missing from older installs
// and adds them if needed. Uses PRAGMA table_info to detect column existence.
func (s *Store) ensureOptionalColumns() error {
	// Columns with NOT NULL DEFAULT constraints.
	columns := []optionalColumn{
		{Table: "episodic_memory", Column: "memory_state", ColType: "TEXT", Default: "'active'"},
		{Table: "semantic_memory", Column: "memory_state", ColType: "TEXT", Default: "'active'"},
		{Table: "pipeline_traces", Column: "session_id", ColType: "TEXT", Default: "''"},
		{Table: "sessions", Column: "non_interactive", ColType: "INTEGER", Default: "0"},
		{Table: "skills", Column: "usage_count", ColType: "INTEGER", Default: "0"},
		{Table: "hippocampus", Column: "access_level", ColType: "TEXT", Default: "'internal'"},
		{Table: "hippocampus", Column: "row_count", ColType: "INTEGER", Default: "0"},
		{Table: "cron_jobs", Column: "retry_count", ColType: "INTEGER", Default: "0"},
		{Table: "cron_jobs", Column: "max_retries", ColType: "INTEGER", Default: "3"},
		{Table: "cron_jobs", Column: "retry_delay_ms", ColType: "INTEGER", Default: "60000"},
		// installed_themes columns for older DBs that created the table without them.
		{Table: "installed_themes", Column: "name", ColType: "TEXT", Default: "''"},
		{Table: "installed_themes", Column: "source", ColType: "TEXT", Default: "'catalog'"},
		{Table: "installed_themes", Column: "version", ColType: "TEXT", Default: "'1.0.0'"},
		{Table: "installed_themes", Column: "active", ColType: "INTEGER", Default: "0"},
		{Table: "installed_themes", Column: "content", ColType: "TEXT", Default: "'{}'"},
	}

	for _, col := range columns {
		exists, err := s.columnExists(col.Table, col.Column)
		if err != nil {
			return core.WrapError(core.ErrDatabase, fmt.Sprintf("failed to check column %s.%s", col.Table, col.Column), err)
		}
		if exists {
			continue
		}
		alter := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s NOT NULL DEFAULT %s",
			col.Table, col.Column, col.ColType, col.Default)
		if _, err := s.db.Exec(alter); err != nil {
			return core.WrapError(core.ErrDatabase, fmt.Sprintf("failed to add column %s.%s", col.Table, col.Column), err)
		}
		log.Info().Str("table", col.Table).Str("column", col.Column).Msg("added optional column")
	}

	// Nullable columns (no NOT NULL constraint).
	nullableColumns := []optionalNullableColumn{
		{Table: "session_messages", Column: "topic_tag", ColType: "TEXT"},
		{Table: "episodic_memory", Column: "owner_id", ColType: "TEXT"},
		{Table: "skills", Column: "last_used_at", ColType: "TEXT"},
		{Table: "approval_requests", Column: "turn_id", ColType: "TEXT"},
		{Table: "sub_agents", Column: "last_used_at", ColType: "TEXT"},
		{Table: "pipeline_traces", Column: "react_trace_json", ColType: "TEXT"},
		{Table: "pipeline_traces", Column: "inference_params_json", ColType: "TEXT"},
		{Table: "memory_index", Column: "last_verified", ColType: "TEXT"},
	}

	for _, col := range nullableColumns {
		exists, err := s.columnExists(col.Table, col.Column)
		if err != nil {
			return core.WrapError(core.ErrDatabase, fmt.Sprintf("failed to check column %s.%s", col.Table, col.Column), err)
		}
		if exists {
			continue
		}
		alter := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
			col.Table, col.Column, col.ColType)
		if _, err := s.db.Exec(alter); err != nil {
			return core.WrapError(core.ErrDatabase, fmt.Sprintf("failed to add column %s.%s", col.Table, col.Column), err)
		}
		log.Info().Str("table", col.Table).Str("column", col.Column).Msg("added optional column")
	}

	return nil
}

// columnExists returns true if the given column exists on the table.
func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// initSchema creates the base schema and seeds the version if this is a fresh database.
func (s *Store) initSchema() error {
	_, err := s.db.Exec(schemaDDL)
	if err != nil {
		return core.WrapError(core.ErrDatabase, "schema init failed", err)
	}

	var count int
	err = s.db.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count)
	if err != nil {
		return core.WrapError(core.ErrDatabase, "failed to check schema_version", err)
	}

	if count == 0 {
		_, err = s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", embeddedSchemaVersion)
		if err != nil {
			return core.WrapError(core.ErrDatabase, "failed to seed schema_version", err)
		}
		log.Info().Int("version", embeddedSchemaVersion).Msg("schema initialized")
	}

	return nil
}
