package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WasmCapability represents a capability granted to a WASM plugin.
type WasmCapability string

const (
	WasmCapNetwork    WasmCapability = "network"
	WasmCapFilesystem WasmCapability = "filesystem"
	WasmCapEnv        WasmCapability = "env"
)

// WasmPluginConfig configures a WASM plugin.
type WasmPluginConfig struct {
	Name               string           `json:"name"`
	WasmPath           string           `json:"wasm_path"`
	MemoryLimitBytes   uint64           `json:"memory_limit_bytes"`
	ExecutionTimeoutMs uint64           `json:"execution_timeout_ms"`
	Capabilities       []WasmCapability `json:"capabilities"`
}

// DefaultWasmConfig returns defaults for a WASM plugin.
func DefaultWasmConfig(name, path string) WasmPluginConfig {
	return WasmPluginConfig{
		Name:               name,
		WasmPath:           path,
		MemoryLimitBytes:   64 * 1024 * 1024, // 64 MB
		ExecutionTimeoutMs: 30_000,           // 30 seconds
	}
}

// WasmRuntime manages WASM plugin execution.
type WasmRuntime struct {
	mu            sync.RWMutex
	plugins       map[string]*wasmPlugin
	semaphore     chan struct{}
	maxConcurrent int
}

type wasmPlugin struct {
	config WasmPluginConfig
	module []byte // compiled WASM bytes
	loaded bool
}

// WasmToolResult is the result of a WASM tool execution.
type WasmToolResult struct {
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

const maxConcurrentWasm = 32

// NewWasmRuntime creates a WASM runtime with bounded concurrency.
func NewWasmRuntime() *WasmRuntime {
	return &WasmRuntime{
		plugins:       make(map[string]*wasmPlugin),
		semaphore:     make(chan struct{}, maxConcurrentWasm),
		maxConcurrent: maxConcurrentWasm,
	}
}

// Register adds a WASM plugin configuration.
func (wr *WasmRuntime) Register(cfg WasmPluginConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("wasm plugin name is required")
	}

	wr.mu.Lock()
	defer wr.mu.Unlock()

	if _, exists := wr.plugins[cfg.Name]; exists {
		return fmt.Errorf("wasm plugin %q already registered", cfg.Name)
	}

	wr.plugins[cfg.Name] = &wasmPlugin{config: cfg}
	return nil
}

// Load reads the WASM module bytes from disk.
func (wr *WasmRuntime) Load(name string) error {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	p, ok := wr.plugins[name]
	if !ok {
		return fmt.Errorf("wasm plugin %q not found", name)
	}

	data, err := os.ReadFile(p.config.WasmPath)
	if err != nil {
		return fmt.Errorf("reading wasm module %q: %w", name, err)
	}

	p.module = data
	p.loaded = true
	return nil
}

// Execute runs a WASM plugin's tool function with the given input.
func (wr *WasmRuntime) Execute(ctx context.Context, name string, input map[string]any) (*WasmToolResult, error) {
	wr.mu.RLock()
	p, ok := wr.plugins[name]
	wr.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("wasm plugin %q not found", name)
	}

	if !p.loaded || p.module == nil {
		return nil, fmt.Errorf("wasm plugin %q not loaded", name)
	}

	// Acquire concurrency slot.
	select {
	case wr.semaphore <- struct{}{}:
		defer func() { <-wr.semaphore }()
	case <-ctx.Done():
		return nil, fmt.Errorf("wasm execution cancelled: %w", ctx.Err())
	}

	// Apply execution timeout.
	timeout := time.Duration(p.config.ExecutionTimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Create a fresh wazero runtime for isolation.
	rtConfig := wazero.NewRuntimeConfig()
	if p.config.MemoryLimitBytes > 0 {
		rtConfig = rtConfig.WithMemoryLimitPages(uint32(p.config.MemoryLimitBytes / 65536))
	}

	rt := wazero.NewRuntimeWithConfig(execCtx, rtConfig)
	defer func() { _ = rt.Close(execCtx) }()

	// Instantiate WASI for basic I/O.
	wasi_snapshot_preview1.MustInstantiate(execCtx, rt)

	// Serialize input for the module.
	inputJSON, _ := json.Marshal(input)

	// Compile and instantiate the module.
	compiled, err := rt.CompileModule(execCtx, p.module)
	if err != nil {
		return &WasmToolResult{
			Error:      fmt.Sprintf("compile error: %v", err),
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	modConfig := wazero.NewModuleConfig().
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithArgs(name, string(inputJSON))

	mod, err := rt.InstantiateModule(execCtx, compiled, modConfig)
	if err != nil {
		return &WasmToolResult{
			Error:      fmt.Sprintf("instantiation error: %v", err),
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	_ = mod

	return &WasmToolResult{
		Output:     "WASM execution completed",
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// Unregister removes a WASM plugin.
func (wr *WasmRuntime) Unregister(name string) bool {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	if _, ok := wr.plugins[name]; !ok {
		return false
	}
	delete(wr.plugins, name)
	return true
}

// List returns all registered WASM plugin names.
func (wr *WasmRuntime) List() []string {
	wr.mu.RLock()
	defer wr.mu.RUnlock()
	names := make([]string, 0, len(wr.plugins))
	for name := range wr.plugins {
		names = append(names, name)
	}
	return names
}

// IsLoaded returns whether a plugin's module bytes have been loaded.
func (wr *WasmRuntime) IsLoaded(name string) bool {
	wr.mu.RLock()
	defer wr.mu.RUnlock()
	p, ok := wr.plugins[name]
	return ok && p.loaded
}

// PluginCount returns the number of registered plugins.
func (wr *WasmRuntime) PluginCount() int {
	wr.mu.RLock()
	defer wr.mu.RUnlock()
	return len(wr.plugins)
}

// AvailableSlots returns unused concurrency slots.
func (wr *WasmRuntime) AvailableSlots() int {
	return wr.maxConcurrent - len(wr.semaphore)
}
