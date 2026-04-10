package daemon

import (
	"testing"

	"roboticus/internal/core"
)

func TestNew_FullBootstrap(t *testing.T) {
	dir := t.TempDir()
	cfg := core.DefaultConfig()
	cfg.Database.Path = dir + "/daemon_test.db"

	d, err := New(&cfg, BootOptions{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if d == nil {
		t.Fatal("nil")
	}
	if d.store == nil {
		t.Error("store should be initialized")
	}
	if d.llm == nil {
		t.Error("llm should be initialized")
	}
	if d.pipe == nil {
		t.Error("pipeline should be initialized")
	}
	if d.router == nil {
		t.Error("router should be initialized")
	}
	if d.appState == nil {
		t.Error("appState should be initialized")
	}
	if d.eventBus == nil {
		t.Error("eventBus should be initialized")
	}
	if d.bgWorker == nil {
		t.Error("bgWorker should be initialized")
	}

	// Clean shutdown.
	_ = d.Stop(nil)
}

func TestNew_CustomConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := core.DefaultConfig()
	cfg.Database.Path = dir + "/custom.db"
	cfg.Server.Port = 9999
	cfg.Agent.Name = "TestBot"

	d, err := New(&cfg, BootOptions{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if d.cfg.Server.Port != 9999 {
		t.Errorf("port = %d", d.cfg.Server.Port)
	}
	if d.cfg.Agent.Name != "TestBot" {
		t.Errorf("name = %s", d.cfg.Agent.Name)
	}
	_ = d.Stop(nil)
}

func TestDaemon_StopIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := core.DefaultConfig()
	cfg.Database.Path = dir + "/idem.db"

	d, _ := New(&cfg, BootOptions{})
	_ = d.Stop(nil)
	_ = d.Stop(nil) // should not panic
}

func TestInstall_InvalidConfig(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Database.Path = "/nonexistent/deep/path/db.sqlite"
	// Install may fail or succeed depending on service manager — just exercise.
	_ = Install(&cfg)
}

func TestStatus_NotInstalled(t *testing.T) {
	cfg := core.DefaultConfig()
	_, _ = Status(&cfg) // exercise the code path
}
