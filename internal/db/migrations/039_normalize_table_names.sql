-- Migration 039: Normalize all legacy short table names to full names.
--
-- Closes LOW-1: After migration 037 normalized new FTS entries, this migration
-- normalizes existing rows in embeddings and memory_index tables so that
-- the IN ('short', 'full') guards in consolidation can be removed.

UPDATE embeddings SET source_table = 'episodic_memory' WHERE source_table = 'episodic';
UPDATE embeddings SET source_table = 'semantic_memory' WHERE source_table = 'semantic';
UPDATE embeddings SET source_table = 'procedural_memory' WHERE source_table = 'procedural';
UPDATE embeddings SET source_table = 'relationship_memory' WHERE source_table = 'relationship';
UPDATE embeddings SET source_table = 'working_memory' WHERE source_table = 'working';

UPDATE memory_index SET source_table = 'episodic_memory' WHERE source_table = 'episodic';
UPDATE memory_index SET source_table = 'semantic_memory' WHERE source_table = 'semantic';
UPDATE memory_index SET source_table = 'procedural_memory' WHERE source_table = 'procedural';
UPDATE memory_index SET source_table = 'relationship_memory' WHERE source_table = 'relationship';
UPDATE memory_index SET source_table = 'working_memory' WHERE source_table = 'working';
