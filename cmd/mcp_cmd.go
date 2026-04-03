package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP (Model Context Protocol) connections",
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active MCP connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/mcp/connections")
		if err != nil {
			return err
		}
		connections, ok := data["connections"].([]any)
		if !ok {
			printJSON(data)
			return nil
		}
		if len(connections) == 0 {
			fmt.Println("No MCP connections.")
			return nil
		}
		for _, c := range connections {
			cm, _ := c.(map[string]any)
			fmt.Printf("  %-20v status=%v  tools=%v\n",
				cm["name"], cm["status"], cm["tools_count"])
		}
		return nil
	},
}

var mcpConnectCmd = &cobra.Command{
	Use:   "connect [name]",
	Short: "Connect to an MCP server by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/mcp/connect", map[string]any{
			"name": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Printf("Connected to MCP server %q.\n", args[0])
		printJSON(data)
		return nil
	},
}

var mcpDisconnectCmd = &cobra.Command{
	Use:   "disconnect [name]",
	Short: "Disconnect from an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/mcp/disconnect/"+args[0], nil)
		if err != nil {
			return err
		}
		fmt.Printf("Disconnected from MCP server %q.\n", args[0])
		if data != nil {
			printJSON(data)
		}
		return nil
	},
}

func init() {
	mcpCmd.AddCommand(mcpListCmd, mcpConnectCmd, mcpDisconnectCmd)
	rootCmd.AddCommand(mcpCmd)
}
