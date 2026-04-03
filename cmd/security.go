package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var securityCmd = &cobra.Command{
	Use:   "security",
	Short: "Report security configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("Security Configuration:")
		fmt.Printf("  Workspace Only:        %v\n", cfg.Security.WorkspaceOnly)
		fmt.Printf("  Deny on Empty Allowlist: %v\n", cfg.Security.DenyOnEmptyAllowlist)

		if len(cfg.Security.AllowedPaths) > 0 {
			fmt.Printf("  Allowed Paths:         %d entries\n", len(cfg.Security.AllowedPaths))
			for _, p := range cfg.Security.AllowedPaths {
				fmt.Printf("    - %s\n", p)
			}
		} else {
			fmt.Println("  Allowed Paths:         (none)")
		}

		if len(cfg.Security.ProtectedPaths) > 0 {
			fmt.Printf("  Protected Paths:       %d entries\n", len(cfg.Security.ProtectedPaths))
		} else {
			fmt.Println("  Protected Paths:       (none)")
		}

		fmt.Printf("  Sandbox Enabled:       %v\n", cfg.Sandbox.Enabled)
		fmt.Printf("  Classifier Enabled:    %v\n", cfg.Classifier.Enabled)
		fmt.Printf("  Approvals Enabled:     %v\n", cfg.Approvals.Enabled)

		if cfg.Approvals.Enabled && len(cfg.Approvals.GatedTools) > 0 {
			fmt.Printf("  Gated Tools:           %d\n", len(cfg.Approvals.GatedTools))
		}

		if len(cfg.Approvals.BlockedTools) > 0 {
			fmt.Printf("  Blocked Tools:         %d\n", len(cfg.Approvals.BlockedTools))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(securityCmd)
}
