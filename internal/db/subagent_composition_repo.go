package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type SubagentSpec struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name,omitempty"`
	Model          string   `json:"model"`
	Role           string   `json:"role"`
	Description    string   `json:"description,omitempty"`
	FixedSkills    []string `json:"fixed_skills,omitempty"`
	FallbackModels []string `json:"fallback_models,omitempty"`
	Enabled        bool     `json:"enabled"`
}

type SubagentCompositionRepository struct {
	q Querier
}

func NewSubagentCompositionRepository(q Querier) *SubagentCompositionRepository {
	return &SubagentCompositionRepository{q: q}
}

func (r *SubagentCompositionRepository) GetByName(ctx context.Context, name string) (*SubagentSpec, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT id,
		       name,
		       COALESCE(display_name, ''),
		       model,
		       role,
		       COALESCE(description, ''),
		       COALESCE(skills_json, '[]'),
		       COALESCE(fallback_models_json, '[]'),
		       enabled
		  FROM sub_agents
		 WHERE name = ?`, strings.TrimSpace(name))

	var spec SubagentSpec
	var skillsJSON, fallbackJSON string
	var enabled int
	err := row.Scan(
		&spec.ID,
		&spec.Name,
		&spec.DisplayName,
		&spec.Model,
		&spec.Role,
		&spec.Description,
		&skillsJSON,
		&fallbackJSON,
		&enabled,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	spec.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(skillsJSON), &spec.FixedSkills)
	_ = json.Unmarshal([]byte(fallbackJSON), &spec.FallbackModels)
	if spec.DisplayName == "" {
		spec.DisplayName = spec.Name
	}
	if spec.Role == "" {
		spec.Role = "subagent"
	}
	return &spec, nil
}

func (r *SubagentCompositionRepository) Upsert(ctx context.Context, spec SubagentSpec) (bool, *SubagentSpec, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return false, nil, fmt.Errorf("name is required")
	}
	existing, err := r.GetByName(ctx, name)
	if err != nil {
		return false, nil, err
	}

	if spec.ID == "" {
		if existing != nil {
			spec.ID = existing.ID
		} else {
			spec.ID = uuid.NewString()
		}
	}
	if strings.TrimSpace(spec.DisplayName) == "" {
		spec.DisplayName = name
	}
	if strings.TrimSpace(spec.Role) == "" {
		spec.Role = "subagent"
	}

	skillsJSON, _ := json.Marshal(spec.FixedSkills)
	fallbackJSON, _ := json.Marshal(spec.FallbackModels)
	enabled := 0
	if spec.Enabled {
		enabled = 1
	}

	_, err = r.q.ExecContext(ctx, `
		INSERT INTO sub_agents (
			id, name, display_name, model, role, description,
			skills_json, fallback_models_json, enabled
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			display_name = excluded.display_name,
			model = excluded.model,
			role = excluded.role,
			description = excluded.description,
			skills_json = excluded.skills_json,
			fallback_models_json = excluded.fallback_models_json,
			enabled = excluded.enabled`,
		spec.ID,
		name,
		spec.DisplayName,
		spec.Model,
		spec.Role,
		spec.Description,
		string(skillsJSON),
		string(fallbackJSON),
		enabled,
	)
	if err != nil {
		return false, nil, err
	}
	updated, err := r.GetByName(ctx, name)
	if err != nil {
		return false, nil, err
	}
	return existing == nil, updated, nil
}
