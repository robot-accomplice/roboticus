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
		fmt.Println("Channels:")
		for _, c := range channels {
			cm, _ := c.(map[string]any)
			platform, _ := cm["platform"].(string)
			status, _ := cm["status"].(string)
			fmt.Printf("  %-15s %s\n", platform, status)
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

var channelsReplayCmd = &cobra.Command{
	Use:   "replay [id]",
	Short: "Replay a dead-letter queue entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/channels/dead-letter/"+args[0]+"/replay", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Replayed dead-letter entry %s\n", args[0])
		printJSON(data)
		return nil
	},
}

func init() {
	channelsCmd.AddCommand(channelsListCmd, channelsTestCmd, channelsDeadLetterCmd, channelsReplayCmd)
	rootCmd.AddCommand(channelsCmd)
}
