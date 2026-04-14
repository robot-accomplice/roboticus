package configcmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"roboticus/internal/core"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new roboticus workspace",
	Long:  `Creates the ~/.roboticus/ directory, a default config file, and standard subdirectories.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configDir := core.ConfigDir()

		// Create the main config directory.
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			return fmt.Errorf("failed to create %s: %w", configDir, err)
		}
		fmt.Printf("  created %s\n", configDir)

		// Create standard subdirectories.
		subdirs := []string{"skills", "plugins", "data"}
		for _, sub := range subdirs {
			dir := filepath.Join(configDir, sub)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return fmt.Errorf("failed to create %s: %w", dir, err)
			}
			fmt.Printf("  created %s\n", dir)
		}

		// Write default config file if it doesn't exist.
		configPath := core.ConfigFilePath()
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if err := os.WriteFile(configPath, []byte(defaultConfigTOML), 0o600); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}
			fmt.Printf("  created %s\n", configPath)
		} else {
			fmt.Printf("  config already exists: %s\n", configPath)
		}

		fmt.Println("\nroboticus workspace initialized successfully.")
		return nil
	},
}

const defaultConfigTOML = `# Roboticus configuration file

[agent]
name = "roboticus"
workspace = "~/.roboticus/workspace"
autonomy_max_react_turns = 10
autonomy_max_turn_duration_seconds = 90

[server]
port = 18789
bind = "127.0.0.1"

[database]
path = "~/.roboticus/roboticus.db"

[models]
primary = "claude-sonnet-4-20250514"

[models.routing]
mode = "primary"
confidence_threshold = 0.9

[memory]
working_budget = 40
episodic_budget = 25
semantic_budget = 15
procedural_budget = 10
relationship_budget = 10

[cache]
ttl_seconds = 3600
similarity_threshold = 0.85

[treasury]
daily_cap = 5.0
per_payment_cap = 1.0
transfer_limit = 1.0

[security]
workspace_only = true
deny_on_empty_allowlist = true

[session]
scope_mode = "agent"
`
