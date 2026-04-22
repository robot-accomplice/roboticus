package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"roboticus/internal/core"
)

var skillNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,127}$`)

type SkillCompositionStep struct {
	ToolName string          `json:"tool_name"`
	Params   json.RawMessage `json:"params,omitempty"`
}

type SkillCompositionSpec struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Kind            string                 `json:"kind"`
	Description     string                 `json:"description,omitempty"`
	Content         string                 `json:"content,omitempty"`
	Triggers        []string               `json:"triggers,omitempty"`
	Priority        int                    `json:"priority"`
	ToolChain       []SkillCompositionStep `json:"tool_chain,omitempty"`
	PolicyOverrides json.RawMessage        `json:"policy_overrides,omitempty"`
	ScriptPath      string                 `json:"script_path,omitempty"`
	RiskLevel       string                 `json:"risk_level"`
	Enabled         bool                   `json:"enabled"`
	Version         string                 `json:"version"`
	Author          string                 `json:"author"`
	RegistrySource  string                 `json:"registry_source"`
	SourcePath      string                 `json:"source_path"`
	ContentHash     string                 `json:"content_hash"`
}

type SkillCompositionRepository struct {
	store     *Store
	skillsDir string
	skills    *SkillsRepository
}

func NewSkillCompositionRepository(store *Store, skillsDir string) *SkillCompositionRepository {
	if strings.TrimSpace(skillsDir) == "" {
		skillsDir = filepath.Join(core.ConfigDir(), "skills")
	}
	return &SkillCompositionRepository{
		store:     store,
		skillsDir: skillsDir,
		skills:    NewSkillsRepository(store),
	}
}

func (r *SkillCompositionRepository) GetByName(ctx context.Context, name string) (*SkillCompositionSpec, error) {
	row, err := r.skills.GetByName(ctx, strings.TrimSpace(name))
	if err != nil || row == nil {
		return nil, err
	}
	return skillCompositionSpecFromRow(row), nil
}

func (r *SkillCompositionRepository) Upsert(ctx context.Context, spec SkillCompositionSpec) (bool, *SkillCompositionSpec, error) {
	if r.store == nil {
		return false, nil, fmt.Errorf("database store not available")
	}

	spec, err := normalizeSkillSpec(spec)
	if err != nil {
		return false, nil, err
	}

	existing, err := r.GetByName(ctx, spec.Name)
	if err != nil {
		return false, nil, err
	}
	if spec.ID == "" {
		if existing != nil && existing.ID != "" {
			spec.ID = existing.ID
		} else {
			spec.ID = NewID()
		}
	}

	rendered, path, err := r.render(spec)
	if err != nil {
		return false, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, nil, fmt.Errorf("create skills directory: %w", err)
	}
	if err := os.WriteFile(path, rendered, 0o644); err != nil {
		return false, nil, fmt.Errorf("write skill file: %w", err)
	}

	spec.SourcePath = path
	spec.ContentHash = core.HashSHA256(rendered)

	triggersJSON, _ := json.Marshal(spec.Triggers)
	toolNames := make([]string, 0, len(spec.ToolChain))
	for _, step := range spec.ToolChain {
		if strings.TrimSpace(step.ToolName) != "" {
			toolNames = append(toolNames, strings.TrimSpace(step.ToolName))
		}
	}
	toolChainJSON, _ := json.Marshal(toolNames)

	if err := r.skills.Upsert(ctx, SkillRow{
		ID:                  spec.ID,
		Name:                spec.Name,
		Kind:                spec.Kind,
		Description:         spec.Description,
		SourcePath:          spec.SourcePath,
		ContentHash:         spec.ContentHash,
		TriggersJSON:        string(triggersJSON),
		ToolChainJSON:       string(toolChainJSON),
		PolicyOverridesJSON: strings.TrimSpace(string(spec.PolicyOverrides)),
		ScriptPath:          spec.ScriptPath,
		RiskLevel:           spec.RiskLevel,
		Enabled:             spec.Enabled,
		Version:             spec.Version,
		Author:              spec.Author,
		RegistrySource:      spec.RegistrySource,
	}); err != nil {
		return false, nil, err
	}

	return existing == nil, &spec, nil
}

func normalizeSkillSpec(spec SkillCompositionSpec) (SkillCompositionSpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Kind = strings.ToLower(strings.TrimSpace(spec.Kind))
	spec.Description = strings.TrimSpace(spec.Description)
	spec.Content = strings.TrimSpace(spec.Content)
	spec.ScriptPath = strings.TrimSpace(spec.ScriptPath)
	spec.Version = strings.TrimSpace(spec.Version)
	spec.Author = strings.TrimSpace(spec.Author)
	spec.RegistrySource = strings.TrimSpace(spec.RegistrySource)
	spec.RiskLevel = strings.TrimSpace(spec.RiskLevel)

	if spec.Name == "" {
		return spec, fmt.Errorf("name is required")
	}
	if !skillNamePattern.MatchString(spec.Name) {
		return spec, fmt.Errorf("name must match %q", skillNamePattern.String())
	}
	if spec.Kind == "" {
		spec.Kind = "instruction"
	}
	if spec.Kind != "instruction" && spec.Kind != "structured" {
		return spec, fmt.Errorf("kind must be instruction or structured")
	}
	if spec.Priority <= 0 {
		spec.Priority = 5
	}
	if spec.Version == "" {
		spec.Version = "1.0.0"
	}
	if spec.Author == "" {
		spec.Author = "runtime"
	}
	if spec.RegistrySource == "" {
		spec.RegistrySource = "runtime"
	}
	if spec.RiskLevel == "" {
		spec.RiskLevel = "Caution"
	}
	switch spec.RiskLevel {
	case "Safe", "Caution", "Dangerous", "Forbidden":
	default:
		return spec, fmt.Errorf("risk_level must be one of Safe, Caution, Dangerous, Forbidden")
	}

	if spec.Kind == "instruction" && spec.Content == "" {
		return spec, fmt.Errorf("content is required for instruction skills")
	}
	if spec.Kind == "structured" && len(spec.ToolChain) == 0 {
		return spec, fmt.Errorf("tool_chain is required for structured skills")
	}
	for i := range spec.ToolChain {
		spec.ToolChain[i].ToolName = strings.TrimSpace(spec.ToolChain[i].ToolName)
		if spec.ToolChain[i].ToolName == "" {
			return spec, fmt.Errorf("tool_chain[%d].tool_name is required", i)
		}
	}
	return spec, nil
}

func (r *SkillCompositionRepository) render(spec SkillCompositionSpec) ([]byte, string, error) {
	switch spec.Kind {
	case "instruction":
		type instructionFrontmatter struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description,omitempty"`
			Version     string `yaml:"version,omitempty"`
			Author      string `yaml:"author,omitempty"`
			Triggers    struct {
				Keywords []string `yaml:"keywords,omitempty"`
			} `yaml:"triggers,omitempty"`
			Priority int `yaml:"priority,omitempty"`
		}

		var front instructionFrontmatter
		front.Name = spec.Name
		front.Description = spec.Description
		front.Version = spec.Version
		front.Author = spec.Author
		front.Triggers.Keywords = spec.Triggers
		front.Priority = spec.Priority

		meta, err := yaml.Marshal(front)
		if err != nil {
			return nil, "", fmt.Errorf("marshal instruction frontmatter: %w", err)
		}
		body := strings.TrimSpace(spec.Content)
		doc := "---\n" + string(meta) + "---\n" + body + "\n"
		return []byte(doc), filepath.Join(r.skillsDir, spec.Name+".md"), nil

	case "structured":
		type toolChainStep struct {
			Tool   string            `yaml:"tool"`
			Params map[string]string `yaml:"params,omitempty"`
		}
		type structuredManifest struct {
			Name        string          `yaml:"name"`
			Description string          `yaml:"description,omitempty"`
			Version     string          `yaml:"version,omitempty"`
			Author      string          `yaml:"author,omitempty"`
			Triggers    map[string]any  `yaml:"triggers,omitempty"`
			Priority    int             `yaml:"priority,omitempty"`
			ToolChain   []toolChainStep `yaml:"tool_chain,omitempty"`
		}

		manifest := structuredManifest{
			Name:        spec.Name,
			Description: spec.Description,
			Version:     spec.Version,
			Author:      spec.Author,
			Priority:    spec.Priority,
		}
		if len(spec.Triggers) > 0 {
			manifest.Triggers = map[string]any{"keywords": spec.Triggers}
		}
		for _, step := range spec.ToolChain {
			manifest.ToolChain = append(manifest.ToolChain, toolChainStep{
				Tool:   step.ToolName,
				Params: rawJSONToStringMap(step.Params),
			})
		}
		doc, err := yaml.Marshal(manifest)
		if err != nil {
			return nil, "", fmt.Errorf("marshal structured manifest: %w", err)
		}
		return doc, filepath.Join(r.skillsDir, spec.Name+".yaml"), nil
	default:
		return nil, "", fmt.Errorf("unsupported kind: %s", spec.Kind)
	}
}

func skillCompositionSpecFromRow(row *SkillRow) *SkillCompositionSpec {
	spec := &SkillCompositionSpec{
		ID:             row.ID,
		Name:           row.Name,
		Kind:           row.Kind,
		Description:    row.Description,
		Enabled:        row.Enabled,
		Version:        row.Version,
		Author:         row.Author,
		RegistrySource: row.RegistrySource,
		ScriptPath:     row.ScriptPath,
		RiskLevel:      row.RiskLevel,
		SourcePath:     row.SourcePath,
		ContentHash:    row.ContentHash,
	}
	_ = json.Unmarshal([]byte(row.TriggersJSON), &spec.Triggers)
	var toolNames []string
	_ = json.Unmarshal([]byte(row.ToolChainJSON), &toolNames)
	for _, name := range toolNames {
		spec.ToolChain = append(spec.ToolChain, SkillCompositionStep{ToolName: name})
	}
	if strings.TrimSpace(row.PolicyOverridesJSON) != "" {
		spec.PolicyOverrides = json.RawMessage(row.PolicyOverridesJSON)
	}
	return spec
}

func rawJSONToStringMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if len(payload) == 0 {
		return nil
	}
	out := make(map[string]string, len(payload))
	for key, value := range payload {
		out[key] = fmt.Sprint(value)
	}
	return out
}
