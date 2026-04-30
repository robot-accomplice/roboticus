package configcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"roboticus/cmd/internal/cmdutil"
	"strconv"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"roboticus/internal/core"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/config")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Get a specific config value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]

		// Try live runtime first via API.
		if data, err := cmdutil.APIGet("/api/config"); err == nil {
			if val, found := navigatePath(data, key); found {
				fmt.Printf("%s = %v\n", key, val)
				return nil
			}
		}

		// Fall back to local file via TOML parse.
		if tree, _, err := readConfigTOML(); err == nil {
			if val, found := navigatePath(tree, key); found {
				fmt.Printf("%s = %v\n", key, val)
				return nil
			}
		}

		// Last resort: check viper (env vars, defaults).
		if v := viper.Get(key); v != nil {
			fmt.Printf("%s = %v\n", key, v)
			return nil
		}

		return fmt.Errorf("key %q not found", key)
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := cmdutil.LoadConfig()
		if err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
		fmt.Println("Configuration is valid.")
		return nil
	},
}

var configLintCmd = &cobra.Command{
	Use:   "lint [file]",
	Short: "Parse and validate a config file without applying",
	Long:  "Lint checks TOML syntax, field validation, memory budgets, treasury constraints, and provider reachability without modifying the running config.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Determine file path.
		path := ""
		if len(args) > 0 {
			path = args[0]
		} else {
			path = core.ConfigFilePath()
		}

		// Read and parse TOML.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}

		var cfg core.Config
		if err := toml.Unmarshal(data, &cfg); err != nil {
			fmt.Printf("FAIL: TOML syntax error in %s:\n  %v\n", path, err)
			return err
		}

		// Run validation.
		if err := cfg.Validate(); err != nil {
			fmt.Printf("FAIL: validation error in %s:\n  %v\n", path, err)
			return err
		}
		core.WarnEmptyFilesystemAllowlistFailureOpen(&cfg)

		fmt.Printf("OK: %s is valid (%d bytes, %d sections)\n", path, len(data), countSections(data))
		return nil
	},
}

// countSections counts [section] headers in TOML data.
func countSections(data []byte) int {
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") {
			count++
		}
	}
	return count
}

var configCapabilitiesCmd = &cobra.Command{
	Use:   "capabilities",
	Short: "List available capabilities",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/config/capabilities")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set [path] [value]",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]

		tree, raw, err := readConfigTOML()
		if err != nil {
			return err
		}
		_ = raw

		parts := strings.Split(key, ".")
		setNestedValue(tree, parts, autoType(value))

		newRaw, err := toml.Marshal(tree)
		if err != nil {
			return fmt.Errorf("failed to marshal TOML: %w", err)
		}

		_, err = cmdutil.APIPut("/api/config/raw", map[string]any{"raw": string(newRaw)})
		if err != nil {
			// Fall back to writing the local file directly.
			configPath := filepath.Join(core.ConfigDir(), "roboticus.toml")
			if writeErr := os.WriteFile(configPath, newRaw, 0o600); writeErr != nil {
				return fmt.Errorf("API unreachable and local write failed: %w", writeErr)
			}
		}
		fmt.Printf("Set %s = %s\n", key, value)
		return nil
	},
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset [path]",
	Short: "Remove a config key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]

		tree, _, err := readConfigTOML()
		if err != nil {
			return err
		}

		parts := strings.Split(key, ".")
		deleteNestedKey(tree, parts)

		newRaw, err := toml.Marshal(tree)
		if err != nil {
			return fmt.Errorf("failed to marshal TOML: %w", err)
		}

		_, err = cmdutil.APIPut("/api/config/raw", map[string]any{"raw": string(newRaw)})
		if err != nil {
			configPath := filepath.Join(core.ConfigDir(), "roboticus.toml")
			if writeErr := os.WriteFile(configPath, newRaw, 0o600); writeErr != nil {
				return fmt.Errorf("API unreachable and local write failed: %w", writeErr)
			}
		}
		fmt.Printf("Unset %s\n", key)
		return nil
	},
}

var configBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create a backup of the config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		configDir := core.ConfigDir()
		configPath := filepath.Join(configDir, "roboticus.toml")

		content, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}

		timestamp := time.Now().Format("20060102-150405")
		backupPath := fmt.Sprintf("%s.backup.%s", configPath, timestamp)

		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			return fmt.Errorf("failed to write backup: %w", err)
		}

		fmt.Printf("Config backed up to %s\n", backupPath)
		return nil
	},
}

// readConfigTOML reads the config file as raw bytes and parses into a map.
// It tries the API /api/config/raw first, then falls back to the local file.
func readConfigTOML() (map[string]any, []byte, error) {
	// Try API first.
	data, err := cmdutil.APIGet("/api/config/raw")
	if err == nil {
		if raw, ok := data["raw"].(string); ok {
			var tree map[string]any
			if parseErr := toml.Unmarshal([]byte(raw), &tree); parseErr != nil {
				return nil, nil, fmt.Errorf("failed to parse TOML from API: %w", parseErr)
			}
			return tree, []byte(raw), nil
		}
	}

	// Fall back to local file.
	configPath := filepath.Join(core.ConfigDir(), "roboticus.toml")
	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		return nil, nil, fmt.Errorf("cannot read config (API unreachable, local read failed): %w", readErr)
	}
	var tree map[string]any
	if parseErr := toml.Unmarshal(raw, &tree); parseErr != nil {
		return nil, nil, fmt.Errorf("failed to parse TOML: %w", parseErr)
	}
	return tree, raw, nil
}

// navigatePath walks a dotted path (e.g. "models.primary") through nested maps.
func navigatePath(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var current any = m
	for _, part := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		val, exists := cm[part]
		if !exists {
			return nil, false
		}
		current = val
	}
	return current, true
}

// setNestedValue navigates to the parent map for the given path parts and sets the leaf value.
// Intermediate maps are created as needed.
func setNestedValue(m map[string]any, parts []string, value any) {
	current := m
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]]
		if !ok {
			child := make(map[string]any)
			current[parts[i]] = child
			current = child
			continue
		}
		cm, ok := next.(map[string]any)
		if !ok {
			child := make(map[string]any)
			current[parts[i]] = child
			current = child
			continue
		}
		current = cm
	}
	current[parts[len(parts)-1]] = value
}

// deleteNestedKey navigates to the parent map and deletes the leaf key.
func deleteNestedKey(m map[string]any, parts []string) {
	current := m
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]]
		if !ok {
			return
		}
		cm, ok := next.(map[string]any)
		if !ok {
			return
		}
		current = cm
	}
	delete(current, parts[len(parts)-1])
}

// autoType converts a string value to an appropriate Go type for TOML serialization.
func autoType(s string) any {
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseBool(s); err == nil {
		return v
	}
	return s
}

func init() {
	configCmd.AddCommand(configShowCmd, configGetCmd, configValidateCmd, configLintCmd, configCapabilitiesCmd,
		configSetCmd, configUnsetCmd, configBackupCmd)
}
