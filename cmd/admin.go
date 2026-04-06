package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Administrative operations",
}

var adminRosterCmd = &cobra.Command{
	Use:   "roster",
	Short: "List all agents in the roster",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/roster")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var adminModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available models and providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/models/available")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var adminSubagentsCmd = &cobra.Command{
	Use:   "subagents",
	Short: "List registered sub-agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/subagents")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var adminStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show cost, cache, and efficiency stats",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, endpoint := range []struct{ name, path string }{
			{"Costs", "/api/stats/costs"},
			{"Cache", "/api/stats/cache"},
			{"Efficiency", "/api/stats/efficiency"},
		} {
			data, err := apiGet(endpoint.path)
			if err != nil {
				fmt.Printf("%s: error (%v)\n", endpoint.name, err)
				continue
			}
			fmt.Printf("--- %s ---\n", endpoint.name)
			printJSON(data)
		}
		return nil
	},
}

func init() {
	adminCmd.AddCommand(
		adminRosterCmd, adminModelsCmd, adminSubagentsCmd, adminStatsCmd,
	)
	rootCmd.AddCommand(adminCmd)
}
