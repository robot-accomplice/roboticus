package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Query available models and routing diagnostics",
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available models",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/models/available")
		if err != nil {
			return err
		}
		models, ok := data["models"].([]any)
		if !ok {
			printJSON(data)
			return nil
		}
		for _, m := range models {
			mm, _ := m.(map[string]any)
			fmt.Printf("  %-30v provider=%-12v context=%v\n",
				mm["id"], mm["provider"], mm["context_window"])
		}
		return nil
	},
}

var modelsDiagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Show model routing diagnostics",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/models/routing-diagnostics")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

func init() {
	modelsCmd.AddCommand(modelsListCmd, modelsDiagnosticsCmd)
	rootCmd.AddCommand(modelsCmd)
}
