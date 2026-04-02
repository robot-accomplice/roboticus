package db

import (
	"context"
	"database/sql"
)

// SkillRow represents a row in the skills table.
type SkillRow struct {
	ID                  string
	Name                string
	Kind                string // "structured", "instruction", "scripted", "builtin"
	Description         string
	SourcePath          string
	ContentHash         string
	TriggersJSON        string
	ToolChainJSON       string
	PolicyOverridesJSON string
	ScriptPath          string
	RiskLevel           string
	Enabled             bool
	LastLoadedAt        string
	CreatedAt           string
	Version             string
	Author              string
	RegistrySource      string
}

// SkillsRepository handles skill persistence.
type SkillsRepository struct {
	q Querier
}

// NewSkillsRepository creates a skills repository.
func NewSkillsRepository(q Querier) *SkillsRepository {
	return &SkillsRepository{q: q}
}

// Upsert inserts or replaces a skill record.
func (r *SkillsRepository) Upsert(ctx context.Context, row SkillRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO skills
		 (id, name, kind, description, source_path, content_hash, triggers_json, tool_chain_json,
		  policy_overrides_json, script_path, risk_level, enabled, last_loaded_at, version, author, registry_source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   kind = excluded.kind,
		   description = excluded.description,
		   source_path = excluded.source_path,
		   content_hash = excluded.content_hash,
		   triggers_json = excluded.triggers_json,
		   tool_chain_json = excluded.tool_chain_json,
		   policy_overrides_json = excluded.policy_overrides_json,
		   script_path = excluded.script_path,
		   risk_level = excluded.risk_level,
		   enabled = excluded.enabled,
		   last_loaded_at = excluded.last_loaded_at,
		   version = excluded.version,
		   author = excluded.author,
		   registry_source = excluded.registry_source`,
		row.ID, row.Name, row.Kind, nullIfEmpty(row.Description), row.SourcePath, row.ContentHash,
		nullIfEmpty(row.TriggersJSON), nullIfEmpty(row.ToolChainJSON), nullIfEmpty(row.PolicyOverridesJSON),
		nullIfEmpty(row.ScriptPath), row.RiskLevel, boolToInt(row.Enabled),
		nullIfEmpty(row.LastLoadedAt), row.Version, row.Author, row.RegistrySource)
	return err
}

// GetByName retrieves a skill by name. Returns nil if not found.
func (r *SkillsRepository) GetByName(ctx context.Context, name string) (*SkillRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, name, kind, COALESCE(description,''), source_path, content_hash,
		        COALESCE(triggers_json,''), COALESCE(tool_chain_json,''), COALESCE(policy_overrides_json,''),
		        COALESCE(script_path,''), risk_level, enabled, COALESCE(last_loaded_at,''), created_at,
		        version, author, registry_source
		 FROM skills WHERE name = ?`, name)
	var s SkillRow
	var enabled int
	err := row.Scan(&s.ID, &s.Name, &s.Kind, &s.Description, &s.SourcePath, &s.ContentHash,
		&s.TriggersJSON, &s.ToolChainJSON, &s.PolicyOverridesJSON, &s.ScriptPath,
		&s.RiskLevel, &enabled, &s.LastLoadedAt, &s.CreatedAt, &s.Version, &s.Author, &s.RegistrySource)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Enabled = enabled == 1
	return &s, nil
}

// List returns all skills, optionally filtered by kind (empty = all).
func (r *SkillsRepository) List(ctx context.Context, kind string) ([]SkillRow, error) {
	query := `SELECT id, name, kind, COALESCE(description,''), source_path, content_hash,
		         COALESCE(triggers_json,''), COALESCE(tool_chain_json,''), COALESCE(policy_overrides_json,''),
		         COALESCE(script_path,''), risk_level, enabled, COALESCE(last_loaded_at,''), created_at,
		         version, author, registry_source
		  FROM skills`
	var args []any
	if kind != "" {
		query += " WHERE kind = ?"
		args = append(args, kind)
	}
	query += " ORDER BY name ASC"

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []SkillRow
	for rows.Next() {
		var s SkillRow
		var enabled int
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.Description, &s.SourcePath, &s.ContentHash,
			&s.TriggersJSON, &s.ToolChainJSON, &s.PolicyOverridesJSON, &s.ScriptPath,
			&s.RiskLevel, &enabled, &s.LastLoadedAt, &s.CreatedAt, &s.Version, &s.Author, &s.RegistrySource); err != nil {
			return nil, err
		}
		s.Enabled = enabled == 1
		result = append(result, s)
	}
	return result, rows.Err()
}

// SetEnabled toggles a skill's enabled state.
func (r *SkillsRepository) SetEnabled(ctx context.Context, name string, enabled bool) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE skills SET enabled = ? WHERE name = ?`, boolToInt(enabled), name)
	return err
}

// Delete removes a skill by name.
func (r *SkillsRepository) Delete(ctx context.Context, name string) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM skills WHERE name = ?`, name)
	return err
}
