package cmd

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"goboticus/internal/daemon"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the goboticus agent runtime",
	Long: `Start the goboticus daemon in the foreground.

On Linux this integrates with systemd, on macOS with launchd,
and on Windows with the Service Control Manager.
Use "goboticus service install" to register as a system service.`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := ensureParentDir(cfg.Database.Path); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	d, err := daemon.New(&cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize daemon: %w", err)
	}

	log.Info().Msg("starting goboticus in interactive mode")
	return d.RunInteractive()
}
