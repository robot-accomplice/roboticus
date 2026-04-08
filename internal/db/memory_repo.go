package db

import (
	"context"
	"database/sql"
)

// MemoryRow is a unified row type for any memory tier.
type MemoryRow struct {
	ID         string
	SessionID  string
	Tier       string // "working", "episodic", "semantic", "procedural", "relationship"
	Content    string
	Category   string  // semantic
	Key        string  // semantic
	Value      string  // semantic
	EntryType  string  // working
	Importance float64 // episodic / working
	Confidence float64 // semantic
	CreatedAt  string
}

// MemoryRepository handles 5-tier memory persistence.
type MemoryRepository struct {
	q Querier
}

// NewMemoryRepository creates a memory repository.
func NewMemoryRepository(q Querier) *MemoryRepository {
	return &MemoryRepository{q: q}
}

// StoreWorking inserts a working-memory entry.
func (r *MemoryRepository) StoreWorking(ctx context.Context, id, sessionID, entryType, content string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content) VALUES (?, ?, ?, ?)`,
		id, sessionID, entryType, content)
	return err
}

// StoreEpisodic inserts an episodic-memory entry.
func (r *MemoryRepository) StoreEpisodic(ctx context.Context, id, classification, content string, importance float64) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance) VALUES (?, ?, ?, ?)`,
		id, classification, content, importance)
	return err
}

// StoreSemantic upserts a semantic-memory fact.
func (r *MemoryRepository) StoreSemantic(ctx context.Context, id, category, key, value string, confidence float64) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(category, key) DO UPDATE SET value = excluded.value, confidence = excluded.confidence,
		 memory_state = 'active', state_reason = NULL`,
		id, category, key, value, confidence)
	return err
}

// StoreProcedural inserts a procedural-memory entry.
func (r *MemoryRepository) StoreProcedural(ctx context.Context, id, name, steps string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO procedural_memory (id, name, steps) VALUES (?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET steps = excluded.steps, updated_at = datetime('now')`,
		id, name, steps)
	return err
}

// StoreRelationship upserts a relationship-memory entry.
func (r *MemoryRepository) StoreRelationship(ctx context.Context, id, entityID, entityName string, trustScore float64) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO relationship_memory (id, entity_id, entity_name, trust_score) VALUES (?, ?, ?, ?)
		 ON CONFLICT(entity_id) DO UPDATE SET entity_name = excluded.entity_name, trust_score = excluded.trust_score`,
		id, entityID, entityName, trustScore)
	return err
}

// QueryWorkingBySession returns working-memory entries for a session.
func (r *MemoryRepository) QueryWorkingBySession(ctx context.Context, sessionID string, limit int) ([]MemoryRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, session_id, entry_type, content, created_at FROM working_memory
		 WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []MemoryRow
	for rows.Next() {
		var m MemoryRow
		m.Tier = "working"
		if err := rows.Scan(&m.ID, &m.SessionID, &m.EntryType, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// QuerySemantic returns semantic facts filtered by category.
func (r *MemoryRepository) QuerySemantic(ctx context.Context, category string, limit int) ([]MemoryRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, category, key, value, confidence FROM semantic_memory
		 WHERE category = ? ORDER BY confidence DESC LIMIT ?`, category, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []MemoryRow
	for rows.Next() {
		var m MemoryRow
		m.Tier = "semantic"
		if err := rows.Scan(&m.ID, &m.Category, &m.Key, &m.Value, &m.Confidence); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// QueryEpisodic returns episodic memories ordered by importance.
func (r *MemoryRepository) QueryEpisodic(ctx context.Context, limit int) ([]MemoryRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, classification, content, importance, created_at FROM episodic_memory
		 ORDER BY importance DESC, created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []MemoryRow
	for rows.Next() {
		var m MemoryRow
		m.Tier = "episodic"
		if err := rows.Scan(&m.ID, &m.Category, &m.Content, &m.Importance, &m.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// DeleteWorking removes a working-memory entry by ID.
func (r *MemoryRepository) DeleteWorking(ctx context.Context, id string) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM working_memory WHERE id = ?`, id)
	return err
}

// GetSemantic looks up a single semantic fact by category+key.
func (r *MemoryRepository) GetSemantic(ctx context.Context, category, key string) (*MemoryRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, category, key, value, confidence FROM semantic_memory WHERE category = ? AND key = ?`,
		category, key)
	var m MemoryRow
	m.Tier = "semantic"
	err := row.Scan(&m.ID, &m.Category, &m.Key, &m.Value, &m.Confidence)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
