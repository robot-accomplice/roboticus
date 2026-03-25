package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ObsidianConfig configures vault integration.
type ObsidianConfig struct {
	VaultPath    string   `toml:"vault_path" json:"vault_path"`
	AutoDetect   bool     `toml:"auto_detect" json:"auto_detect"`
	IndexOnStart bool     `toml:"index_on_start" json:"index_on_start"`
	IgnoreDirs   []string `toml:"ignore_dirs" json:"ignore_dirs"`
}

// WikiLink represents a parsed [[target|display]] link.
type WikiLink struct {
	Target  string // note title or path
	Display string // display text (empty = same as target)
	Heading string // optional #heading anchor
}

// ObsidianNote represents a parsed markdown note.
type ObsidianNote struct {
	Path          string            `json:"path"`
	Title         string            `json:"title"`
	Content       string            `json:"-"`
	Frontmatter   map[string]string `json:"frontmatter,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	OutgoingLinks []string          `json:"outgoing_links,omitempty"`
	ModifiedAt    time.Time         `json:"modified_at"`
}

// ObsidianVault manages an Obsidian vault's note index.
type ObsidianVault struct {
	mu            sync.RWMutex
	root          string
	config        ObsidianConfig
	notes         map[string]*ObsidianNote // path → note
	nameIndex     map[string]string        // lowercase title → path
	backlinkIndex map[string][]string      // target title → []source paths
}

var (
	wikilinkRE    = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	tagRE         = regexp.MustCompile(`(?:^|\s)#([a-zA-Z][a-zA-Z0-9_/-]*)`)
	frontmatterRE = regexp.MustCompile(`(?s)^---\n(.+?)\n---`)
)

// NewObsidianVault creates a vault manager.
func NewObsidianVault(cfg ObsidianConfig) *ObsidianVault {
	root := cfg.VaultPath
	if cfg.AutoDetect && root == "" {
		root = autoDetectVault()
	}
	return &ObsidianVault{
		root:          root,
		config:        cfg,
		notes:         make(map[string]*ObsidianNote),
		nameIndex:     make(map[string]string),
		backlinkIndex: make(map[string][]string),
	}
}

// Root returns the vault root path.
func (v *ObsidianVault) Root() string { return v.root }

// Scan walks the vault and indexes all markdown notes.
func (v *ObsidianVault) Scan() error {
	if v.root == "" {
		return nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Clear old index.
	v.notes = make(map[string]*ObsidianNote)
	v.nameIndex = make(map[string]string)
	v.backlinkIndex = make(map[string][]string)

	ignoreDirs := make(map[string]bool)
	for _, d := range v.config.IgnoreDirs {
		ignoreDirs[d] = true
	}

	err := filepath.Walk(v.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || ignoreDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}

		relPath, _ := filepath.Rel(v.root, path)
		relPath = filepath.ToSlash(relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)

		note := &ObsidianNote{
			Path:       relPath,
			Title:      titleFromFilename(info.Name()),
			Content:    content,
			ModifiedAt: info.ModTime(),
		}

		note.Frontmatter = parseFrontmatter(content)
		if fmTitle, ok := note.Frontmatter["title"]; ok && fmTitle != "" {
			note.Title = fmTitle
		}
		note.Tags = extractTags(content)
		note.OutgoingLinks = extractWikiLinkTargets(content)

		v.notes[relPath] = note
		v.nameIndex[strings.ToLower(note.Title)] = relPath

		// Build backlink index.
		for _, target := range note.OutgoingLinks {
			key := strings.ToLower(target)
			v.backlinkIndex[key] = append(v.backlinkIndex[key], relPath)
		}

		return nil
	})

	if err != nil {
		return err
	}

	log.Info().Int("notes", len(v.notes)).Str("vault", v.root).Msg("obsidian vault indexed")
	return nil
}

// ResolveWikiLink resolves a [[target]] or [[target#heading]] to a note path.
func (v *ObsidianVault) ResolveWikiLink(target string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	// Strip heading anchor.
	clean := target
	if idx := strings.Index(clean, "#"); idx >= 0 {
		clean = clean[:idx]
	}
	clean = strings.TrimSpace(clean)

	path, ok := v.nameIndex[strings.ToLower(clean)]
	return path, ok
}

// GetBacklinks returns all notes that link to the given title.
func (v *ObsidianVault) GetBacklinks(title string) []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.backlinkIndex[strings.ToLower(title)]
}

// SearchNotes finds notes whose title or content contains the query.
func (v *ObsidianVault) SearchNotes(query string, limit int) []*ObsidianNote {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}

	lower := strings.ToLower(query)
	var results []*ObsidianNote
	for _, note := range v.notes {
		if strings.Contains(strings.ToLower(note.Title), lower) ||
			strings.Contains(strings.ToLower(note.Content), lower) {
			results = append(results, note)
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

// ReadNote returns a note by path.
func (v *ObsidianVault) ReadNote(path string) *ObsidianNote {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.notes[path]
}

// ListAllTags returns all unique tags across the vault.
func (v *ObsidianVault) ListAllTags() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	tagSet := make(map[string]bool)
	for _, note := range v.notes {
		for _, tag := range note.Tags {
			tagSet[tag] = true
		}
	}
	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	return tags
}

// NoteCount returns the number of indexed notes.
func (v *ObsidianVault) NoteCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.notes)
}

// --- Helpers ---

func titleFromFilename(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// ParseWikiLink parses [[target|display]] or [[target#heading|display]].
func ParseWikiLink(raw string) WikiLink {
	// Strip [[ and ]].
	inner := strings.TrimPrefix(raw, "[[")
	inner = strings.TrimSuffix(inner, "]]")

	wl := WikiLink{}

	// Split display text.
	if idx := strings.Index(inner, "|"); idx >= 0 {
		wl.Display = inner[idx+1:]
		inner = inner[:idx]
	}

	// Split heading.
	if idx := strings.Index(inner, "#"); idx >= 0 {
		wl.Heading = inner[idx+1:]
		inner = inner[:idx]
	}

	wl.Target = strings.TrimSpace(inner)
	if wl.Display == "" {
		wl.Display = wl.Target
	}

	return wl
}

func parseFrontmatter(content string) map[string]string {
	m := make(map[string]string)
	match := frontmatterRE.FindStringSubmatch(content)
	if match == nil {
		return m
	}
	for _, line := range strings.Split(match[1], "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key != "" {
				m[key] = val
			}
		}
	}
	return m
}

func extractTags(content string) []string {
	matches := tagRE.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var tags []string
	for _, m := range matches {
		tag := m[1]
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

func extractWikiLinkTargets(content string) []string {
	matches := wikilinkRE.FindAllStringSubmatch(content, -1)
	var targets []string
	for _, m := range matches {
		wl := ParseWikiLink(m[0])
		if wl.Target != "" {
			targets = append(targets, wl.Target)
		}
	}
	return targets
}

func autoDetectVault() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Documents", "Obsidian"),
		filepath.Join(home, "obsidian"),
		filepath.Join(home, "vault"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
}
