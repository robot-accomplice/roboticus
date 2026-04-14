package models

import (
	"roboticus/cmd/internal/cmdutil"
	"fmt"

	"github.com/spf13/cobra"
)

var circuitCmd = &cobra.Command{
	Use:   "circuit",
	Short: "Manage circuit breaker state for LLM providers",
}

var circuitStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show circuit breaker status for all providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/breaker/status")
		if err != nil {
			return err
		}
		breakers, ok := data["breakers"].([]any)
		if !ok {
			cmdutil.PrintJSON(data)
			return nil
		}
		for _, b := range breakers {
			bm, _ := b.(map[string]any)
			fmt.Printf("%-20v state=%-8v failures=%v\n",
				bm["provider"], bm["state"], bm["failures"])
		}
		return nil
	},
}

var circuitResetCmd = &cobra.Command{
	Use:   "reset [provider]",
	Short: "Reset the circuit breaker for a provider",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIPost("/api/breaker/reset/"+args[0], nil)
		if err != nil {
			return err
		}
		fmt.Printf("Circuit breaker for %q reset.\n", args[0])
		if data != nil {
			cmdutil.PrintJSON(data)
		}
		return nil
	},
}

func init() {
	circuitCmd.AddCommand(circuitStatusCmd, circuitResetCmd)}
