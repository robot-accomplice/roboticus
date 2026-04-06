package daemon

import (
	"testing"

	"github.com/kardianos/service"

	"roboticus/internal/core"
)

func TestServiceConfig(t *testing.T) {
	cfg := ServiceConfig()
	if cfg == nil {
		t.Fatal("should not be nil")
	}
	if cfg.Name != "roboticus" {
		t.Errorf("name = %s", cfg.Name)
	}
	if cfg.DisplayName == "" {
		t.Error("display name should not be empty")
	}
	if cfg.Description == "" {
		t.Error("description should not be empty")
	}
}

func TestNew_InvalidDB(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Database.Path = "/nonexistent/path/test.db"
	_, err := New(&cfg)
	if err == nil {
		t.Error("should fail with invalid DB path")
	}
}

func TestNew_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := core.DefaultConfig()
	cfg.Database.Path = dir + "/test.db"

	d, err := New(&cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if d == nil {
		t.Fatal("daemon should not be nil")
	}

	// Test Stop without Start.
	err = d.Stop(nil)
	if err != nil {
		t.Errorf("stop: %v", err)
	}
}

func TestDaemon_ImplementsServiceInterface(t *testing.T) {
	// Compile-time check that Daemon implements service.Interface.
	var _ service.Interface = (*Daemon)(nil)
}
