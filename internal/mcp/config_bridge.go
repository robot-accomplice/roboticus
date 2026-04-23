package mcp

import (
	"os"
	"strings"

	"roboticus/internal/core"
)

// ConfigFromCoreEntry is the single authoritative bridge from persisted config
// into the MCP runtime contract. Daemon startup, route tests, and validation
// harnesses should all use this path so auth/header semantics cannot drift.
func ConfigFromCoreEntry(entry core.MCPServerEntry) McpServerConfig {
	env := cloneStringMap(entry.Env)
	headers := map[string]string{}
	if tokenEnv := strings.TrimSpace(entry.AuthTokenEnv); tokenEnv != "" {
		if token := strings.TrimSpace(os.Getenv(tokenEnv)); token != "" {
			headers["Authorization"] = "Bearer " + token
			if env == nil {
				env = map[string]string{}
			}
			// Preserve the original variable name for stdio targets that expect
			// the token in their child process environment.
			env[tokenEnv] = token
		}
	}
	if len(headers) == 0 {
		headers = nil
	}
	return McpServerConfig{
		Name:          entry.Name,
		Transport:     entry.Transport,
		Command:       entry.Command,
		Args:          append([]string(nil), entry.Args...),
		URL:           entry.URL,
		Env:           env,
		Headers:       headers,
		Enabled:       entry.Enabled,
		AuthTokenEnv:  entry.AuthTokenEnv,
		ToolAllowlist: append([]string(nil), entry.ToolAllowlist...),
	}
}

func ConfigsFromCoreEntries(entries []core.MCPServerEntry) []McpServerConfig {
	if len(entries) == 0 {
		return nil
	}
	out := make([]McpServerConfig, 0, len(entries))
	for _, entry := range entries {
		out = append(out, ConfigFromCoreEntry(entry))
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
