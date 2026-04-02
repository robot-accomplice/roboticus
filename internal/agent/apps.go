package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// AppManifest describes an installable agent application.
type AppManifest struct {
	Package      AppPackage      `toml:"package"`
	Profile      AppProfile      `toml:"profile"`
	Requirements AppRequirements `toml:"requirements"`
}

// AppPackage holds metadata about the application package.
type AppPackage struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	MinVersion  string `toml:"min_roboticus_version"` // compat field
}

// AppProfile holds the agent identity configuration.
type AppProfile struct {
	AgentName    string `toml:"agent_name"`
	AgentID      string `toml:"agent_id"`
	DefaultTheme string `toml:"default_theme"`
}

// AppRequirements holds model and feature requirements for the app.
type AppRequirements struct {
	MinModelParams    string `toml:"min_model_params"`
	RecommendedModel  string `toml:"recommended_model"`
	DelegationEnabled bool   `toml:"delegation_enabled"`
}

// InstalledApp tracks an installed application.
type InstalledApp struct {
	Manifest AppManifest
	Path     string
	Enabled  bool
}

// AppManager handles app installation, listing, and removal.
type AppManager struct {
	mu      sync.RWMutex
	apps    map[string]*InstalledApp
	appsDir string
}

// NewAppManager creates an app manager with the given apps directory.
func NewAppManager(appsDir string) *AppManager {
	return &AppManager{
		apps:    make(map[string]*InstalledApp),
		appsDir: appsDir,
	}
}

// Install reads a manifest.toml from the given path and installs the app.
func (am *AppManager) Install(manifestPath string) (*InstalledApp, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var manifest AppManifest
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	if manifest.Package.Name == "" {
		return nil, fmt.Errorf("manifest missing package.name")
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.apps[manifest.Package.Name]; exists {
		return nil, fmt.Errorf("app %q already installed", manifest.Package.Name)
	}

	app := &InstalledApp{
		Manifest: manifest,
		Path:     filepath.Dir(manifestPath),
		Enabled:  true,
	}
	am.apps[manifest.Package.Name] = app
	return app, nil
}

// InstallFromMemory installs an app from an already-parsed manifest (for testing).
func (am *AppManager) InstallFromMemory(manifest AppManifest, path string) (*InstalledApp, error) {
	if manifest.Package.Name == "" {
		return nil, fmt.Errorf("manifest missing package.name")
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.apps[manifest.Package.Name]; exists {
		return nil, fmt.Errorf("app %q already installed", manifest.Package.Name)
	}

	app := &InstalledApp{Manifest: manifest, Path: path, Enabled: true}
	am.apps[manifest.Package.Name] = app
	return app, nil
}

// Uninstall removes an installed app by name.
func (am *AppManager) Uninstall(name string) bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	if _, exists := am.apps[name]; !exists {
		return false
	}
	delete(am.apps, name)
	return true
}

// List returns all installed apps.
func (am *AppManager) List() []InstalledApp {
	am.mu.RLock()
	defer am.mu.RUnlock()
	result := make([]InstalledApp, 0, len(am.apps))
	for _, app := range am.apps {
		result = append(result, *app)
	}
	return result
}

// Get returns an installed app by name.
func (am *AppManager) Get(name string) (*InstalledApp, bool) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	app, ok := am.apps[name]
	if !ok {
		return nil, false
	}
	return app, true
}

// SetEnabled toggles the enabled state of an installed app.
func (am *AppManager) SetEnabled(name string, enabled bool) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	app, ok := am.apps[name]
	if !ok {
		return fmt.Errorf("app %q not found", name)
	}
	app.Enabled = enabled
	return nil
}
