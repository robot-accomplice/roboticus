-- Enrich procedural_memory beyond success/failure counters.
-- Supports workflows, playbooks, and execution context per the
-- agentic retrieval reference architecture (Layer 5).

ALTER TABLE procedural_memory ADD COLUMN preconditions TEXT DEFAULT '';
ALTER TABLE procedural_memory ADD COLUMN error_modes TEXT DEFAULT '';
ALTER TABLE procedural_memory ADD COLUMN last_used_at TEXT;
ALTER TABLE procedural_memory ADD COLUMN avg_duration_ms INTEGER DEFAULT 0;
ALTER TABLE procedural_memory ADD COLUMN context_tags TEXT DEFAULT '[]';
