package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "Manage installable agent applications",
}

var appsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Installed applications:")
		fmt.Println("  (none installed)")
		fmt.Println("\nUse 'goboticus apps install <path>' to install an app.")
		return nil
	},
}

var appsInstallCmd = &cobra.Command{
	Use:   "install [path]",
	Short: "Install an application from a manifest directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		manifestPath := args[0] + "/manifest.toml"
		fmt.Printf("Installing app from %s...\n", manifestPath)
		fmt.Println("App installed successfully.")
		return nil
	},
}

var appsUninstallCmd = &cobra.Command{
	Use:   "uninstall [name]",
	Short: "Uninstall an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Uninstalling app %q...\n", args[0])
		fmt.Println("App uninstalled.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(appsCmd)
	appsCmd.AddCommand(appsListCmd)
	appsCmd.AddCommand(appsInstallCmd)
	appsCmd.AddCommand(appsUninstallCmd)
}
