package mcp

import (
	"os"
	"testing"

	"roboticus/internal/core"
)

func TestConfigFromCoreEntry_AuthTokenPropagatesToHeadersAndEnv(t *testing.T) {
	const envName = "ROBOTICUS_TEST_MCP_TOKEN"
	t.Setenv(envName, "secret-token")

	cfg := ConfigFromCoreEntry(core.MCPServerEntry{
		Name:          "secure-sse",
		Transport:     "sse",
		URL:           "https://example.test/sse",
		AuthTokenEnv:  envName,
		Env:           map[string]string{"EXISTING": "1"},
		ToolAllowlist: []string{"echo"},
		Enabled:       true,
	})

	if got := cfg.Headers["Authorization"]; got != "Bearer secret-token" {
		t.Fatalf("authorization header = %q, want Bearer secret-token", got)
	}
	if got := cfg.Env[envName]; got != "secret-token" {
		t.Fatalf("env propagated token = %q, want secret-token", got)
	}
	if cfg.ToolAllowlist[0] != "echo" {
		t.Fatalf("tool allowlist = %v", cfg.ToolAllowlist)
	}
}

func TestConfigsFromCoreEntries_ClonesInput(t *testing.T) {
	entries := []core.MCPServerEntry{{
		Name:      "fixture",
		Transport: "stdio",
		Command:   "echo",
		Args:      []string{"hello"},
		Env:       map[string]string{"A": "1"},
		Enabled:   true,
	}}

	cfgs := ConfigsFromCoreEntries(entries)
	if len(cfgs) != 1 {
		t.Fatalf("config count = %d, want 1", len(cfgs))
	}
	entries[0].Args[0] = "mutated"
	entries[0].Env["A"] = "2"
	if cfgs[0].Args[0] != "hello" {
		t.Fatalf("args were not cloned: %v", cfgs[0].Args)
	}
	if cfgs[0].Env["A"] != "1" {
		t.Fatalf("env was not cloned: %v", cfgs[0].Env)
	}
}

func TestConfigFromCoreEntry_EmptyAuthTokenDoesNotInventHeaders(t *testing.T) {
	const envName = "ROBOTICUS_TEST_MCP_EMPTY_TOKEN"
	_ = os.Unsetenv(envName)

	cfg := ConfigFromCoreEntry(core.MCPServerEntry{
		Name:         "fixture",
		Transport:    "sse",
		URL:          "https://example.test/sse",
		AuthTokenEnv: envName,
	})

	if cfg.Headers != nil {
		t.Fatalf("headers = %v, want nil", cfg.Headers)
	}
}
