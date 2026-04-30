package core

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.Port != DefaultServerPort {
		t.Errorf("default port = %d, want %d", cfg.Server.Port, DefaultServerPort)
	}
	if cfg.Server.Bind != DefaultServerBind {
		t.Errorf("default bind = %q, want %q", cfg.Server.Bind, DefaultServerBind)
	}
	if cfg.Agent.Name != "roboticus" {
		t.Errorf("default agent name = %q, want %q", cfg.Agent.Name, "roboticus")
	}
	if cfg.Database.Path == "" {
		t.Error("default database path should not be empty")
	}
	if cfg.Cache.PromptCompression {
		t.Error("prompt compression should be disabled by default")
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config should be valid: %v", err)
	}

	cfg.Server.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Error("port 0 should be invalid")
	}

	cfg.Server.Port = 70000
	if err := cfg.Validate(); err == nil {
		t.Error("port 70000 should be invalid")
	}

	cfg.Server.Port = 8080
	cfg.Database.Path = ""
	if err := cfg.Validate(); err == nil {
		t.Error("empty database path should be invalid")
	}
}

// TestWebToolsConfigDefaultsAreSafe verifies that the new WebTools
// config block ships with conservative defaults: the unfiltered
// http_fetch tool stays disabled by default so operators must
// explicitly opt in. web_search may be enabled because it is read-only
// against an external search endpoint that the operator configures.
func TestWebToolsConfigDefaultsAreSafe(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.WebTools.HTTPFetchEnabled {
		t.Fatal("WebTools.HTTPFetchEnabled defaults to true; expected false so operators opt in explicitly")
	}
	if cfg.WebTools.GholaEnabled {
		t.Fatal("WebTools.GholaEnabled defaults to true; expected false so operators opt in explicitly")
	}
}
