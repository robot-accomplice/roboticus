package db

import (
	"context"
	"database/sql"
)

// ToolCallRow represents a row in the tool_calls table.
type ToolCallRow struct {
	ID         string
	TurnID     string
	ToolName   string
	Input      string
	Output     string
	SkillID    string
	SkillName  string
	SkillHash  string
	Status     string
	DurationMs int64
	CreatedAt  string
}

// ToolsRepository handles tool-call execution tracking.
type ToolsRepository struct {
	q Querier
}

// NewToolsRepository creates a tools repository.
func NewToolsRepository(q Querier) *ToolsRepository {
	return &ToolsRepository{q: q}
}

// Record inserts a tool-call execution record.
func (r *ToolsRepository) Record(ctx context.Context, row ToolCallRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, output, skill_id, skill_name, skill_hash, status, duration_ms)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(id) DO UPDATE SET
             turn_id = excluded.turn_id,
             tool_name = excluded.tool_name,
             input = excluded.input,
             output = excluded.output,
             skill_id = excluded.skill_id,
             skill_name = excluded.skill_name,
             skill_hash = excluded.skill_hash,
             status = excluded.status,
             duration_ms = excluded.duration_ms`,
		row.ID, row.TurnID, row.ToolName, row.Input,
		nullIfEmpty(row.Output), nullIfEmpty(row.SkillID), nullIfEmpty(row.SkillName), nullIfEmpty(row.SkillHash),
		row.Status, row.DurationMs)
	return err
}

// GetByID retrieves a tool-call by ID. Returns nil if not found.
func (r *ToolsRepository) GetByID(ctx context.Context, id string) (*ToolCallRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, turn_id, tool_name, input, COALESCE(output,''), COALESCE(skill_id,''),
		        COALESCE(skill_name,''), COALESCE(skill_hash,''), status, COALESCE(duration_ms,0), created_at
		 FROM tool_calls WHERE id = ?`, id)
	var tc ToolCallRow
	err := row.Scan(&tc.ID, &tc.TurnID, &tc.ToolName, &tc.Input, &tc.Output,
		&tc.SkillID, &tc.SkillName, &tc.SkillHash, &tc.Status, &tc.DurationMs, &tc.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tc, nil
}

// ListByTurn returns all tool calls for a turn.
func (r *ToolsRepository) ListByTurn(ctx context.Context, turnID string) ([]ToolCallRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, turn_id, tool_name, input, COALESCE(output,''), COALESCE(skill_id,''),
		        COALESCE(skill_name,''), COALESCE(skill_hash,''), status, COALESCE(duration_ms,0), created_at
		 FROM tool_calls WHERE turn_id = ? ORDER BY created_at ASC`, turnID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []ToolCallRow
	for rows.Next() {
		var tc ToolCallRow
		if err := rows.Scan(&tc.ID, &tc.TurnID, &tc.ToolName, &tc.Input, &tc.Output,
			&tc.SkillID, &tc.SkillName, &tc.SkillHash, &tc.Status, &tc.DurationMs, &tc.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, tc)
	}
	return result, rows.Err()
}

// UpdateOutput sets the output and status for a tool call.
func (r *ToolsRepository) UpdateOutput(ctx context.Context, id, output, status string, durationMs int64) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE tool_calls SET output = ?, status = ?, duration_ms = ? WHERE id = ?`,
		output, status, durationMs, id)
	return err
}

// nullIfEmpty converts empty string to nil for nullable columns.
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
