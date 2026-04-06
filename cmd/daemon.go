package cmd

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"roboticus/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the roboticus daemon (alias for service)",
}

var daemonInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register roboticus as a system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := daemon.Install(&cfg); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		log.Info().Msg("service installed")
		return nil
	},
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the roboticus service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := daemon.Control(&cfg, "start"); err != nil {
			return fmt.Errorf("start failed: %w", err)
		}
		log.Info().Msg("service started")
		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the roboticus service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := daemon.Control(&cfg, "stop"); err != nil {
			return fmt.Errorf("stop failed: %w", err)
		}
		log.Info().Msg("service stopped")
		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the roboticus service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := daemon.Control(&cfg, "restart"); err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}
		log.Info().Msg("service restarted")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the roboticus service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		status, err := daemon.Status(&cfg)
		if err != nil {
			return fmt.Errorf("status check failed: %w", err)
		}
		fmt.Printf("roboticus service: %s\n", status)
		return nil
	},
}

var daemonUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove roboticus from the system service manager",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := daemon.Uninstall(&cfg); err != nil {
			return fmt.Errorf("uninstall failed: %w", err)
		}
		log.Info().Msg("service uninstalled")
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonInstallCmd, daemonStartCmd, daemonStopCmd,
		daemonRestartCmd, daemonStatusCmd, daemonUninstallCmd)
	rootCmd.AddCommand(daemonCmd)
}
