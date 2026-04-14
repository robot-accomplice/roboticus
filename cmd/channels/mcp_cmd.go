package channels

import (
	"roboticus/cmd/internal/cmdutil"
	"fmt"

	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP (Model Context Protocol) servers",
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured MCP servers",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/mcp/servers")
		if err != nil {
			return err
		}
		servers, ok := data["servers"].([]any)
		if !ok {
			cmdutil.PrintJSON(data)
			return nil
		}
		if len(servers) == 0 {
			fmt.Println("No MCP servers configured.")
			return nil
		}
		for _, s := range servers {
			sm, _ := s.(map[string]any)
			name, _ := sm["name"].(string)
			enabled, _ := sm["enabled"].(bool)
			connected, _ := sm["connected"].(bool)
			toolCount := sm["tool_count"]
			if toolCount == nil {
				toolCount = sm["tools_count"]
			}
			fmt.Printf("  %-20s  enabled=%-5t  connected=%-5t  tools=%v\n",
				name, enabled, connected, toolCount)
		}
		return nil
	},
}

var mcpConnectCmd = &cobra.Command{
	Use:   "connect [name]",
	Short: "Connect to an MCP server by name (runtime-only, does not persist across restarts)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIPost("/api/mcp/connect", map[string]any{
			"name": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Printf("Connected to MCP server %q.\n", args[0])
		cmdutil.PrintJSON(data)
		return nil
	},
}

var mcpDisconnectCmd = &cobra.Command{
	Use:   "disconnect [name]",
	Short: "Disconnect from an MCP server (runtime-only, does not persist across restarts)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIPost("/api/mcp/disconnect/"+args[0], nil)
		if err != nil {
			return err
		}
		fmt.Printf("Disconnected from MCP server %q.\n", args[0])
		if data != nil {
			cmdutil.PrintJSON(data)
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
		data, err := cmdutil.APIGet("/api/mcp/servers/" + name)
		if err != nil {
			return fmt.Errorf("MCP server %q not found or unavailable: %w", name, err)
		}
		cmdutil.PrintJSON(data)
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

		data, err := cmdutil.APIPost("/api/mcp/servers/"+name+"/test", nil)
		if err != nil {
			fmt.Printf("  FAIL: %v\n", err)
			return nil
		}

		if ok, _ := data["ok"].(bool); ok {
			fmt.Printf("MCP server %q: OK\n", name)
		} else {
			fmt.Printf("MCP server %q: test returned unexpected result\n", name)
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

func init() {
	mcpCmd.AddCommand(mcpListCmd, mcpConnectCmd, mcpDisconnectCmd, mcpShowCmd, mcpTestCmd)}
