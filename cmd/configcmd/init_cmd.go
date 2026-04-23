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

		// Create standard subdirectories in the config dir. "workspace"
		// is included here as the default placement, but the ACTUAL
		// workspace that init creates below (after writing or reading
		// the config) may point elsewhere — see workspace handling
		// below the config-write block.
		subdirs := []string{"skills", "plugins", "data", "workspace"}
		for _, sub := range subdirs {
			dir := filepath.Join(configDir, sub)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return fmt.Errorf("failed to create %s: %w", dir, err)
			}
			fmt.Printf("  created %s\n", dir)
		}

		// Write default config file if it doesn't exist.
		configPath := core.ConfigFilePath()
		configNewlyWritten := false
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if err := os.WriteFile(configPath, []byte(defaultConfigTOML), 0o600); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}
			fmt.Printf("  created %s\n", configPath)
			configNewlyWritten = true
		} else {
			fmt.Printf("  config already exists: %s\n", configPath)
		}

		// v1.0.6 self-audit P2-K: ensure the workspace dir the config
		// REFERENCES actually exists on disk, not just the default
		// `<configDir>/workspace`. If the operator hand-edited
		// workspace to `/opt/agents/workspace` before re-running init,
		// pre-fix init would happily (re)create `<configDir>/workspace`
		// and silently leave `/opt/agents/workspace` missing. Now: load
		// the effective config (which NormalizePaths expands ~ in) and
		// mkdir whatever workspace the config actually points at.
		//
		// Only runs when the config was already on disk — a fresh
		// default-toml install is covered by the subdirs loop above.
		if !configNewlyWritten {
			effective, err := core.LoadConfigFromFile(configPath)
			if err == nil && effective.Agent.Workspace != "" {
				ws := effective.Agent.Workspace
				// Skip if it's the same path the subdirs loop already
				// created (avoids noise on the common default case).
				defaultWs := filepath.Join(configDir, "workspace")
				if ws != defaultWs {
					if err := os.MkdirAll(ws, 0o700); err != nil {
						// Non-fatal: init's job is primarily to set up
						// the default dir. A missing custom workspace
						// dir can be created later by setup.go's
						// personality flow. Log rather than fail.
						fmt.Printf("  warning: could not create configured workspace %s: %v\n", ws, err)
					} else {
						fmt.Printf("  created %s (from config)\n", ws)
					}
				}
			}
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
bind = "localhost"

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
