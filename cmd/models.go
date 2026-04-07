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

var modelsScanCmd = &cobra.Command{
	Use:   "scan [provider]",
	Short: "Scan for available models with validation",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "/api/models/available?validation_level=scan"
		if len(args) > 0 {
			path += "&provider=" + args[0]
		}
		data, err := apiGet(path)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var modelsExerciseCmd = &cobra.Command{
	Use:   "exercise [model]",
	Short: "Exercise a model with a test prompt to verify connectivity and quality",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{"prompt": "Respond with exactly: OK"}
		if len(args) > 0 {
			body["model"] = args[0]
		}
		data, err := apiPost("/api/models/routing-eval", body)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var modelsSuggestCmd = &cobra.Command{
	Use:   "suggest",
	Short: "Suggest optimal model routing based on current quality and cost data",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/models/routing-diagnostics")
		if err != nil {
			return err
		}
		profiles, ok := data["profiles"].([]any)
		if !ok {
			fmt.Println("No model profiles available.")
			return nil
		}
		fmt.Println("Model routing suggestions based on current metascore data:")
		fmt.Println()
		for _, p := range profiles {
			pm, _ := p.(map[string]any)
			model, _ := pm["model"].(string)
			meta, _ := pm["metascore"].(map[string]any)
			score := 0.0
			if meta != nil {
				score, _ = meta["final_score"].(float64)
			}
			blocked, _ := pm["blocked_by_config"].(bool)
			local, _ := pm["is_local"].(bool)
			status := "available"
			if blocked {
				status = "blocked"
			}
			locality := "cloud"
			if local {
				locality = "local"
			}
			fmt.Printf("  %-35s score=%.3f  %s  %s\n", model, score, locality, status)
		}
		return nil
	},
}

var modelsResetCmd = &cobra.Command{
	Use:   "reset [model]",
	Short: "Reset quality scores for a model (or all models)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if len(args) > 0 {
			body["model"] = args[0]
		}
		data, err := apiPost("/api/models/reset", body)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var modelsBaselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Show baseline routing dataset and quality observations",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/models/routing-dataset")
		if err != nil {
			return err
		}
		dataset, ok := data["dataset"].([]any)
		if !ok || len(dataset) == 0 {
			fmt.Println("No routing baseline data available.")
			return nil
		}
		fmt.Printf("Routing baseline: %d observations\n\n", len(dataset))
		fmt.Printf("  %-35s %-10s %-8s %-10s %s\n", "MODEL", "STRATEGY", "QUALITY", "COST", "LATENCY")
		fmt.Println("  " + "─────────────────────────────────── ────────── ──────── ────────── ───────")
		for _, row := range dataset {
			rm, _ := row.(map[string]any)
			model, _ := rm["selected_model"].(string)
			strategy, _ := rm["strategy"].(string)
			quality, _ := rm["quality"].(float64)
			cost, _ := rm["cost"].(float64)
			latency, _ := rm["latency_ms"].(float64)
			fmt.Printf("  %-35s %-10s %-8.3f $%-9.4f %.0fms\n", model, strategy, quality, cost, latency)
		}
		return nil
	},
}

func init() {
	modelsCmd.AddCommand(modelsListCmd, modelsDiagnosticsCmd, modelsScanCmd,
		modelsExerciseCmd, modelsSuggestCmd, modelsResetCmd, modelsBaselineCmd)
	rootCmd.AddCommand(modelsCmd)
}
