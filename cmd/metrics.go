package cmd

import (
	"github.com/spf13/cobra"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "View cost, cache, and capacity metrics",
}

var metricsCostsCmd = &cobra.Command{
	Use:   "costs",
	Short: "Show LLM usage costs",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/stats/costs")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var metricsCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Show cache hit/miss statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/stats/cache")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var metricsCapacityCmd = &cobra.Command{
	Use:   "capacity",
	Short: "Show system capacity and rate limits",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/stats/capacity")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

func init() {
	metricsCmd.AddCommand(metricsCostsCmd, metricsCacheCmd, metricsCapacityCmd)
	rootCmd.AddCommand(metricsCmd)
}
