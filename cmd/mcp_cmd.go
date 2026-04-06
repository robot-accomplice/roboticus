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

var mcpShowCmd = &cobra.Command{
	Use:   "show <NAME>",
	Short: "Show tools and details for an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		data, err := apiGet("/api/mcp/tools")
		if err != nil {
			return err
		}

		// Filter tools by server name.
		if tools, ok := data["tools"].([]any); ok {
			var matched []any
			for _, t := range tools {
				tm, _ := t.(map[string]any)
				if tm["server"] == name || tm["server_name"] == name {
					matched = append(matched, tm)
				}
			}
			if len(matched) > 0 {
				fmt.Printf("MCP server %q — %d tool(s):\n", name, len(matched))
				printJSON(matched)
				return nil
			}
		}

		// If no tools matched, try showing the raw response filtered differently.
		if servers, ok := data["servers"].([]any); ok {
			for _, s := range servers {
				sm, _ := s.(map[string]any)
				if sm["name"] == name {
					printJSON(sm)
					return nil
				}
			}
		}

		fmt.Printf("MCP server %q not found or has no tools.\n", name)
		return nil
	},
}

var mcpTestCmd = &cobra.Command{
	Use:   "test <NAME>",
	Short: "Test connectivity to an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		fmt.Printf("Testing MCP server %q...\n", name)

		// Step 1: Connect.
		_, err := apiPost("/api/mcp/connect", map[string]any{
			"name": name,
		})
		if err != nil {
			fmt.Printf("  FAIL: connection failed: %v\n", err)
			return nil
		}
		fmt.Println("  connected")

		// Step 2: Check tools.
		data, err := apiGet("/api/mcp/tools")
		if err != nil {
			fmt.Printf("  WARN: could not list tools: %v\n", err)
		} else {
			toolCount := 0
			if tools, ok := data["tools"].([]any); ok {
				for _, t := range tools {
					tm, _ := t.(map[string]any)
					if tm["server"] == name || tm["server_name"] == name {
						toolCount++
					}
				}
			}
			fmt.Printf("  tools available: %d\n", toolCount)
		}

		// Step 3: Disconnect.
		_, _ = apiPost("/api/mcp/disconnect/"+name, nil)
		fmt.Println("  disconnected")
		fmt.Printf("MCP server %q: OK\n", name)
		return nil
	},
}

func init() {
	mcpCmd.AddCommand(mcpListCmd, mcpConnectCmd, mcpDisconnectCmd, mcpShowCmd, mcpTestCmd)
	rootCmd.AddCommand(mcpCmd)
}
