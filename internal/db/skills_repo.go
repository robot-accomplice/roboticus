package db

import (
	"context"
	"database/sql"
	"fmt"
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

// DiscoveredSkillRow represents a file-backed runtime skill discovered during
// inventory reconciliation. Discovery refreshes metadata but preserves any
// existing operator enable/disable decision.
type DiscoveredSkillRow struct {
	Name           string
	Kind           string
	Description    string
	SourcePath     string
	ContentHash    string
	TriggersJSON   string
	ToolChainJSON  string
	Version        string
	Author         string
	RegistrySource string
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

// UpsertDiscovered inserts a newly discovered file-backed skill as enabled, or
// refreshes metadata for an existing row without changing its enabled state.
func (r *SkillsRepository) UpsertDiscovered(ctx context.Context, row DiscoveredSkillRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO skills
		 (id, name, kind, description, source_path, content_hash, triggers_json, tool_chain_json,
		  risk_level, enabled, last_loaded_at, version, author, registry_source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'Caution', 1, datetime('now'), ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   kind = excluded.kind,
		   description = excluded.description,
		   source_path = excluded.source_path,
		   content_hash = excluded.content_hash,
		   triggers_json = excluded.triggers_json,
		   tool_chain_json = excluded.tool_chain_json,
		   last_loaded_at = datetime('now'),
		   version = excluded.version,
		   author = excluded.author,
		   registry_source = excluded.registry_source`,
		NewID(), row.Name, row.Kind, nullIfEmpty(row.Description), row.SourcePath, row.ContentHash,
		nullIfEmpty(row.TriggersJSON), nullIfEmpty(row.ToolChainJSON),
		stringOr(row.Version, "0.0.0"), stringOr(row.Author, "local"), stringOr(row.RegistrySource, "local"))
	return err
}

// TouchSkillUsage records that a skill actually executed on the live path.
func (r *SkillsRepository) TouchSkillUsage(ctx context.Context, name string) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE skills
		    SET usage_count = usage_count + 1,
		        last_used_at = datetime('now')
		  WHERE name = ?`,
		name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
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
	res, err := r.q.ExecContext(ctx,
		`UPDATE skills SET enabled = ? WHERE name = ?`, boolToInt(enabled), name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// Delete removes a skill by name.
func (r *SkillsRepository) Delete(ctx context.Context, name string) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM skills WHERE name = ?`, name)
	return err
}

// DeleteByID removes a skill by ID. Returns the number of rows affected.
func (r *SkillsRepository) DeleteByID(ctx context.Context, id string) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM skills WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ToggleByID flips a skill's enabled flag by ID. Returns rows affected.
func (r *SkillsRepository) ToggleByID(ctx context.Context, id string) (int64, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE skills SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateField updates a single field on a skill by ID.
func (r *SkillsRepository) UpdateField(ctx context.Context, id, field, value string) error {
	// Only allow known fields to prevent SQL injection.
	allowed := map[string]bool{"description": true, "risk_level": true, "version": true, "enabled": true}
	if !allowed[field] {
		return fmt.Errorf("unknown skill field: %s", field)
	}
	_, err := r.q.ExecContext(ctx,
		`UPDATE skills SET `+field+` = ? WHERE id = ?`, value, id)
	return err
}

func stringOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
