package cmd

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"roboticus/internal/db"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run migrations and data import/export",
}

var migrateDBCmd = &cobra.Command{
	Use:   "db",
	Short: "Run database schema migrations",
	RunE:  runMigrate,
}

func runMigrate(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := ensureParentDir(cfg.Database.Path); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	store, err := db.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() { _ = store.Close() }()

	log.Info().Str("path", cfg.Database.Path).Msg("migrations complete")
	return nil
}

var migrateImportCmd = &cobra.Command{
	Use:   "import <SOURCE>",
	Short: "Import data from a Legacy workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("data migration from Legacy workspace is not yet available in the Go implementation")
	},
}

var migrateExportCmd = &cobra.Command{
	Use:   "export <TARGET>",
	Short: "Export data to Legacy format",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("data export to Legacy format is not yet available in the Go implementation")
	},
}

func init() {
	// Default behavior: running `migrate` without subcommand runs DB migrations.
	migrateCmd.RunE = runMigrate

	migrateCmd.AddCommand(migrateDBCmd, migrateImportCmd, migrateExportCmd)
	rootCmd.AddCommand(migrateCmd)
}
