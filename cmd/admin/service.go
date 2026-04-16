package admin

import (
	"roboticus/cmd/internal/cmdutil"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"roboticus/internal/daemon"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage the roboticus system service",
	Long: `Install, uninstall, start, stop, restart, or check status of the
roboticus system service.

  Linux:   integrates with systemd (creates roboticus.service)
  macOS:   integrates with launchd (creates com.roboticus.agent.plist)
  Windows: integrates with Service Control Manager`,
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register roboticus as a system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
		if err != nil {
			return err
		}
		// Pin service registration to the absolutized config path (see
		// daemon.ServiceInstallConfig + cmdutil.EffectiveConfigPathAbs
		// for the full rationale — tl;dr the service manager's CWD
		// isn't the shell's CWD, so a relative --config would boot the
		// wrong file).
		configPath, err := cmdutil.EffectiveConfigPathAbs()
		if err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		if err := daemon.Install(&cfg, configPath); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		log.Info().Str("config", configPath).Msg("service installed (absolute config path embedded)")
		return nil
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove roboticus from the system service manager",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
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
	Short: "Start the roboticus service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
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
	Short: "Stop the roboticus service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
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
	Short: "Restart the roboticus service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
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
	Short: "Check the roboticus service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cmdutil.LoadConfig()
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

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceStatusCmd)}
