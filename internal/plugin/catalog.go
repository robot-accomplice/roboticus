package plugin

import (
	"sort"
	"strings"
	"sync"

	"roboticus/internal/core"
)

// PluginCatalogEntry represents a plugin available in the catalog.
type PluginCatalogEntry struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description"`
	Manifest    core.SkillManifest `json:"manifest"`
}

// PluginCatalog is a thread-safe catalog of available plugins.
type PluginCatalog struct {
	mu      sync.RWMutex
	entries []PluginCatalogEntry
}

// NewPluginCatalog creates an empty plugin catalog.
func NewPluginCatalog() *PluginCatalog {
	return &PluginCatalog{}
}

// Register adds a plugin to the catalog.
func (pc *PluginCatalog) Register(entry PluginCatalogEntry) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Replace if already exists.
	for i, e := range pc.entries {
		if e.Name == entry.Name {
			pc.entries[i] = entry
			return
		}
	}
	pc.entries = append(pc.entries, entry)
}

// List returns all catalog entries sorted by name.
func (pc *PluginCatalog) List() []PluginCatalogEntry {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	result := make([]PluginCatalogEntry, len(pc.entries))
	copy(result, pc.entries)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Find looks up a plugin by name (case-insensitive).
func (pc *PluginCatalog) Find(name string) (PluginCatalogEntry, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	lower := strings.ToLower(name)
	for _, e := range pc.entries {
		if strings.ToLower(e.Name) == lower {
			return e, true
		}
	}
	return PluginCatalogEntry{}, false
}
