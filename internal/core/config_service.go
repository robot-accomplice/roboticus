package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
)

// DBExecer is the narrow interface for database writes needed by config service.
// Avoids importing internal/db which would create a cycle (core → db → core).
type DBExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ApplyConfigPatch loads the config from disk, merges the patch via JSON
// round-trip, validates the result, writes valid TOML back to disk, and
// persists an audit trail entry. This is the canonical config mutation path.
//
// Ownership: This function lives in core (not routes) because config mutation
// is business logic that affects shared behavior (architecture_rules.md §4.2).
// Route handlers call this; they do not implement config persistence.
func ApplyConfigPatch(ctx context.Context, store DBExecer, patch map[string]any) (string, error) {
	path := ConfigFilePath()

	// Load the existing config from disk (falls back to defaults if absent).
	merged, err := LoadConfigFromFile(path)
	if err != nil {
		return path, fmt.Errorf("failed to load config: %w", err)
	}

	// Apply the patch: marshal the current config to JSON, overlay the patch
	// keys, then unmarshal back into the Config struct. This ensures only
	// known fields are accepted and types are enforced.
	base, err := json.Marshal(merged)
	if err != nil {
		return path, fmt.Errorf("failed to marshal config: %w", err)
	}

	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		return path, fmt.Errorf("failed to parse config: %w", err)
	}

	for k, v := range patch {
		baseMap[k] = v
	}

	// Coerce string values to []string for known array fields that the
	// frontend may send as flat strings (e.g., "path1\npath2" or "path1").
	coerceArrayFields(baseMap)

	patchedJSON, err := json.Marshal(baseMap)
	if err != nil {
		return path, fmt.Errorf("failed to marshal patched config: %w", err)
	}

	if err := json.Unmarshal(patchedJSON, &merged); err != nil {
		return path, fmt.Errorf("patch produced invalid config: %w", err)
	}

	// Validate the merged config.
	if err := merged.Validate(); err != nil {
		return path, fmt.Errorf("validation failed: %w", err)
	}

	// Persist patch to identity table for audit trail.
	if store != nil {
		auditJSON, _ := json.Marshal(patch)
		if _, err := store.ExecContext(ctx,
			`INSERT INTO identity (key, value) VALUES ('config_patch:latest', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			string(auditJSON)); err != nil {
			log.Warn().Err(err).Msg("config_service: audit trail persist failed")
		}
	}

	// Write the validated config as TOML.
	tomlBytes, err := MarshalTOML(&merged)
	if err != nil {
		return path, fmt.Errorf("failed to marshal TOML: %w", err)
	}

	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return path, fmt.Errorf("failed to create config dir: %w", err)
	}
	if err := os.WriteFile(path, tomlBytes, 0o644); err != nil {
		return path, fmt.Errorf("failed to write config file: %w", err)
	}

	return path, nil
}

// coerceArrayFields walks a config map and converts string values to []string
// for fields that the Config struct declares as []string. This prevents JSON
// unmarshal failures when the frontend sends a flat string instead of an array.
func coerceArrayFields(m map[string]any) {
	// Known array field paths (section → field names).
	arrayFields := map[string][]string{
		"security": {"allowed_paths", "protected_paths", "extra_protected_paths",
			"interpreter_allow", "script_allowed_paths", "trusted_sender_ids"},
		"skills": {"allowed_interpreters"},
		"models": {"fallback"},
	}
	for section, fields := range arrayFields {
		sub, ok := m[section].(map[string]any)
		if !ok {
			continue
		}
		for _, f := range fields {
			v, exists := sub[f]
			if !exists {
				continue
			}
			if s, isStr := v.(string); isStr {
				// Split newline-separated or comma-separated strings into arrays.
				parts := strings.Split(s, "\n")
				var result []string
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						result = append(result, p)
					}
				}
				if len(result) == 0 {
					sub[f] = []string{}
				} else {
					sub[f] = result
				}
			}
		}
	}
	// Also handle nested security.filesystem array fields.
	if sec, ok := m["security"].(map[string]any); ok {
		if fs, ok := sec["filesystem"].(map[string]any); ok {
			for _, f := range []string{"tool_allowed_paths", "script_allowed_paths"} {
				if s, isStr := fs[f].(string); isStr {
					parts := strings.Split(s, "\n")
					var result []string
					for _, p := range parts {
						p = strings.TrimSpace(p)
						if p != "" {
							result = append(result, p)
						}
					}
					if len(result) == 0 {
						fs[f] = []string{}
					} else {
						fs[f] = result
					}
				}
			}
		}
	}

	// Handle top-level array fields.
	topLevelArrays := []string{"disabled_bundled_providers"}
	for _, f := range topLevelArrays {
		v, exists := m[f]
		if !exists {
			continue
		}
		if s, isStr := v.(string); isStr {
			parts := strings.Split(s, "\n")
			var result []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					result = append(result, p)
				}
			}
			if len(result) == 0 {
				m[f] = []string{}
			} else {
				m[f] = result
			}
		} else if v == nil {
			m[f] = []string{}
		}
	}
}

// ReadConfigRaw reads the raw TOML config file content.
func ReadConfigRaw() ([]byte, error) {
	return os.ReadFile(ConfigFilePath())
}

// WriteConfigRaw writes raw TOML content directly to the config file.
func WriteConfigRaw(content []byte) (string, error) {
	path := ConfigFilePath()
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return path, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return path, fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

// WritePluginFile writes a plugin script to the given directory.
func WritePluginFile(pluginsDir, name, content string) (string, error) {
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugins dir: %w", err)
	}
	path := pluginsDir + "/" + name + ".lua"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write plugin: %w", err)
	}
	return path, nil
}
