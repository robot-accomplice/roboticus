package admin

import (
	"roboticus/cmd/internal/cmdutil"
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"roboticus/internal/db"
)

var defragCmd = &cobra.Command{
	Use:   "defrag",
	Short: "Database optimization or code quality scanning",
	Long: `Defrag operates in two modes:

  --mode=db   (default) Database maintenance: integrity check, FTS rebuild,
              stale data pruning, VACUUM.

  --mode=code Code quality scanner: scans source files for legacy naming
              patterns, deprecated function usage, and TODO/FIXME counts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, _ := cmd.Flags().GetString("mode")

		switch mode {
		case "db", "":
			return runDefragDB()
		case "code":
			return runDefragCode()
		default:
			return fmt.Errorf("unknown mode %q (expected 'db' or 'code')", mode)
		}
	},
}

func runDefragDB() error {
	cfg, err := cmdutil.LoadConfig()
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
}

// runDefragCode scans source files for code quality issues matching Rust's
// defrag code-quality scanner: legacy naming, deprecated functions, TODO/FIXME.
func runDefragCode() error {
	fmt.Println("Code quality scan")
	fmt.Println()

	root := "."

	// Legacy naming patterns (snake_case in Go files, old prefixes).
	legacyPatterns := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"snake_case exported func", regexp.MustCompile(`func\s+[A-Z][a-z]+_[A-Za-z]`)},
		{"old 'roboticus_' prefix variable", regexp.MustCompile(`\b(roboticus_[a-z_]+)\b`)},
	}

	// Deprecated function patterns.
	deprecatedPatterns := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"direct p.Run() (use pipeline.RunPipeline)", regexp.MustCompile(`\bp\.Run\(`)},
		{"ioutil (deprecated since Go 1.16)", regexp.MustCompile(`"io/ioutil"`)},
	}

	todoPattern := regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|XXX)\b`)

	var (
		legacyHits     []string
		deprecatedHits []string
		todoCount      int
		fixmeCount     int
		hackCount      int
		filesScanned   int
	)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip test files for legacy naming (test helpers often use snake_case).
		isTest := strings.HasSuffix(path, "_test.go")

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()

		filesScanned++
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			// Legacy naming (skip tests).
			if !isTest {
				for _, lp := range legacyPatterns {
					if lp.pattern.MatchString(line) {
						legacyHits = append(legacyHits, fmt.Sprintf("  %s:%d  %s", path, lineNum, lp.name))
					}
				}
			}

			// Deprecated functions.
			for _, dp := range deprecatedPatterns {
				if dp.pattern.MatchString(line) {
					deprecatedHits = append(deprecatedHits, fmt.Sprintf("  %s:%d  %s", path, lineNum, dp.name))
				}
			}

			// TODO/FIXME/HACK counts.
			matches := todoPattern.FindAllString(line, -1)
			for _, m := range matches {
				switch strings.ToUpper(m) {
				case "TODO":
					todoCount++
				case "FIXME":
					fixmeCount++
				case "HACK", "XXX":
					hackCount++
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk failed: %w", err)
	}

	fmt.Printf("Scanned %d .go files\n\n", filesScanned)

	// Legacy naming.
	fmt.Printf("Legacy naming patterns: %d\n", len(legacyHits))
	for _, h := range legacyHits {
		fmt.Println(h)
	}
	if len(legacyHits) > 0 {
		fmt.Println()
	}

	// Deprecated functions.
	fmt.Printf("Deprecated function usage: %d\n", len(deprecatedHits))
	for _, h := range deprecatedHits {
		fmt.Println(h)
	}
	if len(deprecatedHits) > 0 {
		fmt.Println()
	}

	// TODO/FIXME counts.
	total := todoCount + fixmeCount + hackCount
	fmt.Printf("Annotations: %d total (TODO=%d, FIXME=%d, HACK/XXX=%d)\n",
		total, todoCount, fixmeCount, hackCount)

	fmt.Println()
	if len(legacyHits) == 0 && len(deprecatedHits) == 0 && total == 0 {
		fmt.Println("Codebase is clean.")
	} else {
		fmt.Printf("Found %d issues to address.\n", len(legacyHits)+len(deprecatedHits)+total)
	}

	return nil
}

func init() {
	defragCmd.Flags().String("mode", "db", "Operation mode: 'db' (database maintenance) or 'code' (quality scan)")}
