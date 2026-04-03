package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"goboticus/internal/db"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset all data (preserves schema)",
	Long:  `Truncates all data tables in the database. Schema version is preserved.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Println("WARNING: This will permanently delete ALL data from the database.")
		fmt.Println("Schema and migrations will be preserved.")
		fmt.Print("Type YES to confirm: ")

		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("aborted")
		}
		if strings.TrimSpace(scanner.Text()) != "YES" {
			fmt.Println("Aborted.")
			return nil
		}

		store, err := db.Open(cfg.Database.Path)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer func() { _ = store.Close() }()

		if err := store.TruncateAllData(); err != nil {
			return fmt.Errorf("reset failed: %w", err)
		}

		fmt.Println("All data has been deleted. Schema preserved.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resetCmd)
}
