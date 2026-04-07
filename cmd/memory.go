package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Query and manage memory tiers",
}

var memoryWorkingCmd = &cobra.Command{
	Use:   "working",
	Short: "List working memory entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "/api/memory/working"
		sessionID, _ := cmd.Flags().GetString("session")
		if sessionID != "" {
			path += "?session_id=" + sessionID
		}
		data, err := apiGet(path)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var memoryEpisodicCmd = &cobra.Command{
	Use:   "episodic",
	Short: "List episodic memory entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/memory/episodic")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var memorySemanticCmd = &cobra.Command{
	Use:   "semantic",
	Short: "List semantic memory entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		category, _ := cmd.Flags().GetString("category")
		path := "/api/memory/semantic"
		if category != "" {
			path = "/api/memory/semantic/" + category
		}
		data, err := apiGet(path)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var memorySearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search across all memory tiers",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet(fmt.Sprintf("/api/memory/search?q=%s", args[0]))
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var memoryStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show memory tier statistics and health",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Fetch analytics from the dedicated endpoint.
		data, err := apiGet("/api/stats/memory-analytics")
		if err != nil {
			return err
		}

		fmt.Println("Memory Statistics:")
		fmt.Println()

		// Entry counts by tier.
		if counts, ok := data["entry_counts"].(map[string]any); ok {
			fmt.Println("  Tier           Entries")
			fmt.Println("  ─────────────  ───────")
			for _, tier := range []string{"working", "episodic", "semantic", "procedural", "relationship"} {
				count := 0
				if v, ok := counts[tier].(float64); ok {
					count = int(v)
				}
				fmt.Printf("  %-15s %d\n", tier, count)
			}
		}

		fmt.Println()

		// Retrieval stats.
		if hits, ok := data["retrieval_hits"].(float64); ok {
			fmt.Printf("  Retrieval hits:     %.0f\n", hits)
		}
		if rate, ok := data["hit_rate"].(float64); ok {
			fmt.Printf("  Hit rate:           %.1f%%\n", rate*100)
		}
		if roi, ok := data["memory_roi"].(float64); ok {
			fmt.Printf("  Memory ROI:         %.2f\n", roi)
		}
		if util, ok := data["avg_budget_utilization"].(float64); ok {
			fmt.Printf("  Budget utilization: %.1f%%\n", util*100)
		}
		if turns, ok := data["total_turns"].(float64); ok {
			fmt.Printf("  Total turns:        %.0f\n", turns)
		}

		return nil
	},
}

var memoryConsolidateCmd = &cobra.Command{
	Use:   "consolidate",
	Short: "Run memory consolidation (dedup, decay, prune)",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/memory/consolidate", nil)
		if err != nil {
			return err
		}
		report, _ := data["report"].(map[string]any)
		if report == nil {
			printJSON(data)
			return nil
		}
		fmt.Printf("Memory consolidation complete:\n")
		for k, v := range report {
			fmt.Printf("  %-20s %v\n", k, v)
		}
		return nil
	},
}

var memoryReindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild memory search index",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/memory/reindex", nil)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

func init() {
	memoryWorkingCmd.Flags().String("session", "", "Filter by session ID")
	memorySemanticCmd.Flags().String("category", "", "Filter by category")

	memoryCmd.AddCommand(memoryWorkingCmd, memoryEpisodicCmd, memorySemanticCmd,
		memorySearchCmd, memoryStatsCmd, memoryConsolidateCmd, memoryReindexCmd)
	rootCmd.AddCommand(memoryCmd)
}
