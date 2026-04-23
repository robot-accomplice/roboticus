CREATE TABLE IF NOT EXISTS knowledge_facts (
    id TEXT PRIMARY KEY,
    subject TEXT NOT NULL,
    relation TEXT NOT NULL,
    object TEXT NOT NULL,
    source_table TEXT,
    source_id TEXT,
    confidence REAL NOT NULL DEFAULT 0.7,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_knowledge_facts_subject ON knowledge_facts(subject);
CREATE INDEX IF NOT EXISTS idx_knowledge_facts_relation ON knowledge_facts(relation);
CREATE INDEX IF NOT EXISTS idx_knowledge_facts_object ON knowledge_facts(object);
CREATE INDEX IF NOT EXISTS idx_knowledge_facts_source ON knowledge_facts(source_table, source_id);

CREATE TRIGGER IF NOT EXISTS knowledge_facts_ai AFTER INSERT ON knowledge_facts BEGIN
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (new.subject || ' ' || new.relation || ' ' || new.object,
            'graph', 'knowledge_facts', new.id);
END;

CREATE TRIGGER IF NOT EXISTS knowledge_facts_au AFTER UPDATE ON knowledge_facts BEGIN
    DELETE FROM memory_fts WHERE source_table = 'knowledge_facts' AND source_id = old.id;
    INSERT INTO memory_fts(content, category, source_table, source_id)
    VALUES (new.subject || ' ' || new.relation || ' ' || new.object,
            'graph', 'knowledge_facts', new.id);
END;

CREATE TRIGGER IF NOT EXISTS knowledge_facts_ad AFTER DELETE ON knowledge_facts BEGIN
    DELETE FROM memory_fts WHERE source_table = 'knowledge_facts' AND source_id = old.id;
END;
