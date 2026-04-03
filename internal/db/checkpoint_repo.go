package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CheckpointRepository handles context checkpoint persistence.
type CheckpointRepository struct {
	q Querier
}

// NewCheckpointRepository creates a checkpoint repository.
func NewCheckpointRepository(q Querier) *CheckpointRepository {
	return &CheckpointRepository{q: q}
}

// SaveCheckpoint inserts a new context checkpoint for a session.
// The data parameter is stored in the memory_summary column.
func (r *CheckpointRepository) SaveCheckpoint(ctx context.Context, sessionID, data string) error {
	id := fmt.Sprintf("ckpt-%d", time.Now().UnixNano())
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO context_checkpoints (id, session_id, system_prompt_hash, memory_summary)
		 VALUES (?, ?, '', ?)`,
		id, sessionID, data,
	)
	return err
}

// LoadCheckpoint returns the latest checkpoint data (memory_summary) for a session.
func (r *CheckpointRepository) LoadCheckpoint(ctx context.Context, sessionID string) (string, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT memory_summary FROM context_checkpoints
		 WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`,
		sessionID,
	)
	var data string
	err := row.Scan(&data)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return data, err
}

// DeleteOld deletes old checkpoints, keeping only the most recent keepCount per session.
func (r *CheckpointRepository) DeleteOld(ctx context.Context, keepCount int) (int64, error) {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM context_checkpoints WHERE id IN (
		   SELECT c1.id FROM context_checkpoints c1
		   WHERE (SELECT COUNT(*) FROM context_checkpoints c2
		          WHERE c2.session_id = c1.session_id AND c2.created_at >= c1.created_at) > ?
		 )`,
		keepCount,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
