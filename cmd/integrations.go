package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var integrationsCmd = &cobra.Command{
	Use:   "integrations",
	Short: "Manage channel integrations",
}

var integrationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured channel integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/channels/status")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var integrationsTestCmd = &cobra.Command{
	Use:   "test [platform]",
	Short: "Send a test message on a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		platform := args[0]
		data, err := apiPost("/api/channels/"+platform+"/test", nil)
		if err != nil {
			return fmt.Errorf("test failed for %s: %w", platform, err)
		}
		printJSON(data)
		return nil
	},
}

var integrationsHealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Show channel health with pass/fail indicators",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/channels/status")
		if err != nil {
			return err
		}

		channels, ok := data["channels"].([]any)
		if !ok {
			printJSON(data)
			return nil
		}

		for _, ch := range channels {
			cm, _ := ch.(map[string]any)
			name, _ := cm["name"].(string)
			enabled, _ := cm["enabled"].(bool)
			status, _ := cm["status"].(string)

			indicator := "FAIL"
			if enabled && (status == "ok" || status == "connected" || status == "active") {
				indicator = "PASS"
			} else if !enabled {
				indicator = "SKIP"
			}
			fmt.Printf("  [%s] %s  (enabled=%v, status=%v)\n", indicator, name, enabled, status)
		}
		return nil
	},
}

func init() {
	integrationsCmd.AddCommand(integrationsListCmd, integrationsTestCmd, integrationsHealthCmd)
	rootCmd.AddCommand(integrationsCmd)
}
