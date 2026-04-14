package admin

import (
	"roboticus/cmd/internal/cmdutil"
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
		data, err := cmdutil.APIGet("/api/stats/costs")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var metricsCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Show cache hit/miss statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/stats/cache")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var metricsCapacityCmd = &cobra.Command{
	Use:   "capacity",
	Short: "Show system capacity and rate limits",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/stats/capacity")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var metricsTransactionsCmd = &cobra.Command{
	Use:   "transactions",
	Short: "Show transaction metrics",
	RunE: func(cmd *cobra.Command, args []string) error {
		hours, _ := cmd.Flags().GetString("hours")
		if hours == "" {
			hours = "24"
		}
		data, err := cmdutil.APIGet("/api/stats/transactions?hours=" + hours)
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

func init() {
	metricsTransactionsCmd.Flags().String("hours", "24", "number of hours to look back")
	metricsCmd.AddCommand(metricsCostsCmd, metricsCacheCmd, metricsCapacityCmd, metricsTransactionsCmd)}
