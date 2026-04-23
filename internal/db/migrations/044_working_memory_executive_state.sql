-- Expand working_memory into an executive-state store.
--
-- Background: working memory previously only tracked goals, notes, turn
-- summaries, decisions, observations, and facts. Milestone 7 of the agentic
-- memory roadmap requires structured executive state so multi-step tasks
-- preserve their plan, assumptions, unresolved questions, verified
-- conclusions, decision checkpoints, and stopping criteria across turns and
-- restarts.
--
-- Changes:
--   - Expand the entry_type CHECK to include executive-state types.
--   - Add task_id so entries can be grouped into the task they belong to.
--   - Add payload (JSON) so structured state (plan steps, decision options,
--     stopping thresholds) survives alongside the free-text content.
--
-- SQLite cannot alter a CHECK constraint in place, so this migration rebuilds
-- the table. Existing rows are preserved verbatim.

CREATE TABLE working_memory_new (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    entry_type TEXT NOT NULL CHECK(entry_type IN (
        'goal', 'note', 'turn_summary', 'decision', 'observation', 'fact',
        'plan', 'assumption', 'unresolved_question', 'verified_conclusion',
        'decision_checkpoint', 'stopping_criteria'
    )),
    content TEXT NOT NULL,
    importance INTEGER NOT NULL DEFAULT 5,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    persisted_at TEXT,
    task_id TEXT,
    payload TEXT
);

INSERT INTO working_memory_new (id, session_id, entry_type, content, importance, created_at, persisted_at)
SELECT id, session_id, entry_type, content, importance, created_at, persisted_at
FROM working_memory;

DROP TABLE working_memory;

ALTER TABLE working_memory_new RENAME TO working_memory;

CREATE INDEX IF NOT EXISTS idx_working_memory_session ON working_memory(session_id);
CREATE INDEX IF NOT EXISTS idx_working_memory_task ON working_memory(task_id);
CREATE INDEX IF NOT EXISTS idx_working_memory_entry_type ON working_memory(entry_type);
