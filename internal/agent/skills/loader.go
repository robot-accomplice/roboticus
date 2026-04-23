package skills

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"roboticus/internal/core"
)

// Type distinguishes between structured and instruction skills.
type Type int

const (
	Structured  Type = iota // TOML manifest with schema
	Instruction             // Markdown with YAML frontmatter
)

// Trigger defines when a skill should auto-activate.
type Trigger struct {
	Keywords []string `yaml:"keywords" json:"keywords"`
}

// ToolChainStep defines a single tool invocation within a structured skill.
type ToolChainStep struct {
	ToolName string            `yaml:"tool" json:"tool"`
	Params   map[string]string `yaml:"params" json:"params"`
}

// Manifest holds metadata for a structured skill (from TOML/YAML frontmatter).
type Manifest struct {
	Name        string          `yaml:"name" json:"name"`
	Description string          `yaml:"description" json:"description"`
	Version     string          `yaml:"version" json:"version"`
	Author      string          `yaml:"author" json:"author"`
	Triggers    Trigger         `yaml:"triggers" json:"triggers"`
	PairedTool  string          `yaml:"paired_tool" json:"paired_tool"`
	Priority    int             `yaml:"priority" json:"priority"`
	ToolChain   []ToolChainStep `yaml:"tool_chain" json:"tool_chain"`
}

// Skill wraps a loaded skill with its hash for change detection.
type Skill struct {
	Type       Type
	Manifest   Manifest
	Body       string // instruction body (markdown) or empty for structured
	Hash       string // SHA-256 of source file
	SourcePath string
}

// Name returns the skill's name.
func (s *Skill) Name() string { return s.Manifest.Name }

// Triggers returns the activation keywords.
func (s *Skill) Triggers() []string { return s.Manifest.Triggers.Keywords }

// Loader discovers and loads skills from a directory.
type Loader struct{}

// NewLoader creates a skill loader.
func NewLoader() *Loader {
	return &Loader{}
}

// LoadFromDir recursively loads .md and .toml skill files from a directory.
// Subdirectory failures are logged but don't abort the overall load.
func (sl *Loader) LoadFromDir(dir string) []*Skill {
	var skills []*Skill

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

// LoadFromPaths loads a list of concrete skill files, skipping invalid or
// missing entries without aborting the entire batch.
func (sl *Loader) LoadFromPaths(paths []string) []*Skill {
	var skills []*Skill
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}

		skill, err := sl.loadFile(path)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("skipping skill path")
			continue
		}
		skills = append(skills, skill)
	}
	return skills
}

// loadFile loads a single skill file.
func (sl *Loader) loadFile(path string) (*Skill, error) {
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
func (sl *Loader) loadInstruction(path string) (*Skill, error) {
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

	var manifest Manifest
	if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
		return nil, fmt.Errorf("invalid frontmatter in %s: %w", path, err)
	}

	if manifest.Priority == 0 {
		manifest.Priority = 5
	}

	return &Skill{
		Type:       Instruction,
		Manifest:   manifest,
		Body:       body,
		Hash:       hash,
		SourcePath: path,
	}, nil
}

// loadStructured parses a TOML/YAML manifest file.
func (sl *Loader) loadStructured(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest in %s: %w", path, err)
	}

	return &Skill{
		Type:       Structured,
		Manifest:   manifest,
		Hash:       hash,
		SourcePath: path,
	}, nil
}

// HashSkillContent returns the hex-encoded SHA-256 hash of the given content.
// Used for change detection and cache invalidation.
func HashSkillContent(content []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

// LoadedSkillKind distinguishes the two dual-format skill representations.
type LoadedSkillKind int

const (
	LoadedSkillStructured  LoadedSkillKind = iota // TOML/YAML manifest with tool chain
	LoadedSkillInstruction                        // Markdown with frontmatter
)

// LoadedSkill wraps a skill with its source format, core types, and content hash.
// Supports dual-format loading: structured manifests (core.SkillManifest) and
// instruction skills (core.InstructionSkill).
type LoadedSkill struct {
	Kind        LoadedSkillKind
	Structured  *core.SkillManifest
	Instruction *core.InstructionSkill
	Hash        string
	Path        string
}

// LoadRecursive walks one or more directories (e.g. learned/, custom/) and
// returns all skills found. Subdirectory failures are logged but don't abort.
func LoadRecursive(dirs ...string) []*Skill {
	loader := NewLoader()
	var all []*Skill
	for _, dir := range dirs {
		skills := loader.LoadFromDir(dir)
		all = append(all, skills...)
	}
	return all
}

// Matcher matches user input against loaded skill triggers.
type Matcher struct {
	skills []*Skill
}

// NewMatcher creates a matcher from pre-loaded skills.
func NewMatcher(skills []*Skill) *Matcher {
	return &Matcher{skills: skills}
}

// SetSkills replaces the loaded skill set (used by hot-reload).
func (sm *Matcher) SetSkills(skills []*Skill) {
	sm.skills = skills
}

// Match finds the highest-priority skill whose triggers match the content.
// Returns nil if no skill matches.
func (sm *Matcher) Match(content string) *Skill {
	lower := strings.ToLower(content)

	var best *Skill
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
