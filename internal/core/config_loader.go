package core

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
)

var (
	tomlUnmarshal = toml.Unmarshal
	tomlMarshal   = toml.Marshal
)

//go:embed bundled_providers.toml
var bundledProvidersTOML string

// LoadConfigFromFile reads and parses a TOML config file into a Config struct.
// It starts with DefaultConfig and overlays values from the file.
//
// v1.0.6: path fields are normalized (tilde-expanded) before return. Pre-fix,
// callers that used LoadConfigFromFile directly (bypassing
// cmdutil.LoadConfig's wrapper, which DID call NormalizePaths) got back
// literal "~/.roboticus/workspace" strings. That broke downstream
// os.Stat / os.Open calls silently — the firmware-migration path was
// the P1-G audit finding where a default config with
// `workspace = "~/.roboticus/workspace"` never matched any real file.
// Normalizing here means no caller can skip the step even if they don't
// realize it exists.
func LoadConfigFromFile(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// DefaultConfig carries literal "~/..." paths too — normalize so the
			// file-not-found branch matches the file-present branch.
			cfg.NormalizePaths()
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := tomlUnmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Resolve roboticus alias: working_budget_pct overrides working_budget when set.
	if cfg.Memory.WorkingBudgetPct != 0 {
		cfg.Memory.WorkingBudget = cfg.Memory.WorkingBudgetPct
	}
	cfg.NormalizePaths()
	return cfg, nil
}

// MarshalTOML serialises a Config as TOML bytes.
func MarshalTOML(cfg *Config) ([]byte, error) {
	return tomlMarshal(cfg)
}

// MergeBundledProviders adds default provider configs for well-known services.
// User-defined providers take precedence — bundled configs are only inserted
// if no provider with that name exists.
func (c *Config) MergeBundledProviders() {
	bundled := parseBundledProviders()
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	// Build disabled set for O(1) lookup.
	disabledSet := make(map[string]bool, len(c.DisabledBundledProviders))
	for _, d := range c.DisabledBundledProviders {
		disabledSet[strings.ToLower(strings.TrimSpace(d))] = true
	}

	for name, bcfg := range bundled {
		if disabledSet[strings.ToLower(name)] {
			continue
		}
		if existing, exists := c.Providers[name]; !exists {
			c.Providers[name] = bcfg
		} else {
			// Merge bundled defaults into user config for fields the user didn't set.
			if existing.URL == "" {
				existing.URL = bcfg.URL
			}
			if existing.Tier == "" {
				existing.Tier = bcfg.Tier
			}
			if existing.Format == "" {
				existing.Format = bcfg.Format
			}
			if existing.ChatPath == "" {
				existing.ChatPath = bcfg.ChatPath
			}
			if !existing.IsLocal && bcfg.IsLocal {
				existing.IsLocal = true
			}
			c.Providers[name] = existing
		}
	}
}

// parseBundledProviders decodes the embedded bundled_providers.toml.
func parseBundledProviders() map[string]ProviderConfig {
	// Parse manually since we don't want a viper dependency here.
	// The bundled file is simple enough for a lightweight parse.
	result := make(map[string]ProviderConfig)

	var current string     // e.g. "ollama"
	var extraTarget string // non-empty when inside [providers.<name>.extra_headers]
	var cfg ProviderConfig
	for _, line := range strings.Split(bundledProvidersTOML, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[providers.") {
			// Flush the previous provider.
			if current != "" {
				result[current] = cfg
			}
			inner := strings.TrimPrefix(line, "[providers.")
			inner = strings.TrimSuffix(inner, "]")
			// Check for sub-table like "anthropic.extra_headers".
			if strings.HasSuffix(inner, ".extra_headers") {
				provName := strings.TrimSuffix(inner, ".extra_headers")
				extraTarget = provName
				// Retrieve the already-stored provider to attach headers.
				if prev, ok := result[provName]; ok {
					current = provName
					cfg = prev
				}
			} else {
				extraTarget = ""
				current = inner
				cfg = ProviderConfig{}
			}
			continue
		}
		if current == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"")

		// If we are inside an extra_headers sub-table, store as header.
		if extraTarget != "" {
			if cfg.ExtraHeaders == nil {
				cfg.ExtraHeaders = make(map[string]string)
			}
			cfg.ExtraHeaders[key] = val
			continue
		}

		switch key {
		case "url":
			cfg.URL = val
		case "tier":
			cfg.Tier = val
		case "format":
			cfg.Format = val
		case "chat_path":
			cfg.ChatPath = val
		case "api_key_env":
			// Ignored — keys come from keystore, not env vars.
		case "is_local":
			cfg.IsLocal = val == "true"
		case "auth_header":
			cfg.AuthHeader = val
		case "embedding_path":
			cfg.EmbeddingPath = val
		case "embedding_model":
			cfg.EmbeddingModel = val
		case "embedding_dimensions":
			if _, err := fmt.Sscanf(val, "%d", &cfg.EmbeddingDimensions); err != nil {
				log.Warn().Err(err).Str("key", "embedding_dimensions").Str("val", val).Msg("config: invalid integer")
			}
		case "cost_per_input_token":
			if _, err := fmt.Sscanf(val, "%f", &cfg.CostPerInputToken); err != nil {
				log.Warn().Err(err).Str("key", "cost_per_input_token").Str("val", val).Msg("config: invalid float")
			}
		case "cost_per_output_token":
			if _, err := fmt.Sscanf(val, "%f", &cfg.CostPerOutputToken); err != nil {
				log.Warn().Err(err).Str("key", "cost_per_output_token").Str("val", val).Msg("config: invalid float")
			}
		}
	}
	if current != "" {
		result[current] = cfg
	}
	return result
}
