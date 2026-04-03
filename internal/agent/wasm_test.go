package agent

import (
	"context"
	"testing"
)

func TestWasmRuntime_Register(t *testing.T) {
	wr := NewWasmRuntime()
	cfg := DefaultWasmConfig("test-plugin", "/tmp/test.wasm")

	if err := wr.Register(cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if wr.PluginCount() != 1 {
		t.Errorf("PluginCount = %d, want 1", wr.PluginCount())
	}
}

func TestWasmRuntime_DuplicateRegister(t *testing.T) {
	wr := NewWasmRuntime()
	cfg := DefaultWasmConfig("test", "/tmp/test.wasm")
	_ = wr.Register(cfg)

	err := wr.Register(cfg)
	if err == nil {
		t.Error("expected error for duplicate register")
	}
}

func TestWasmRuntime_EmptyName(t *testing.T) {
	wr := NewWasmRuntime()
	err := wr.Register(WasmPluginConfig{WasmPath: "/tmp/test.wasm"})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestWasmRuntime_LoadNonexistent(t *testing.T) {
	wr := NewWasmRuntime()
	_ = wr.Register(DefaultWasmConfig("test", "/nonexistent/path.wasm"))

	err := wr.Load("test")
	if err == nil {
		t.Error("expected error loading nonexistent file")
	}
}

func TestWasmRuntime_ExecuteNotLoaded(t *testing.T) {
	wr := NewWasmRuntime()
	_ = wr.Register(DefaultWasmConfig("test", "/tmp/test.wasm"))

	_, err := wr.Execute(context.Background(), "test", nil)
	if err == nil {
		t.Error("expected error executing unloaded plugin")
	}
}

func TestWasmRuntime_ExecuteNotFound(t *testing.T) {
	wr := NewWasmRuntime()
	_, err := wr.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestWasmRuntime_Unregister(t *testing.T) {
	wr := NewWasmRuntime()
	_ = wr.Register(DefaultWasmConfig("test", "/tmp/test.wasm"))

	if !wr.Unregister("test") {
		t.Error("Unregister should return true")
	}
	if wr.Unregister("test") {
		t.Error("second Unregister should return false")
	}
	if wr.PluginCount() != 0 {
		t.Errorf("PluginCount = %d after unregister, want 0", wr.PluginCount())
	}
}

func TestWasmRuntime_List(t *testing.T) {
	wr := NewWasmRuntime()
	_ = wr.Register(DefaultWasmConfig("a", "/tmp/a.wasm"))
	_ = wr.Register(DefaultWasmConfig("b", "/tmp/b.wasm"))

	names := wr.List()
	if len(names) != 2 {
		t.Errorf("List() returned %d names, want 2", len(names))
	}
}

func TestWasmRuntime_DefaultConfig(t *testing.T) {
	cfg := DefaultWasmConfig("test", "/path/to/module.wasm")
	if cfg.MemoryLimitBytes != 64*1024*1024 {
		t.Errorf("memory limit = %d, want %d", cfg.MemoryLimitBytes, 64*1024*1024)
	}
	if cfg.ExecutionTimeoutMs != 30000 {
		t.Errorf("timeout = %d, want 30000", cfg.ExecutionTimeoutMs)
	}
}

func TestWasmRuntime_AvailableSlots(t *testing.T) {
	wr := NewWasmRuntime()
	if wr.AvailableSlots() != maxConcurrentWasm {
		t.Errorf("initial slots = %d, want %d", wr.AvailableSlots(), maxConcurrentWasm)
	}
}

func TestWasmRuntime_IsLoaded(t *testing.T) {
	wr := NewWasmRuntime()
	_ = wr.Register(DefaultWasmConfig("test", "/tmp/test.wasm"))

	if wr.IsLoaded("test") {
		t.Error("should not be loaded before Load()")
	}
	if wr.IsLoaded("nonexistent") {
		t.Error("nonexistent should not be loaded")
	}
}
