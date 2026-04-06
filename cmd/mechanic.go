package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

var mechanicCmd = &cobra.Command{
	Use:     "mechanic",
	Aliases: []string{"doctor"},
	Short:   "Database diagnostics and repair",
	RunE: func(cmd *cobra.Command, args []string) error {
		repair, _ := cmd.Flags().GetBool("repair")

		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		dbPath := cfg.Database.Path
		if dbPath == "" {
			dbPath = core.DefaultConfig().Database.Path
		}

		// Report disk usage.
		if info, statErr := os.Stat(dbPath); statErr == nil {
			fmt.Printf("Database path: %s\n", dbPath)
			fmt.Printf("Disk usage:    %.2f MB\n", float64(info.Size())/(1024*1024))
		} else {
			fmt.Printf("Database path: %s (not found)\n", dbPath)
			return nil
		}

		store, err := db.Open(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer func() { _ = store.Close() }()

		ctx := context.Background()

		// Schema version.
		var version int
		if err := store.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version); err == nil {
			fmt.Printf("Schema version: %d\n", version)
		}

		// Table row counts.
		fmt.Println("\nTable Row Counts:")
		tables := []string{"sessions", "turns", "episodic_memory", "semantic_memory"}
		for _, t := range tables {
			var count int
			query := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, t) //nolint:gosec // table names are static constants
			if err := store.QueryRowContext(ctx, query).Scan(&count); err == nil {
				fmt.Printf("  %-20s %d\n", t, count)
			} else {
				fmt.Printf("  %-20s (error: %v)\n", t, err)
			}
		}

		if repair {
			fmt.Println("\nRunning VACUUM...")
			if _, err := store.ExecContext(ctx, `VACUUM`); err != nil {
				fmt.Printf("  VACUUM failed: %v\n", err)
			} else {
				fmt.Println("  VACUUM complete.")
			}

			fmt.Println("Running integrity check...")
			var result string
			if err := store.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
				fmt.Printf("  Integrity check failed: %v\n", err)
			} else {
				fmt.Printf("  Result: %s\n", result)
			}
		}

		return nil
	},
}

func init() {
	mechanicCmd.Flags().Bool("repair", false, "run VACUUM and integrity check")
	rootCmd.AddCommand(mechanicCmd)
}
