package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		data, err := apiGet("/api/config")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Get a specific config value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		val := viper.Get(args[0])
		if val == nil {
			return fmt.Errorf("key %q not found", args[0])
		}
		fmt.Printf("%s = %v\n", args[0], val)
		return nil
	},
}

var configValidateCmd = &cobra.Command{
	Use:     "validate",
	Aliases: []string{"lint"},
	Short:   "Validate configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := loadConfig()
		if err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
		fmt.Println("Configuration is valid.")
		return nil
	},
}

var configCapabilitiesCmd = &cobra.Command{
	Use:   "capabilities",
	Short: "List available capabilities",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/config/capabilities")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set [path] [value]",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		data, err := apiGet("/api/config/raw")
		if err != nil {
			return err
		}

		raw, _ := data["raw"].(string)
		// Simple line-based replacement: look for key = ... and replace value.
		// Key path uses dots, TOML uses sections; do a simple string approach.
		replaced := false
		lines := strings.Split(raw, "\n")
		leafKey := key
		if idx := strings.LastIndex(key, "."); idx >= 0 {
			leafKey = key[idx+1:]
		}
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, leafKey+" ") || strings.HasPrefix(trimmed, leafKey+"=") {
				lines[i] = fmt.Sprintf("%s = %q", leafKey, value)
				replaced = true
				break
			}
		}
		if !replaced {
			lines = append(lines, fmt.Sprintf("%s = %q", leafKey, value))
		}

		newRaw := strings.Join(lines, "\n")
		_, err = apiPut("/api/config/raw", map[string]any{"raw": newRaw})
		if err != nil {
			return err
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
		data, err := apiGet("/api/config/raw")
		if err != nil {
			return err
		}

		raw, _ := data["raw"].(string)
		leafKey := key
		if idx := strings.LastIndex(key, "."); idx >= 0 {
			leafKey = key[idx+1:]
		}

		lines := strings.Split(raw, "\n")
		var out []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, leafKey+" ") || strings.HasPrefix(trimmed, leafKey+"=") {
				continue
			}
			out = append(out, line)
		}

		newRaw := strings.Join(out, "\n")
		_, err = apiPut("/api/config/raw", map[string]any{"raw": newRaw})
		if err != nil {
			return err
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

func init() {
	configCmd.AddCommand(configShowCmd, configGetCmd, configValidateCmd, configCapabilitiesCmd,
		configSetCmd, configUnsetCmd, configBackupCmd)
	rootCmd.AddCommand(configCmd)
}
