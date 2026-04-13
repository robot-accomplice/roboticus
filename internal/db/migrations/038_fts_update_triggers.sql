-- Migration 038: Add UPDATE triggers for FTS5 consistency.
--
-- Fixes HIGH-4: FTS index goes stale on UPDATE because only INSERT/DELETE
-- triggers existed. When episodic content or semantic values are updated,
-- the FTS index now reflects the new content.

CREATE TRIGGER IF NOT EXISTS episodic_memory_fts_au AFTER UPDATE OF content ON episodic_memory
BEGIN
  DELETE FROM memory_fts WHERE source_table = 'episodic_memory' AND source_id = OLD.id;
  INSERT INTO memory_fts (content, source_table, source_id, category)
    VALUES (NEW.content, 'episodic_memory', NEW.id, NEW.classification);
END;

CREATE TRIGGER IF NOT EXISTS semantic_memory_fts_au AFTER UPDATE OF value ON semantic_memory
BEGIN
  DELETE FROM memory_fts WHERE source_table = 'semantic_memory' AND source_id = OLD.id;
  INSERT INTO memory_fts (content, source_table, source_id, category)
    VALUES (NEW.value, 'semantic_memory', NEW.id, NEW.category);
END;
