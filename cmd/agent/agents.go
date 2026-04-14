package agent

import (
	"roboticus/cmd/internal/cmdutil"
	"fmt"

	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage agents",
}

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/agents")
		if err != nil {
			return err
		}
		agents, ok := data["agents"].([]any)
		if !ok {
			cmdutil.PrintJSON(data)
			return nil
		}
		if len(agents) == 0 {
			fmt.Println("No agents.")
			return nil
		}
		for _, a := range agents {
			am, _ := a.(map[string]any)
			fmt.Printf("  %-20v status=%-10v model=%v\n",
				am["id"], am["status"], am["model"])
		}
		return nil
	},
}

var agentsStartCmd = &cobra.Command{
	Use:   "start [id]",
	Short: "Start an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIPost("/api/agents/"+args[0]+"/start", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Agent %s started.\n", args[0])
		cmdutil.PrintJSON(data)
		return nil
	},
}

var agentsStopCmd = &cobra.Command{
	Use:   "stop [id]",
	Short: "Stop an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIPost("/api/agents/"+args[0]+"/stop", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Agent %s stopped.\n", args[0])
		cmdutil.PrintJSON(data)
		return nil
	},
}

func init() {
	agentsCmd.AddCommand(agentsListCmd, agentsStartCmd, agentsStopCmd)}
