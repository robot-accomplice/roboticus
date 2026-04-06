package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"plugins": []any{
			map[string]any{"name": "calculator", "version": "1.0"},
		},
	}))
	defer cleanup()

	err := pluginsListCmd.RunE(pluginsListCmd, nil)
	if err != nil {
		t.Fatalf("plugins list: %v", err)
	}
}

func TestPluginsListCmd_FallbackToSkills(t *testing.T) {
	callCount := 0
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"plugins": []any{}})
	}))
	defer cleanup()

	err := pluginsListCmd.RunE(pluginsListCmd, nil)
	if err != nil {
		t.Fatalf("plugins list: %v", err)
	}
	if callCount < 1 {
		t.Errorf("expected at least 1 API call, got %d", callCount)
	}
}

func TestPluginsListCmd_BothEndpointsFail(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
	}))
	defer cleanup()

	err := pluginsListCmd.RunE(pluginsListCmd, nil)
	if err == nil {
		t.Fatal("expected error when both endpoints fail")
	}
}

func TestPluginsSearchCmd_RemoteCatalog(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "roboticus.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	origCfg := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = origCfg }()

	manifest := registryManifest{
		Version: "v2026.04.10",
		Packs: registryPacks{
			Plugins: &pluginCatalog{
				Catalog: []pluginCatalogEntry{
					{Name: "claude-code", Version: "0.1.0", Description: "Delegate coding tasks", Author: "Roboticus", Tier: "official"},
					{Name: "weather", Version: "1.0.0", Description: "Forecast lookup", Author: "Community", Tier: "community"},
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/registry/manifest.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	origRegistry := updateRegistryURL
	origClient := updateHTTPClient
	updateRegistryURL = server.URL + "/registry/manifest.json"
	updateHTTPClient = server.Client()
	defer func() {
		updateRegistryURL = origRegistry
		updateHTTPClient = origClient
	}()

	if err := pluginsSearchCmd.RunE(pluginsSearchCmd, []string{"claude"}); err != nil {
		t.Fatalf("plugins search: %v", err)
	}
}

func TestPluginsSearchCmd_NoCatalog(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "roboticus.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	origCfg := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = origCfg }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(registryManifest{Version: "v2026.04.10"})
	}))
	defer server.Close()

	origRegistry := updateRegistryURL
	origClient := updateHTTPClient
	updateRegistryURL = server.URL
	updateHTTPClient = server.Client()
	defer func() {
		updateRegistryURL = origRegistry
		updateHTTPClient = origClient
	}()

	if err := pluginsSearchCmd.RunE(pluginsSearchCmd, []string{"claude"}); err == nil {
		t.Fatal("expected error when plugin catalog is unavailable")
	}
}
