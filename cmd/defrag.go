package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"goboticus/internal/db"
)

var defragCmd = &cobra.Command{
	Use:   "defrag",
	Short: "Database optimization: VACUUM, FTS rebuild, stale data pruning",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		store, err := db.Open(cfg.Database.Path)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = store.Close() }()

		ctx := context.Background()

		// 1. Integrity check.
		fmt.Print("Running integrity check... ")
		var result string
		row := store.QueryRowContext(ctx, "PRAGMA integrity_check")
		_ = row.Scan(&result)
		if result != "ok" {
			return fmt.Errorf("integrity check failed: %s", result)
		}
		fmt.Println("OK")

		// 2. Rebuild FTS index.
		fmt.Print("Rebuilding FTS index... ")
		_, err = store.ExecContext(ctx, "INSERT INTO memory_fts(memory_fts) VALUES ('rebuild')")
		if err != nil {
			fmt.Printf("skipped (%v)\n", err)
		} else {
			fmt.Println("done")
		}

		// 3. Prune expired leases.
		fmt.Print("Clearing expired leases... ")
		res, _ := store.ExecContext(ctx,
			`UPDATE cron_jobs SET lease_holder = NULL, lease_expires_at = NULL
			 WHERE lease_expires_at IS NOT NULL AND lease_expires_at < datetime('now')`)
		n, _ := res.RowsAffected()
		fmt.Printf("%d cleared\n", n)

		// 4. Prune old abuse events (> 30 days).
		fmt.Print("Pruning old abuse events... ")
		res, _ = store.ExecContext(ctx,
			`DELETE FROM abuse_events WHERE created_at < datetime('now', '-30 days')`)
		n, _ = res.RowsAffected()
		fmt.Printf("%d removed\n", n)

		// 5. VACUUM.
		fmt.Print("Running VACUUM... ")
		_, err = store.ExecContext(ctx, "VACUUM")
		if err != nil {
			return fmt.Errorf("vacuum failed: %w", err)
		}
		fmt.Println("done")

		// 6. Report size.
		var pageCount, pageSize int
		_ = store.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)
		_ = store.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize)
		sizeMB := float64(pageCount*pageSize) / (1024 * 1024)
		fmt.Printf("\nDatabase size: %.2f MB (%d pages)\n", sizeMB, pageCount)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(defragCmd)
}
