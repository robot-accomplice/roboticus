-- Track when working memory entries were persisted across process restarts.
-- Entries with persisted_at != NULL survived a shutdown cycle.
-- Startup vet uses this + importance + entry_type to decide what to retain.

ALTER TABLE working_memory ADD COLUMN persisted_at TEXT;
