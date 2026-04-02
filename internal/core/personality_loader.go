package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// LoadOsConfig reads and parses an OS personality file (OS.toml).
func LoadOsConfig(workspaceDir, filename string) (OsConfig, error) {
	if filename == "" {
		filename = "OS.toml"
	}
	path := filepath.Join(workspaceDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultOsConfig(), nil
		}
		return OsConfig{}, fmt.Errorf("read OS config: %w", err)
	}

	var cfg OsConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return OsConfig{}, fmt.Errorf("parse OS config %s: %w", path, err)
	}
	return cfg, nil
}

// LoadFirmwareConfig reads and parses a firmware file (FIRMWARE.toml).
func LoadFirmwareConfig(workspaceDir, filename string) (FirmwareConfig, error) {
	if filename == "" {
		filename = "FIRMWARE.toml"
	}
	path := filepath.Join(workspaceDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultFirmwareConfig(), nil
		}
		return FirmwareConfig{}, fmt.Errorf("read firmware config: %w", err)
	}

	var cfg FirmwareConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return FirmwareConfig{}, fmt.Errorf("parse firmware config %s: %w", path, err)
	}
	return cfg, nil
}

// FormatFirmwareRules renders firmware rules as a prompt section.
func FormatFirmwareRules(fw FirmwareConfig) string {
	if len(fw.Rules) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range fw.Rules {
		switch r.RuleType {
		case "must":
			fmt.Fprintf(&b, "- MUST: %s\n", r.Rule)
		case "must_not":
			fmt.Fprintf(&b, "- MUST NOT: %s\n", r.Rule)
		default:
			fmt.Fprintf(&b, "- %s\n", r.Rule)
		}
	}
	return b.String()
}

// FormatOsPersonality renders the OS personality as a prompt section.
func FormatOsPersonality(os OsConfig) string {
	if os.PromptText != "" {
		return os.PromptText
	}
	// Fallback: generate from voice parameters.
	var parts []string
	if os.Voice.Formality != "" {
		parts = append(parts, "Formality: "+os.Voice.Formality)
	}
	if os.Voice.Verbosity != "" {
		parts = append(parts, "Verbosity: "+os.Voice.Verbosity)
	}
	if os.Voice.Humor != "" {
		parts = append(parts, "Humor: "+os.Voice.Humor)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ". ") + "."
}
