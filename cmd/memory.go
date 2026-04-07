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
	Short: "Show memory tier statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, tier := range []string{"working", "episodic", "semantic"} {
			data, err := apiGet("/api/memory/" + tier)
			if err != nil {
				continue
			}
			if entries, ok := data["entries"].([]any); ok {
				fmt.Printf("%-10s %d entries\n", tier, len(entries))
			} else if entries, ok := data["memories"].([]any); ok {
				fmt.Printf("%-10s %d entries\n", tier, len(entries))
			} else {
				fmt.Printf("%-10s (no entries key)\n", tier)
			}
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
