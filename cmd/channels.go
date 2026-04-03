package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var channelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "Manage channel adapters and dead-letter queue",
}

var channelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channel adapter status",
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
		for _, c := range channels {
			cm, _ := c.(map[string]any)
			fmt.Printf("  %-15v status=%-8v messages=%v\n",
				cm["platform"], cm["status"], cm["message_count"])
		}
		return nil
	},
}

var channelsTestCmd = &cobra.Command{
	Use:   "test [platform]",
	Short: "Send a test message through a channel adapter",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/channels/"+args[0]+"/test", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Test message sent through %q channel.\n", args[0])
		printJSON(data)
		return nil
	},
}

var channelsDeadLetterCmd = &cobra.Command{
	Use:   "dead-letter",
	Short: "View dead-letter queue entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/channels/dead-letter")
		if err != nil {
			return err
		}
		entries, ok := data["entries"].([]any)
		if !ok {
			printJSON(data)
			return nil
		}
		if len(entries) == 0 {
			fmt.Println("Dead-letter queue is empty.")
			return nil
		}
		for _, e := range entries {
			em, _ := e.(map[string]any)
			fmt.Printf("  [%v] platform=%v  error=%v\n",
				em["timestamp"], em["platform"], em["error"])
		}
		return nil
	},
}

func init() {
	channelsCmd.AddCommand(channelsListCmd, channelsTestCmd, channelsDeadLetterCmd)
	rootCmd.AddCommand(channelsCmd)
}
