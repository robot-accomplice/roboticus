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
