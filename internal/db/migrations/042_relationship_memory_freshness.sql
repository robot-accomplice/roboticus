-- M3 follow-on: relationship_memory needs an `updated_at` column so the
-- freshness-aware retrieval / decay code can compute age the same way it
-- does for every other tier.
--
-- SQLite caveat (the bug this rewrite fixes): SQLite's ALTER TABLE ADD
-- COLUMN forbids non-constant default expressions — `datetime('now')`
-- counts as a function call, not a constant, and the original migration
-- fired `Cannot add a column with non-constant default (1)` on every
-- fresh install attempting to apply it. Existing installs whose
-- schema_version already records 42 are unaffected by this rewrite —
-- the migration runner skips them entirely (see
-- internal/db/schema_migrations.go: `if ver <= currentVersion {
-- continue }`). Editing this migration in place is safe: it cannot
-- re-run on databases that already applied it.
--
-- The SQLite-compliant pattern:
--   1. ADD COLUMN nullable (no default expression — SQLite forbids
--      function-call defaults on ADD COLUMN, and a constant default
--      like '' would force the application to special-case empty
--      strings, which is uglier than a NULL the read path already
--      handles).
--   2. UPDATE all existing rows to the current timestamp, so legacy
--      data has a real freshness signal.
--   3. Keep the `relationship_au` AFTER UPDATE trigger so memory_fts
--      stays in sync when the UPSERT path bumps `updated_at`.
--
-- We deliberately do NOT add an INSERT-time trigger to populate
-- updated_at on rows the application forgets to set. An earlier
-- iteration tried that and hit a SQLite trigger-ordering bug: when
-- two AFTER INSERT triggers exist on the same table (the FTS sync
-- trigger from migration 037 and a hypothetical updated_at-init
-- trigger here), SQLite does not guarantee execution order. The
-- init trigger's chained UPDATE could fire `relationship_au` before
-- the FTS sync trigger had run, leaving the FTS DELETE without a
-- row to delete and producing a duplicate FTS entry once the
-- original trigger fired afterwards.
--
-- The application path is the only writer to relationship_memory
-- in production, and its UPSERT branch (manager.go:418-431) already
-- sets `updated_at = datetime('now')` on every conflict-update.
-- Newly inserted rows that haven't been touched again stay NULL,
-- which is fine: the read path uses
-- `COALESCE(updated_at, created_at)` so freshness signals fall back
-- to creation time without observable drift.

ALTER TABLE relationship_memory ADD COLUMN updated_at TEXT;

UPDATE relationship_memory
   SET updated_at = COALESCE(updated_at, datetime('now'))
 WHERE updated_at IS NULL;

CREATE TRIGGER IF NOT EXISTS relationship_au AFTER UPDATE ON relationship_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'relationship_memory' AND source_id = old.id;
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (COALESCE(new.entity_name, '') || ': ' || COALESCE(new.interaction_summary, ''),
            'relationship', 'relationship_memory', new.id);
END;
