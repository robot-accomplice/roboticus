package daemon

import (
	"os"
	"path/filepath"
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

func TestNew_LoadsPluginRegistryIntoAppState(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "echo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `
name = "echo-plugin"
version = "1.0.0"
description = "daemon startup plugin"

[[tools]]
name = "echo"
description = "Echo tool"
dangerous = false
parameters_schema = '{"type":"object","properties":{"text":{"type":"string"}}}'
`
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := core.DefaultConfig()
	cfg.Database.Path = filepath.Join(dir, "plugins.db")
	cfg.Plugins.Dir = filepath.Join(dir, "plugins")

	d, err := New(&cfg, BootOptions{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = d.Stop(nil) }()

	if d.appState == nil || d.appState.Plugins == nil {
		t.Fatal("plugin registry should be initialized on app state")
	}
	if got := len(d.appState.Plugins.List()); got != 1 {
		t.Fatalf("plugin count = %d, want 1", got)
	}
	tools := d.appState.Plugins.AllTools()
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("plugin tools = %+v, want echo", tools)
	}
	if d.appState.Tools.Get("echo") == nil {
		t.Fatal("plugin tool should be registered in main tool registry")
	}
}

func TestInstall_InvalidConfig(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Database.Path = "/nonexistent/deep/path/db.sqlite"
	// Install may fail or succeed depending on service manager — just exercise.
	// v1.0.6: Install now takes a configPath so the installed service is
	// pinned to the operator's explicit config — pass a test-only placeholder.
	_ = Install(&cfg, "/nonexistent/roboticus.toml")
}

func TestStatus_NotInstalled(t *testing.T) {
	cfg := core.DefaultConfig()
	_, _ = Status(&cfg) // exercise the code path
}
