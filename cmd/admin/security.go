package admin

import (
	"roboticus/cmd/internal/cmdutil"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"roboticus/internal/core"
)

var securityCmd = &cobra.Command{
	Use:   "security",
	Short: "Security configuration and auditing",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default to showing security config when run without subcommand.
		return securityShowCmd.RunE(cmd, args)
	},
}

var securityShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Report security configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("Security Configuration:")
		fmt.Printf("  Workspace Only:        %v\n", cfg.Security.IsWorkspaceConfined())
		fmt.Printf("  Deny on Empty Allowlist: %v\n", cfg.Security.Filesystem.DenyOnEmptyAllowlist)

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

var securityAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Run security audit checks on configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		issues := 0

		fmt.Println("Security Audit:")
		fmt.Println("===============")

		// Check: API keys configured (via keystore refs).
		hasAPIKey := false
		for _, p := range cfg.Providers {
			if p.APIKeyRef != "" {
				hasAPIKey = true
			}
		}
		if !hasAPIKey {
			fmt.Println("  [WARN] No provider API keys found in environment")
			issues++
		} else {
			fmt.Println("  [OK]   Provider API keys present")
		}

		// Check: bind address.
		bind := cfg.Server.Bind
		if bind == "" {
			bind = "127.0.0.1"
		}
		ip := net.ParseIP(bind)
		if ip != nil && !ip.IsLoopback() && bind != "0.0.0.0" {
			fmt.Printf("  [OK]   Bind address: %s\n", bind)
		} else if bind == "0.0.0.0" || bind == "::" {
			fmt.Printf("  [WARN] Bind address %s exposes service to all interfaces\n", bind)
			issues++
		} else {
			fmt.Printf("  [OK]   Bind address: %s (loopback)\n", bind)
		}

		// Check: config file permissions.
		configPath := viper.ConfigFileUsed()
		if configPath == "" {
			configPath = strings.Join([]string{os.Getenv("HOME"), ".roboticus", "roboticus.toml"}, "/")
		}
		if info, err := os.Stat(configPath); err == nil {
			perm := info.Mode().Perm()
			if perm&0o077 != 0 {
				fmt.Printf("  [WARN] Config file %s has broad permissions: %o\n", configPath, perm)
				issues++
			} else {
				fmt.Printf("  [OK]   Config file permissions: %o\n", perm)
			}
		}

		// Check: sandbox enabled.
		if cfg.Sandbox.Enabled {
			fmt.Println("  [OK]   Sandbox is enabled")
		} else {
			fmt.Println("  [WARN] Sandbox is disabled")
			issues++
		}

		// Check: approvals.
		if cfg.Approvals.Enabled {
			fmt.Println("  [OK]   Approval workflow is enabled")
		} else {
			fmt.Println("  [INFO] Approval workflow is disabled")
		}

		// Check: wallet file encryption.
		walletPath := cfg.Wallet.Path
		if walletPath == "" {
			walletPath = filepath.Join(core.ConfigDir(), "wallet.json")
		}
		if walletData, readErr := os.ReadFile(walletPath); readErr == nil {
			// If the file parses as plain JSON, it is not encrypted.
			var parsed map[string]any
			if json.Unmarshal(walletData, &parsed) == nil {
				fmt.Printf("  [WARN] Wallet file %s appears to be plaintext JSON (not encrypted)\n", walletPath)
				issues++
			} else {
				fmt.Printf("  [OK]   Wallet file %s appears encrypted\n", walletPath)
			}
		} else if !os.IsNotExist(readErr) {
			fmt.Printf("  [INFO] Could not read wallet file %s: %v\n", walletPath, readErr)
		}

		// Check: bind address + API key combination.
		if (bind == "0.0.0.0" || bind == "::") && !hasAPIKey {
			fmt.Println("  [WARN] Service exposed on all interfaces without API key authentication")
			issues++
		}

		// Check: rate limiting.
		if cfg.RateLimit.Enabled {
			fmt.Printf("  [OK]   Rate limiting enabled (%d req / %d sec window)\n",
				cfg.RateLimit.RequestsPerWindow, cfg.RateLimit.WindowSeconds)
		} else {
			fmt.Println("  [WARN] Rate limiting is disabled")
			issues++
		}

		// Check: sandbox configuration detail.
		if cfg.Sandbox.Enabled {
			detail := ""
			if cfg.Sandbox.MaxMemoryBytes > 0 {
				detail += fmt.Sprintf("max_memory=%dMB", cfg.Sandbox.MaxMemoryBytes/(1024*1024))
			}
			if len(cfg.Sandbox.AllowedPaths) > 0 {
				if detail != "" {
					detail += ", "
				}
				detail += fmt.Sprintf("allowed_paths=%d", len(cfg.Sandbox.AllowedPaths))
			}
			if detail != "" {
				fmt.Printf("  [OK]   Sandbox details: %s\n", detail)
			}
		}

		// Check: DKIM configuration.
		if cfg.DKIM.Enabled {
			fmt.Println("  [OK]   DKIM verification is enabled")
			if !cfg.DKIM.RequireValid {
				fmt.Println("  [WARN] DKIM is enabled but does not require valid signatures")
				issues++
			}
		} else {
			fmt.Println("  [INFO] DKIM verification is disabled")
		}

		fmt.Printf("\n%d issue(s) found.\n", issues)
		return nil
	},
}

func init() {
	securityCmd.AddCommand(securityShowCmd, securityAuditCmd)}
