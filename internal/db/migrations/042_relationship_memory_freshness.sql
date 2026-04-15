ALTER TABLE relationship_memory ADD COLUMN updated_at TEXT NOT NULL DEFAULT (datetime('now'));

CREATE TRIGGER IF NOT EXISTS relationship_au AFTER UPDATE ON relationship_memory BEGIN
    DELETE FROM memory_fts WHERE source_table = 'relationship_memory' AND source_id = old.id;
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (COALESCE(new.entity_name, '') || ': ' || COALESCE(new.interaction_summary, ''),
            'relationship', 'relationship_memory', new.id);
END;
