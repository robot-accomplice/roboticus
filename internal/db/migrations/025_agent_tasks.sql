-- 025: Agent instances, tasks, steps, and delegation outcomes.
ALTER TABLE sub_agents ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sub_agents ADD COLUMN status TEXT NOT NULL DEFAULT 'registered';
ALTER TABLE sub_agents ADD COLUMN error_message TEXT NOT NULL DEFAULT '';
ALTER TABLE sub_agents ADD COLUMN started_at TEXT;
ALTER TABLE sub_agents ADD COLUMN updated_at TEXT NOT NULL DEFAULT (datetime('now'));

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
