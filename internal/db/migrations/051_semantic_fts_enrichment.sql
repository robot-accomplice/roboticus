-- v1.0.7: enrich semantic_memory's FTS corpus so semantic retrieval can
-- preserve key/category lookup without relying on a residual LIKE fallback.
--
-- Before this migration, semantic_memory indexed only `value` into memory_fts.
-- That forced the semantic tier to keep a heuristic SQL fallback for queries
-- that targeted semantic metadata such as the key (`deployment-window`) rather
-- than the value text itself.
--
-- This migration makes the semantic FTS corpus explicit and richer:
--   category + key + ": " + value
--
-- The read path can then stay on semantic-tier FTS and retire the semantic
-- LIKE branch without losing good lookup behavior.

DROP TRIGGER IF EXISTS semantic_memory_fts_ai;
DROP TRIGGER IF EXISTS semantic_memory_fts_au;

CREATE TRIGGER IF NOT EXISTS semantic_memory_fts_ai AFTER INSERT ON semantic_memory
BEGIN
  INSERT INTO memory_fts (content, source_table, source_id, category)
    VALUES (COALESCE(NEW.category, '') || ' ' || COALESCE(NEW.key, '') || ': ' || NEW.value,
            'semantic_memory', NEW.id, NEW.category);
END;

CREATE TRIGGER IF NOT EXISTS semantic_memory_fts_au
  AFTER UPDATE OF category, key, value ON semantic_memory
BEGIN
  DELETE FROM memory_fts
    WHERE source_table = 'semantic_memory' AND source_id = OLD.id;
  INSERT INTO memory_fts (content, source_table, source_id, category)
    VALUES (COALESCE(NEW.category, '') || ' ' || COALESCE(NEW.key, '') || ': ' || NEW.value,
            'semantic_memory', NEW.id, NEW.category);
END;

DELETE FROM memory_fts WHERE source_table = 'semantic_memory';

INSERT INTO memory_fts (content, source_table, source_id, category)
SELECT COALESCE(category, '') || ' ' || COALESCE(key, '') || ': ' || value,
       'semantic_memory', id, category
  FROM semantic_memory;
