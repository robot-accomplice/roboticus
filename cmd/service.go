package cmd

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"goboticus/internal/daemon"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage the goboticus system service",
	Long: `Install, uninstall, start, stop, restart, or check status of the
goboticus system service.

  Linux:   integrates with systemd (creates goboticus.service)
  macOS:   integrates with launchd (creates com.goboticus.agent.plist)
  Windows: integrates with Service Control Manager`,
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register goboticus as a system service",
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

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove goboticus from the system service manager",
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

var serviceStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the goboticus service",
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

var serviceStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the goboticus service",
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

var serviceRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the goboticus service",
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

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the goboticus service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		status, err := daemon.Status(&cfg)
		if err != nil {
			return fmt.Errorf("status check failed: %w", err)
		}
		fmt.Printf("goboticus service: %s\n", status)
		return nil
	},
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	rootCmd.AddCommand(serviceCmd)
}
