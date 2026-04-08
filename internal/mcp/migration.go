package mcp

import "github.com/rs/zerolog/log"

// LegacyClientConfig represents the old [mcp].clients format.
type LegacyClientConfig struct {
	Name      string            `json:"name" mapstructure:"name"`
	Transport string            `json:"transport" mapstructure:"transport"`
	Command   string            `json:"command" mapstructure:"command"`
	Args      []string          `json:"args" mapstructure:"args"`
	URL       string            `json:"url" mapstructure:"url"`
	Env       map[string]string `json:"env" mapstructure:"env"`
}

// MigrateLegacyClients converts old [mcp].clients entries to the current
// [mcp].servers format (McpServerConfig). This allows graceful migration
// from the legacy configuration schema.
func MigrateLegacyClients(clients []LegacyClientConfig) []McpServerConfig {
	if len(clients) == 0 {
		return nil
	}

	servers := make([]McpServerConfig, 0, len(clients))
	for _, c := range clients {
		if c.Name == "" {
			log.Warn().Msg("mcp migration: skipping client with empty name")
			continue
		}

		transport := c.Transport
		if transport == "" {
			// Infer transport from fields.
			if c.Command != "" {
				transport = "stdio"
			} else if c.URL != "" {
				transport = "sse"
			} else {
				log.Warn().Str("client", c.Name).Msg("mcp migration: cannot infer transport, skipping")
				continue
			}
		}

		servers = append(servers, McpServerConfig{
			Name:      c.Name,
			Transport: transport,
			Command:   c.Command,
			Args:      c.Args,
			URL:       c.URL,
			Env:       c.Env,
			Enabled:   true,
		})
	}

	if len(servers) > 0 {
		log.Info().Int("count", len(servers)).Msg("mcp migration: converted legacy clients to servers")
	}

	return servers
}
