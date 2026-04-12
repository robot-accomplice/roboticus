-- v1.0.2: Normalize memory_fts source_table to full names + add procedural/relationship triggers.
--
-- memory_fts previously used short names ('episodic', 'semantic', 'working')
-- while memory_index used full names ('episodic_memory', 'semantic_memory').
-- This mismatch required normalization on every JOIN.
--
-- This migration:
-- 1. Updates existing FTS rows to full names
-- 2. Drops old triggers (short names)
-- 3. Recreates triggers with full names
-- 4. Adds new triggers for procedural/relationship (previously missing)

-- Step 1a: Normalize existing short names to full names in memory_fts.
UPDATE memory_fts SET source_table = 'episodic_memory'     WHERE source_table = 'episodic';
UPDATE memory_fts SET source_table = 'semantic_memory'     WHERE source_table = 'semantic';
UPDATE memory_fts SET source_table = 'working_memory'      WHERE source_table = 'working';
UPDATE memory_fts SET source_table = 'procedural_memory'   WHERE source_table = 'procedural';
UPDATE memory_fts SET source_table = 'relationship_memory' WHERE source_table = 'relationship';

-- Step 1b: Normalize existing short names in memory_index too.
UPDATE memory_index SET source_table = 'episodic_memory'     WHERE source_table = 'episodic';
UPDATE memory_index SET source_table = 'semantic_memory'     WHERE source_table = 'semantic';
UPDATE memory_index SET source_table = 'working_memory'      WHERE source_table = 'working';
UPDATE memory_index SET source_table = 'procedural_memory'   WHERE source_table = 'procedural';
UPDATE memory_index SET source_table = 'relationship_memory' WHERE source_table = 'relationship';

-- Step 2: Drop old triggers (they use short names).
DROP TRIGGER IF EXISTS episodic_ai;
DROP TRIGGER IF EXISTS episodic_ad;

-- Step 3: Recreate episodic triggers with full names.
CREATE TRIGGER IF NOT EXISTS episodic_ai AFTER INSERT ON episodic_memory BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (new.content, new.classification, 'episodic_memory', new.id);
END;

CREATE TRIGGER IF NOT EXISTS episodic_ad AFTER DELETE ON episodic_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'episodic_memory' AND source_id = old.id;
END;

-- Step 4: Add procedural triggers (new in v1.0.2).
CREATE TRIGGER IF NOT EXISTS procedural_ai AFTER INSERT ON procedural_memory BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (new.name || ': ' || new.steps, 'procedural', 'procedural_memory', new.id);
END;

CREATE TRIGGER IF NOT EXISTS procedural_ad AFTER DELETE ON procedural_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'procedural_memory' AND source_id = old.id;
END;

-- Step 5: Add relationship triggers (new in v1.0.2).
CREATE TRIGGER IF NOT EXISTS relationship_ai AFTER INSERT ON relationship_memory BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (COALESCE(new.entity_name, '') || ': ' || COALESCE(new.interaction_summary, ''),
            'relationship', 'relationship_memory', new.id);
END;

CREATE TRIGGER IF NOT EXISTS relationship_ad AFTER DELETE ON relationship_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'relationship_memory' AND source_id = old.id;
END;
