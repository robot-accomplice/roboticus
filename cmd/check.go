package cmd

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"goboticus/internal/core"
	"goboticus/internal/db"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validate config, database, and provider connectivity",
	RunE: func(cmd *cobra.Command, args []string) error {
		allOK := true

		// 1. Config validation.
		fmt.Print("Config... ")
		cfg, err := loadConfig()
		if err != nil {
			fmt.Printf("FAIL: %v\n", err)
			allOK = false
		} else {
			fmt.Println("OK")
		}

		// 2. Database check.
		fmt.Print("Database... ")
		dbPath := cfg.Database.Path
		if dbPath == "" {
			dbPath = core.DefaultConfig().Database.Path
		}
		store, err := db.Open(dbPath)
		if err != nil {
			fmt.Printf("FAIL: %v\n", err)
			allOK = false
		} else {
			if err := store.Ping(); err != nil {
				fmt.Printf("FAIL: ping error: %v\n", err)
				allOK = false
			} else {
				fmt.Println("OK")
			}
			_ = store.Close()
		}

		// 3. Primary model provider reachability.
		fmt.Print("Provider... ")
		providerOK := false
		if cfg.Models.Primary != "" {
			for name, prov := range cfg.Providers {
				if prov.URL != "" {
					client := &http.Client{Timeout: 5 * time.Second}
					req, reqErr := http.NewRequest("HEAD", prov.URL, nil)
					if reqErr != nil {
						continue
					}
					resp, respErr := client.Do(req)
					if respErr != nil {
						fmt.Printf("WARN: provider %q at %s unreachable: %v\n", name, prov.URL, respErr)
						continue
					}
					_ = resp.Body.Close()
					fmt.Printf("OK (%s reachable)\n", name)
					providerOK = true
					break
				}
			}
			if !providerOK {
				fmt.Println("SKIP (no provider URLs configured)")
			}
		} else {
			fmt.Println("SKIP (no primary model)")
		}

		if !allOK {
			os.Exit(1)
		}
		fmt.Println("\nAll checks passed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(checkCmd)
}
