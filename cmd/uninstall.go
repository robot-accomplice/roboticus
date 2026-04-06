package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"roboticus/internal/core"
	"roboticus/internal/daemon"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the roboticus service and optionally remove data",
	RunE: func(cmd *cobra.Command, args []string) error {
		purge, _ := cmd.Flags().GetBool("purge")
		yes, _ := cmd.Flags().GetBool("yes")

		if !yes {
			msg := "Uninstall the roboticus service?"
			if purge {
				msg = "Uninstall the roboticus service and REMOVE all data in ~/.roboticus/?"
			}
			fmt.Printf("%s [y/N] ", msg)
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}
		}

		cfg, err := loadConfig()
		if err != nil {
			log.Warn().Err(err).Msg("failed to load config, proceeding with defaults")
			cfg = core.DefaultConfig()
		}

		if err := daemon.Uninstall(&cfg); err != nil {
			return fmt.Errorf("service uninstall failed: %w", err)
		}
		fmt.Println("Service uninstalled.")

		if purge {
			configDir := core.ConfigDir()
			fmt.Printf("Removing %s...\n", configDir)
			if err := os.RemoveAll(configDir); err != nil {
				return fmt.Errorf("failed to remove data directory: %w", err)
			}
			fmt.Println("Data directory removed.")
		}

		return nil
	},
}

func init() {
	uninstallCmd.Flags().Bool("purge", false, "also remove ~/.roboticus/ directory")
	uninstallCmd.Flags().Bool("yes", false, "skip confirmation prompt")
	rootCmd.AddCommand(uninstallCmd)
}
