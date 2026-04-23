-- M3.1: FTS trigger completeness.
--
-- The scoping pass for the M3 read-path migration found three real
-- correctness gaps in memory_fts coverage that the residual LIKE
-- fallback was masking:
--
--   1. semantic_memory has NO INSERT trigger to memory_fts. Migration
--      038 added an UPDATE trigger, but freshly-inserted semantic rows
--      are FTS-discoverable only if their value field is later mutated.
--
--   2. procedural_memory has an INSERT trigger but no UPDATE trigger,
--      so version bumps (RecordWorkflow at the same name) leave a
--      stale FTS row pointing at the prior steps text.
--
--   3. relationship_memory has the same gap as procedural_memory: an
--      INSERT trigger but no UPDATE trigger, so changes to
--      interaction_summary or entity_name go unindexed.
--
-- This migration closes all three gaps and backfills memory_fts for
-- rows ingested before the missing triggers existed. It is idempotent
-- under the migration runner and safe to re-run on already-current
-- data: the trigger creations use IF NOT EXISTS, and each backfill
-- INSERT filters by `source_id NOT IN (... already in memory_fts ...)`
-- so it only touches rows the trigger pipeline missed.
--
-- Trigger naming follows migration 038's convention
-- (`<table>_fts_ai` for AFTER INSERT, `<table>_fts_au` for AFTER UPDATE)
-- so future readers can find every trigger via a single grep without
-- chasing the older short-form naming used in 037.

-- ---------------------------------------------------------------------
-- Triggers
-- ---------------------------------------------------------------------

-- semantic_memory: missing INSERT trigger. Watches every insert and
-- writes the row's value into memory_fts under its category. The same
-- column choice migration 038's UPDATE trigger uses, so insert and
-- update produce symmetric FTS rows.
CREATE TRIGGER IF NOT EXISTS semantic_memory_fts_ai AFTER INSERT ON semantic_memory
BEGIN
  INSERT INTO memory_fts (content, source_table, source_id, category)
    VALUES (NEW.value, 'semantic_memory', NEW.id, NEW.category);
END;

-- semantic_memory: missing DELETE trigger. The spec's enumerated work
-- list named only the missing INSERT trigger, but the acceptance
-- criteria explicitly require synchronization across "insert, update,
-- AND delete" for every covered tier. Without this trigger, deleting a
-- semantic_memory row leaves an orphaned memory_fts entry that
-- HybridSearch would still surface — exactly the silently-stale-row
-- failure mode the rest of this migration is closing. Added under
-- M3.1's acceptance criteria, surfaced when the round-trip regression
-- test would otherwise have failed at the delete step.
CREATE TRIGGER IF NOT EXISTS semantic_memory_fts_ad AFTER DELETE ON semantic_memory
BEGIN
  DELETE FROM memory_fts
    WHERE source_table = 'semantic_memory' AND source_id = OLD.id;
END;

-- procedural_memory: missing UPDATE trigger. Watches name and steps —
-- the same fields the existing INSERT trigger (procedural_ai) indexes.
-- Re-indexes using the new values so a workflow that bumps version or
-- revises its step list refreshes its FTS row.
CREATE TRIGGER IF NOT EXISTS procedural_memory_fts_au
  AFTER UPDATE OF name, steps ON procedural_memory
BEGIN
  DELETE FROM memory_fts
    WHERE source_table = 'procedural_memory' AND source_id = OLD.id;
  INSERT INTO memory_fts (content, category, source_table, source_id)
    VALUES (NEW.name || ': ' || NEW.steps, 'procedural', 'procedural_memory', NEW.id);
END;

-- relationship_memory: missing UPDATE trigger. Watches entity_name and
-- interaction_summary, the two fields the existing INSERT trigger
-- (relationship_ai) indexes. COALESCE protects against either being NULL
-- so the FTS content stays well-formed.
CREATE TRIGGER IF NOT EXISTS relationship_memory_fts_au
  AFTER UPDATE OF entity_name, interaction_summary ON relationship_memory
BEGIN
  DELETE FROM memory_fts
    WHERE source_table = 'relationship_memory' AND source_id = OLD.id;
  INSERT INTO memory_fts (content, category, source_table, source_id)
    VALUES (COALESCE(NEW.entity_name, '') || ': ' || COALESCE(NEW.interaction_summary, ''),
            'relationship', 'relationship_memory', NEW.id);
END;

-- ---------------------------------------------------------------------
-- Backfill
-- ---------------------------------------------------------------------
-- For each tier, insert into memory_fts any base-table row whose id is
-- not already present in memory_fts under that source_table. This
-- catches rows ingested before the trigger pipeline was complete. The
-- NOT IN subselect makes the backfill no-op on already-current data,
-- so the migration is safe to re-run.

INSERT INTO memory_fts (content, source_table, source_id, category)
SELECT value, 'semantic_memory', id, category
  FROM semantic_memory
 WHERE id NOT IN (
   SELECT source_id FROM memory_fts WHERE source_table = 'semantic_memory'
 );

INSERT INTO memory_fts (content, category, source_table, source_id)
SELECT name || ': ' || steps, 'procedural', 'procedural_memory', id
  FROM procedural_memory
 WHERE id NOT IN (
   SELECT source_id FROM memory_fts WHERE source_table = 'procedural_memory'
 );

INSERT INTO memory_fts (content, category, source_table, source_id)
SELECT COALESCE(entity_name, '') || ': ' || COALESCE(interaction_summary, ''),
       'relationship', 'relationship_memory', id
  FROM relationship_memory
 WHERE id NOT IN (
   SELECT source_id FROM memory_fts WHERE source_table = 'relationship_memory'
 );
