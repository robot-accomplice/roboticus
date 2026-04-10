package cmd

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"roboticus/internal/daemon"
)

var serveCmd = &cobra.Command{
	Use:     "serve",
	Aliases: []string{"start", "run"},
	Short:   "Start the roboticus agent runtime",
	Long: `Start the roboticus daemon in the foreground.

On Linux this integrates with systemd, on macOS with launchd,
and on Windows with the Service Control Manager.
Use "roboticus service install" to register as a system service.`,
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

	// Resolve display flags for the boot sequence.
	colorFlag, _ := cmd.Flags().GetString("color")
	themeFlag, _ := cmd.Flags().GetString("theme")
	nerdmode, _ := cmd.Flags().GetBool("nerdmode")
	noDraw, _ := cmd.Flags().GetBool("no-draw")

	d, err := daemon.New(&cfg, daemon.BootOptions{
		ColorMode: colorFlag,
		Theme:     themeFlag,
		NerdMode:  nerdmode,
		NoDraw:    noDraw,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize daemon: %w", err)
	}

	log.Info().Msg("starting roboticus in interactive mode")
	return d.RunInteractive()
}
