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

// DeleteByName removes a sub-agent by name.
func (r *AgentsRepository) DeleteByName(ctx context.Context, name string) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM sub_agents WHERE name = ?`, name)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateModel updates a sub-agent's model (and optionally description).
// Returns ErrNoRowsAffected if no agent matched.
func (r *AgentsRepository) UpdateModel(ctx context.Context, name, model, description string) error {
	var res sql.Result
	var err error
	if description != "" {
		res, err = r.q.ExecContext(ctx,
			`UPDATE sub_agents SET model = ?, description = ? WHERE name = ?`, model, description, name)
	} else {
		res, err = r.q.ExecContext(ctx, `UPDATE sub_agents SET model = ? WHERE name = ?`, model, name)
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// ToggleEnabled flips a sub-agent's enabled flag.
// Returns ErrNoRowsAffected if no agent matched.
func (r *AgentsRepository) ToggleEnabled(ctx context.Context, name string) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE sub_agents SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// SetEnabledByNameOrID enables or disables a sub-agent by name or ID.
// Returns ErrNoRowsAffected if no agent matched.
func (r *AgentsRepository) SetEnabledByNameOrID(ctx context.Context, nameOrID string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	res, err := r.q.ExecContext(ctx,
		`UPDATE sub_agents SET enabled = ? WHERE id = ? OR name = ?`, val, nameOrID, nameOrID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// PruneOld deletes sub-agents older than the given number of days.
func (r *AgentsRepository) PruneOld(ctx context.Context, olderThanDays int) (int64, error) {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM sub_agents WHERE created_at < datetime('now', '-' || ? || ' days')`, olderThanDays)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Insert creates a new sub-agent with the given fields.
func (r *AgentsRepository) Insert(ctx context.Context, id, name, model, skillsJSON string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO sub_agents (id, name, model, skills_json, enabled) VALUES (?, ?, ?, ?, ?)`,
		id, name, model, skillsJSON, e)
	return err
}
