package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// SkillType distinguishes between structured and instruction skills.
type SkillType int

const (
	SkillStructured  SkillType = iota // TOML manifest with schema
	SkillInstruction                  // Markdown with YAML frontmatter
)

// SkillTrigger defines when a skill should auto-activate.
type SkillTrigger struct {
	Keywords []string `yaml:"keywords" json:"keywords"`
}

// SkillManifest holds metadata for a structured skill (from TOML/YAML frontmatter).
type SkillManifest struct {
	Name        string       `yaml:"name" json:"name"`
	Description string       `yaml:"description" json:"description"`
	Version     string       `yaml:"version" json:"version"`
	Author      string       `yaml:"author" json:"author"`
	Triggers    SkillTrigger `yaml:"triggers" json:"triggers"`
	PairedTool  string       `yaml:"paired_tool" json:"paired_tool"`
	Priority    int          `yaml:"priority" json:"priority"`
}

// LoadedSkill wraps a loaded skill with its hash for change detection.
type LoadedSkill struct {
	Type       SkillType
	Manifest   SkillManifest
	Body       string // instruction body (markdown) or empty for structured
	Hash       string // SHA-256 of source file
	SourcePath string
}

// Name returns the skill's name.
func (s *LoadedSkill) Name() string { return s.Manifest.Name }

// Triggers returns the activation keywords.
func (s *LoadedSkill) Triggers() []string { return s.Manifest.Triggers.Keywords }

// SkillLoader discovers and loads skills from a directory.
type SkillLoader struct{}

// NewSkillLoader creates a skill loader.
func NewSkillLoader() *SkillLoader {
	return &SkillLoader{}
}

// LoadFromDir recursively loads .md and .toml skill files from a directory.
// Subdirectory failures are logged but don't abort the overall load.
func (sl *SkillLoader) LoadFromDir(dir string) []*LoadedSkill {
	var skills []*LoadedSkill

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("failed to read skills directory")
		return nil
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			// Recurse into subdirectories.
			sub := sl.LoadFromDir(path)
			skills = append(skills, sub...)
			continue
		}

		skill, err := sl.loadFile(path)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("skipping file")
			continue
		}
		skills = append(skills, skill)
	}
	return skills
}

// loadFile loads a single skill file.
func (sl *SkillLoader) loadFile(path string) (*LoadedSkill, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		return sl.loadInstruction(path)
	case ".toml", ".yaml", ".yml":
		return sl.loadStructured(path)
	default:
		return nil, fmt.Errorf("unsupported skill format: %s", ext)
	}
}

// loadInstruction parses a markdown file with YAML frontmatter.
func (sl *SkillLoader) loadInstruction(path string) (*LoadedSkill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	// Parse YAML frontmatter between --- delimiters.
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("no frontmatter in %s", path)
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return nil, fmt.Errorf("unclosed frontmatter in %s", path)
	}

	frontmatter := content[3 : end+3]
	body := strings.TrimSpace(content[end+6:])

	var manifest SkillManifest
	if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
		return nil, fmt.Errorf("invalid frontmatter in %s: %w", path, err)
	}

	if manifest.Priority == 0 {
		manifest.Priority = 5
	}

	return &LoadedSkill{
		Type:       SkillInstruction,
		Manifest:   manifest,
		Body:       body,
		Hash:       hash,
		SourcePath: path,
	}, nil
}

// loadStructured parses a TOML/YAML manifest file.
func (sl *SkillLoader) loadStructured(path string) (*LoadedSkill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	var manifest SkillManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest in %s: %w", path, err)
	}

	return &LoadedSkill{
		Type:       SkillStructured,
		Manifest:   manifest,
		Hash:       hash,
		SourcePath: path,
	}, nil
}

// SkillMatcher matches user input against loaded skill triggers.
type SkillMatcher struct {
	skills []*LoadedSkill
}

// NewSkillMatcher creates a matcher from pre-loaded skills.
func NewSkillMatcher(skills []*LoadedSkill) *SkillMatcher {
	return &SkillMatcher{skills: skills}
}

// SetSkills replaces the loaded skill set (used by hot-reload).
func (sm *SkillMatcher) SetSkills(skills []*LoadedSkill) {
	sm.skills = skills
}

// Match finds the highest-priority skill whose triggers match the content.
// Returns nil if no skill matches.
func (sm *SkillMatcher) Match(content string) *LoadedSkill {
	lower := strings.ToLower(content)

	var best *LoadedSkill
	for _, skill := range sm.skills {
		for _, kw := range skill.Triggers() {
			if strings.Contains(lower, strings.ToLower(kw)) {
				if best == nil || skill.Manifest.Priority > best.Manifest.Priority {
					best = skill
				}
				break
			}
		}
	}
	return best
}
