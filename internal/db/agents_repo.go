package db

import (
	"context"
	"database/sql"
	"time"
)

// AgentInstanceRow represents a stored agent instance.
type AgentInstanceRow struct {
	ID        string
	AgentID   string
	Name      string
	Status    string
	Error     string
	StartedAt *time.Time
	UpdatedAt time.Time
	CreatedAt string
}

// AgentsRepository handles agent instance persistence.
type AgentsRepository struct {
	q Querier
}

func NewAgentsRepository(q Querier) *AgentsRepository {
	return &AgentsRepository{q: q}
}

func (r *AgentsRepository) Save(ctx context.Context, row AgentInstanceRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO sub_agents (id, agent_id, name, status, error_message, started_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.AgentID, row.Name, row.Status, row.Error,
		row.StartedAt, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (r *AgentsRepository) List(ctx context.Context) ([]AgentInstanceRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, agent_id, name, status, error_message, started_at, updated_at FROM sub_agents ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []AgentInstanceRow
	for rows.Next() {
		var row AgentInstanceRow
		var startedAt sql.NullString
		var updatedAt string
		if err := rows.Scan(&row.ID, &row.AgentID, &row.Name, &row.Status, &row.Error, &startedAt, &updatedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t, _ := time.Parse(time.RFC3339, startedAt.String)
			row.StartedAt = &t
		}
		row.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		result = append(result, row)
	}
	return result, rows.Err()
}

func (r *AgentsRepository) UpdateStatus(ctx context.Context, id, status, errorMsg string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE sub_agents SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		status, errorMsg, time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

func (r *AgentsRepository) Delete(ctx context.Context, id string) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM sub_agents WHERE id = ?`, id)
	return err
}
