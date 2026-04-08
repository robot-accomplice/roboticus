package plugin

import (
	"fmt"
	"sync"

	"roboticus/internal/core"
)

// RegisteredPlugin tracks a plugin's state within the managed registry.
type RegisteredPlugin struct {
	Name     string             `json:"name"`
	Manifest core.SkillManifest `json:"manifest"`
	Active   bool               `json:"active"`
}

// PluginRegistry is a thread-safe registry for managing plugin lifecycle.
type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]*RegisteredPlugin
}

// NewPluginRegistry creates a new managed plugin registry.
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]*RegisteredPlugin),
	}
}

// Register adds a plugin to the registry. Returns an error if a plugin
// with the same name is already registered.
func (pr *PluginRegistry) Register(name string, manifest core.SkillManifest) error {
	if name == "" {
		return fmt.Errorf("plugin registry: name must not be empty")
	}

	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.plugins[name]; exists {
		return fmt.Errorf("plugin registry: %q already registered", name)
	}

	pr.plugins[name] = &RegisteredPlugin{
		Name:     name,
		Manifest: manifest,
		Active:   true,
	}
	return nil
}

// Get retrieves a registered plugin by name.
func (pr *PluginRegistry) Get(name string) (*RegisteredPlugin, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	p, ok := pr.plugins[name]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent mutation.
	cp := *p
	return &cp, true
}

// List returns all registered plugins.
func (pr *PluginRegistry) List() []*RegisteredPlugin {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	result := make([]*RegisteredPlugin, 0, len(pr.plugins))
	for _, p := range pr.plugins {
		cp := *p
		result = append(result, &cp)
	}
	return result
}

// SetActive enables or disables a registered plugin.
func (pr *PluginRegistry) SetActive(name string, active bool) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	p, ok := pr.plugins[name]
	if !ok {
		return fmt.Errorf("plugin registry: %q not found", name)
	}

	p.Active = active
	return nil
}
