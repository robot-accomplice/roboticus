package admin

import (
	"context"
	"fmt"
	"os"
	"roboticus/cmd/internal/cmdutil"
	"roboticus/cmd/updatecmd"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

var mechanicCmd = &cobra.Command{
	Use:     "mechanic",
	Aliases: []string{"doctor"},
	Short:   "Database diagnostics and repair",
	RunE: func(cmd *cobra.Command, args []string) error {
		repair, _ := cmd.Flags().GetBool("repair")

		cfg, err := cmdutil.LoadConfig()
		if err != nil {
			return err
		}

		dbPath := cfg.Database.Path
		if dbPath == "" {
			dbPath = core.DefaultConfig().Database.Path
		}
		jsonOut := viper.GetBool("json")
		report := map[string]any{
			"database_path": dbPath,
			"repair":        repair,
		}

		// Report disk usage.
		if info, statErr := os.Stat(dbPath); statErr == nil {
			report["disk_usage_mb"] = float64(info.Size()) / (1024 * 1024)
			if !jsonOut {
				fmt.Printf("Database path: %s\n", dbPath)
				fmt.Printf("Disk usage:    %.2f MB\n", report["disk_usage_mb"])
			}
		} else {
			report["status"] = "database_not_found"
			if jsonOut {
				cmdutil.PrintJSON(report)
			} else {
				fmt.Printf("Database path: %s (not found)\n", dbPath)
			}
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
			report["schema_version"] = version
			if !jsonOut {
				fmt.Printf("Schema version: %d\n", version)
			}
		}

		// Table row counts.
		if !jsonOut {
			fmt.Println("\nTable Row Counts:")
		}
		tables := []string{"sessions", "turns", "episodic_memory", "semantic_memory"}
		rowCounts := map[string]any{}
		for _, t := range tables {
			var count int
			query := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, t) //nolint:gosec // table names are static constants
			if err := store.QueryRowContext(ctx, query).Scan(&count); err == nil {
				rowCounts[t] = count
				if !jsonOut {
					fmt.Printf("  %-20s %d\n", t, count)
				}
			} else {
				rowCounts[t] = map[string]string{"error": err.Error()}
				if !jsonOut {
					fmt.Printf("  %-20s (error: %v)\n", t, err)
				}
			}
		}
		report["table_counts"] = rowCounts

		// Always run integrity check.
		if !jsonOut {
			fmt.Println("\nIntegrity check:")
		}
		var integrityResult string
		if err := store.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrityResult); err != nil {
			report["integrity_check"] = map[string]string{"status": "failed", "detail": err.Error()}
			if !jsonOut {
				fmt.Printf("  FAIL: %v\n", err)
			}
		} else {
			report["integrity_check"] = map[string]string{"status": integrityResult}
			if !jsonOut {
				fmt.Printf("  %s\n", integrityResult)
			}
		}

		if repair {
			if !jsonOut {
				fmt.Println("\nRunning VACUUM...")
			}
			if _, err := store.ExecContext(ctx, `VACUUM`); err != nil {
				report["vacuum"] = map[string]string{"status": "failed", "detail": err.Error()}
				if !jsonOut {
					fmt.Printf("  VACUUM failed: %v\n", err)
				}
			} else {
				report["vacuum"] = map[string]string{"status": "repaired", "detail": "VACUUM complete"}
				if !jsonOut {
					fmt.Println("  VACUUM complete.")
				}
			}

			repairSummary, cleanupErr := updatecmd.RunInstallCleanup(ctx, cmdutil.EffectiveConfigPath(), cmdutil.Version)
			report["install_cleanup"] = repairSummary
			if cleanupErr != nil {
				report["install_cleanup_error"] = cleanupErr.Error()
			}
			if !jsonOut {
				for _, action := range repairSummary.Actions {
					fmt.Printf("  %-20s %-20s %s\n", action.Name, action.Status, action.Detail)
				}
			}

			orphaned, orphanErr := repairOrphanReactTraces(ctx, store)
			if orphanErr != nil {
				report["orphan_react_traces"] = map[string]any{"status": "failed", "detail": orphanErr.Error()}
			} else if orphaned > 0 {
				report["orphan_react_traces"] = map[string]any{"status": "repaired", "deleted": orphaned}
			} else {
				report["orphan_react_traces"] = map[string]any{"status": "skipped", "deleted": 0}
			}

			staled, deletedFacts, denialErr := repairFalseCapabilityDenialMemories(ctx, store)
			if denialErr != nil {
				report["false_capability_denial_memories"] = map[string]any{"status": "failed", "detail": denialErr.Error()}
			} else if staled > 0 || deletedFacts > 0 {
				report["false_capability_denial_memories"] = map[string]any{
					"status":           "repaired",
					"staled_rows":      staled,
					"deleted_facts":    deletedFacts,
					"repair_rationale": "false capability-denial memories are non-reinforcing audit artifacts",
				}
			} else {
				report["false_capability_denial_memories"] = map[string]any{"status": "skipped", "staled_rows": 0, "deleted_facts": 0}
			}

			indexRows, ftsRows, derivedErr := repairInactiveMemoryDerivedRows(ctx, store)
			if derivedErr != nil {
				report["derived_memory_hygiene"] = map[string]any{"status": "failed", "detail": derivedErr.Error()}
			} else if indexRows > 0 || ftsRows > 0 {
				report["derived_memory_hygiene"] = map[string]any{
					"status":                    "repaired",
					"deleted_memory_index_rows": indexRows,
					"deleted_memory_fts_rows":   ftsRows,
					"repair_rationale":          "derived memory rows must not outlive active source memory state",
				}
			} else {
				report["derived_memory_hygiene"] = map[string]any{
					"status":                    "skipped",
					"deleted_memory_index_rows": 0,
					"deleted_memory_fts_rows":   0,
				}
			}

			deadCount, oldest, deadErr := db.NewRouteQueries(store).DeadLetterSummary(ctx)
			if deadErr != nil {
				report["dead_letters"] = map[string]any{"status": "failed", "detail": deadErr.Error()}
			} else {
				report["dead_letters"] = map[string]any{"status": "skipped", "count": deadCount, "oldest_created_at": oldest}
			}

			// Re-check after repair.
			if !jsonOut {
				fmt.Println("Re-checking integrity...")
			}
			var result string
			if err := store.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
				report["integrity_recheck"] = map[string]string{"status": "failed", "detail": err.Error()}
				if !jsonOut {
					fmt.Printf("  FAIL: %v\n", err)
				}
			} else {
				report["integrity_recheck"] = map[string]string{"status": result}
				if !jsonOut {
					fmt.Printf("  %s\n", result)
				}
			}
		}

		if jsonOut {
			cmdutil.PrintJSON(report)
		}
		return nil
	},
}

func init() {
	mechanicCmd.Flags().Bool("repair", false, "run VACUUM and integrity check")
}

func repairOrphanReactTraces(ctx context.Context, store *db.Store) (int64, error) {
	result, err := store.ExecContext(ctx,
		`DELETE FROM react_traces
		  WHERE pipeline_trace_id NOT IN (SELECT id FROM pipeline_traces)`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func repairFalseCapabilityDenialMemories(ctx context.Context, store *db.Store) (staledRows int64, deletedFacts int64, err error) {
	pattern := `%capability%`
	toolPattern := `%tool%`
	browsePattern := `%browse%`
	playwrightPattern := `%playwright%`

	staleStatements := []string{
		`UPDATE episodic_memory
		    SET memory_state = 'stale',
		        state_reason = 'false capability denial; non-reinforcing'
		  WHERE memory_state = 'active'
		    AND (lower(content) LIKE ? OR lower(content) LIKE ? OR lower(content) LIKE ? OR lower(content) LIKE ?)
		    AND (lower(content) LIKE '%don''t have%' OR lower(content) LIKE '%do not have%' OR lower(content) LIKE '%cannot%' OR lower(content) LIKE '%can''t%' OR lower(content) LIKE '%not available%')`,
		`UPDATE semantic_memory
		    SET memory_state = 'stale',
		        state_reason = 'false capability denial; non-reinforcing'
		  WHERE memory_state = 'active'
		    AND (lower(value) LIKE ? OR lower(key) LIKE ? OR lower(value) LIKE ? OR lower(value) LIKE ? OR lower(value) LIKE ?)
		    AND (lower(value) LIKE '%don''t have%' OR lower(value) LIKE '%do not have%' OR lower(value) LIKE '%cannot%' OR lower(value) LIKE '%can''t%' OR lower(value) LIKE '%not available%')`,
	}

	res, err := store.ExecContext(ctx, staleStatements[0], pattern, toolPattern, browsePattern, playwrightPattern)
	if err != nil {
		return 0, 0, err
	}
	if n, countErr := res.RowsAffected(); countErr == nil {
		staledRows += n
	}

	res, err = store.ExecContext(ctx, staleStatements[1], pattern, pattern, toolPattern, browsePattern, playwrightPattern)
	if err != nil {
		return staledRows, 0, err
	}
	if n, countErr := res.RowsAffected(); countErr == nil {
		staledRows += n
	}

	res, err = store.ExecContext(ctx,
		`DELETE FROM knowledge_facts
		  WHERE (lower(subject) LIKE ? OR lower(object) LIKE ?)
		    AND (lower(object) LIKE ? OR lower(object) LIKE ? OR lower(object) LIKE ?)
		    AND (lower(subject) LIKE '%don''t have%' OR lower(subject) LIKE '%do not have%' OR lower(subject) LIKE '%cannot%' OR lower(subject) LIKE '%can''t%' OR lower(subject) LIKE '%not available%')`,
		pattern, pattern, toolPattern, browsePattern, playwrightPattern)
	if err != nil {
		return staledRows, 0, err
	}
	if n, countErr := res.RowsAffected(); countErr == nil {
		deletedFacts = n
	}
	return staledRows, deletedFacts, nil
}

func repairInactiveMemoryDerivedRows(ctx context.Context, store *db.Store) (indexRows int64, ftsRows int64, err error) {
	indexResult, err := store.ExecContext(ctx,
		`DELETE FROM memory_index
		  WHERE (
		      source_table IN ('semantic_memory', 'semantic')
		      AND NOT EXISTS (
		        SELECT 1 FROM semantic_memory sm
		         WHERE sm.id = memory_index.source_id
		           AND sm.memory_state = 'active'
		      )
		    )
		    OR (
		      source_table IN ('episodic_memory', 'episodic')
		      AND NOT EXISTS (
		        SELECT 1 FROM episodic_memory em
		         WHERE em.id = memory_index.source_id
		           AND em.memory_state IN ('active', 'promoted')
		      )
		    )`)
	if err != nil {
		return 0, 0, err
	}
	if n, countErr := indexResult.RowsAffected(); countErr == nil {
		indexRows = n
	}

	ftsResult, err := store.ExecContext(ctx,
		`DELETE FROM memory_fts
		  WHERE (
		      source_table IN ('semantic_memory', 'semantic')
		      AND NOT EXISTS (
		        SELECT 1 FROM semantic_memory sm
		         WHERE sm.id = memory_fts.source_id
		           AND sm.memory_state = 'active'
		      )
		    )
		    OR (
		      source_table IN ('episodic_memory', 'episodic')
		      AND NOT EXISTS (
		        SELECT 1 FROM episodic_memory em
		         WHERE em.id = memory_fts.source_id
		           AND em.memory_state IN ('active', 'promoted')
		      )
		    )`)
	if err != nil {
		return indexRows, 0, err
	}
	if n, countErr := ftsResult.RowsAffected(); countErr == nil {
		ftsRows = n
	}
	return indexRows, ftsRows, nil
}
