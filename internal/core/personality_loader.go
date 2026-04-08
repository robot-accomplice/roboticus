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
	// Also check voice-level prompt_text (TOML nests it under [voice]).
	if os.Voice.PromptText != "" {
		return os.Voice.PromptText
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

// GenerateOsTOML creates an OS.toml from quick-setup answers.
// Matches Rust's generate_os_toml().
func GenerateOsTOML(name, formality, proactiveness, domain string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s — Personality Configuration\n\n", name)
	sb.WriteString("[identity]\n")
	fmt.Fprintf(&sb, "name = %q\n", name)
	sb.WriteString("version = \"1.0\"\n")
	sb.WriteString("generated_by = \"quick-setup\"\n\n")
	sb.WriteString("[voice]\n")
	fmt.Fprintf(&sb, "formality = %q\n", formality)
	fmt.Fprintf(&sb, "proactiveness = %q\n", proactiveness)
	sb.WriteString("verbosity = \"concise\"\n")
	sb.WriteString("humor = \"dry\"\n")
	fmt.Fprintf(&sb, "domain = %q\n\n", domain)
	sb.WriteString("prompt_text = \"\"\"\n")
	fmt.Fprintf(&sb, "Be genuinely helpful. Skip filler phrases — just help.\n")
	fmt.Fprintf(&sb, "Have opinions when asked. An assistant with no personality is just a search engine.\n")
	fmt.Fprintf(&sb, "Earn trust through competence.\n")
	sb.WriteString("\"\"\"\n")
	return sb.String()
}

// GenerateFirmwareTOML creates a FIRMWARE.toml from quick-setup boundaries.
// Matches Rust's generate_firmware_toml().
func GenerateFirmwareTOML(boundaries string) string {
	var sb strings.Builder
	sb.WriteString("[approvals]\n")
	sb.WriteString("spending_threshold = 50.0\n")
	sb.WriteString("require_confirmation = \"risky\"\n\n")

	// Default safety rules.
	rules := []struct{ ruleType, rule string }{
		{"must", "Always disclose uncertainty honestly"},
		{"must", "Ask for confirmation before spending, deletion, or irreversible actions"},
		{"must", "Protect operator API keys and private data"},
		{"must", "Distinguish facts from inferences"},
		{"must_not", "Fabricate sources, citations, URLs, or data"},
		{"must_not", "Impersonate a human"},
		{"must_not", "Ignore safety guardrails"},
		{"must_not", "Share information across sessions without consent"},
	}

	for _, r := range rules {
		fmt.Fprintf(&sb, "[[rules]]\nrule_type = %q\nrule = %q\n\n", r.ruleType, r.rule)
	}

	// User-specified boundaries.
	if boundaries != "" {
		for _, b := range strings.Split(boundaries, ";") {
			b = strings.TrimSpace(b)
			if b != "" {
				fmt.Fprintf(&sb, "[[rules]]\nrule_type = \"boundary\"\nrule = %q\n\n", b)
			}
		}
	}

	return sb.String()
}

// LoadOperatorConfig reads and parses an operator profile (OPERATOR.toml).
func LoadOperatorConfig(workspaceDir, filename string) (OperatorConfig, error) {
	if filename == "" {
		filename = "OPERATOR.toml"
	}
	path := filepath.Join(workspaceDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultOperatorConfig(), nil
		}
		return OperatorConfig{}, fmt.Errorf("read operator config: %w", err)
	}

	var cfg OperatorConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return OperatorConfig{}, fmt.Errorf("parse operator config %s: %w", path, err)
	}
	return cfg, nil
}

// LoadDirectivesConfig reads and parses a directives file (DIRECTIVES.toml).
func LoadDirectivesConfig(workspaceDir, filename string) (DirectivesConfig, error) {
	if filename == "" {
		filename = "DIRECTIVES.toml"
	}
	path := filepath.Join(workspaceDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultDirectivesConfig(), nil
		}
		return DirectivesConfig{}, fmt.Errorf("read directives config: %w", err)
	}

	var cfg DirectivesConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return DirectivesConfig{}, fmt.Errorf("parse directives config %s: %w", path, err)
	}
	return cfg, nil
}

// FormatOperatorContext renders the operator profile as a prompt section.
// Returns "" if the operator config is empty (file didn't exist).
func FormatOperatorContext(op OperatorConfig) string {
	var parts []string
	if op.Identity.Name != "" {
		parts = append(parts, "Operator: "+op.Identity.Name)
	}
	if op.Identity.Role != "" {
		parts = append(parts, "Role: "+op.Identity.Role)
	}
	if op.Identity.Timezone != "" {
		parts = append(parts, "Timezone: "+op.Identity.Timezone)
	}
	if op.Context != "" {
		parts = append(parts, op.Context)
	}
	if len(op.Preferences.CommunicationChannels) > 0 {
		parts = append(parts, "Preferred channels: "+strings.Join(op.Preferences.CommunicationChannels, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// FormatDirectives renders goals, missions, and priorities as a prompt section.
// Returns "" if the directives config is empty (file didn't exist).
func FormatDirectives(dir DirectivesConfig) string {
	var sections []string

	if len(dir.Missions) > 0 {
		var missionLines []string
		for _, m := range dir.Missions {
			line := "- " + m.Name
			if m.Description != "" {
				line += ": " + m.Description
			}
			if m.Priority != "" {
				line += " [" + m.Priority + "]"
			}
			if m.Timeframe != "" {
				line += " (" + m.Timeframe + ")"
			}
			missionLines = append(missionLines, line)
		}
		sections = append(sections, "Active Missions:\n"+strings.Join(missionLines, "\n"))
	}

	if len(dir.Priorities) > 0 {
		var priorityLines []string
		for _, p := range dir.Priorities {
			priorityLines = append(priorityLines, "- "+p)
		}
		sections = append(sections, "Priorities:\n"+strings.Join(priorityLines, "\n"))
	}

	if len(dir.Goals.Monthly) > 0 {
		var goalLines []string
		for _, g := range dir.Goals.Monthly {
			goalLines = append(goalLines, "- "+g)
		}
		sections = append(sections, "Monthly Goals:\n"+strings.Join(goalLines, "\n"))
	}

	if len(dir.Goals.Yearly) > 0 {
		var goalLines []string
		for _, g := range dir.Goals.Yearly {
			goalLines = append(goalLines, "- "+g)
		}
		sections = append(sections, "Yearly Goals:\n"+strings.Join(goalLines, "\n"))
	}

	if dir.Context != "" {
		sections = append(sections, dir.Context)
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}
