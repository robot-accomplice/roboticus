package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var keystoreCmd = &cobra.Command{
	Use:   "keystore",
	Short: "Manage the encrypted keystore",
}

var keystoreStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show keystore lock/unlock status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Check if any providers have API key env vars that resolve.
		hasKeys := false
		for name, prov := range cfg.Providers {
			if prov.APIKeyEnv != "" {
				hasKeys = true
				fmt.Printf("  %-20s key env: %s\n", name, prov.APIKeyEnv)
			}
		}

		if hasKeys {
			fmt.Println("\nKeystore status: accessible (provider keys configured)")
		} else {
			fmt.Println("\nKeystore status: no keys configured")
		}

		return nil
	},
}

var keystoreListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored key names (not values)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("Stored Key Names:")
		count := 0
		for name, prov := range cfg.Providers {
			if prov.APIKeyEnv != "" {
				fmt.Printf("  %s (env: %s)\n", name, prov.APIKeyEnv)
				count++
			}
		}

		if count == 0 {
			fmt.Println("  (none)")
		}

		return nil
	},
}

func init() {
	keystoreCmd.AddCommand(keystoreStatusCmd)
	keystoreCmd.AddCommand(keystoreListCmd)
	rootCmd.AddCommand(keystoreCmd)
}
