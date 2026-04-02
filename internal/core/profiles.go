package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProfileEntry describes a single agent configuration profile.
type ProfileEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`      // relative to base dir; empty = root
	Active      bool   `json:"active"`
	InstalledAt string `json:"installed_at,omitempty"`
	Version     string `json:"version,omitempty"`
	Source      string `json:"source,omitempty"` // "local", "registry", "manual"
}

// ProfileRegistry manages named agent configuration profiles.
// Profiles are stored as separate config directories under the base path.
type ProfileRegistry struct {
	basePath string
	profiles map[string]*ProfileEntry
}

// NewProfileRegistry creates a registry at the given base path.
func NewProfileRegistry(basePath string) *ProfileRegistry {
	reg := &ProfileRegistry{
		basePath: basePath,
		profiles: make(map[string]*ProfileEntry),
	}
	// Default profile always exists.
	reg.profiles["default"] = &ProfileEntry{
		Name:   "Default",
		Path:   "",
		Active: true,
		Source: "builtin",
	}
	return reg
}

// List returns all profiles sorted by name.
func (r *ProfileRegistry) List() []ProfileEntry {
	entries := make([]ProfileEntry, 0, len(r.profiles))
	for _, p := range r.profiles {
		entries = append(entries, *p)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// Create adds a new profile with the given name.
func (r *ProfileRegistry) Create(id, description string) (*ProfileEntry, error) {
	if _, exists := r.profiles[id]; exists {
		return nil, fmt.Errorf("profile %q already exists", id)
	}
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return nil, fmt.Errorf("profile name cannot be empty")
	}

	profilePath := filepath.Join("profiles", id)
	absPath := filepath.Join(r.basePath, profilePath)

	// Create profile directories.
	for _, subdir := range []string{"", "workspace", "skills"} {
		if err := os.MkdirAll(filepath.Join(absPath, subdir), 0755); err != nil {
			return nil, fmt.Errorf("create profile directory: %w", err)
		}
	}

	entry := &ProfileEntry{
		Name:        id,
		Description: description,
		Path:        profilePath,
		Active:      false,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Source:      "local",
	}
	r.profiles[id] = entry
	return entry, nil
}

// Switch marks the given profile as active and deactivates all others.
func (r *ProfileRegistry) Switch(id string) error {
	if _, exists := r.profiles[id]; !exists {
		return fmt.Errorf("profile %q not found", id)
	}
	for _, p := range r.profiles {
		p.Active = false
	}
	r.profiles[id].Active = true
	return nil
}

// Delete removes a profile by ID. Cannot delete the default profile.
func (r *ProfileRegistry) Delete(id string) error {
	if id == "default" {
		return fmt.Errorf("cannot delete the default profile")
	}
	entry, exists := r.profiles[id]
	if !exists {
		return fmt.Errorf("profile %q not found", id)
	}
	if entry.Active {
		// Switch to default before deleting.
		r.profiles["default"].Active = true
	}
	delete(r.profiles, id)
	return nil
}

// Active returns the currently active profile, or the default.
func (r *ProfileRegistry) Active() *ProfileEntry {
	for _, p := range r.profiles {
		if p.Active {
			return p
		}
	}
	return r.profiles["default"]
}

// ConfigDir returns the config directory for the given profile.
func (r *ProfileRegistry) ConfigDir(id string) string {
	entry, exists := r.profiles[id]
	if !exists || entry.Path == "" {
		return r.basePath
	}
	return filepath.Join(r.basePath, entry.Path)
}
