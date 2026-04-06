package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage agent sessions",
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/sessions")
		if err != nil {
			return err
		}
		sessions, _ := data["sessions"].([]any)
		if len(sessions) == 0 {
			fmt.Println("No sessions.")
			return nil
		}
		for _, s := range sessions {
			sm, _ := s.(map[string]any)
			fmt.Printf("  %v  agent=%v  scope=%v  nickname=%v\n",
				sm["id"], sm["agent_id"], sm["scope_key"], sm["nickname"])
		}
		return nil
	},
}

var sessionsShowCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show session details and messages",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/sessions/" + args[0])
		if err != nil {
			return err
		}
		printJSON(data)

		msgs, err := apiGet("/api/sessions/" + args[0] + "/messages")
		if err == nil {
			if messages, ok := msgs["messages"].([]any); ok {
				fmt.Printf("\n--- %d messages ---\n", len(messages))
				for _, m := range messages {
					mm, _ := m.(map[string]any)
					fmt.Printf("[%v] %v\n", mm["role"], truncateStr(fmt.Sprintf("%v", mm["content"]), 120))
				}
			}
		}
		return nil
	},
}

var sessionsDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := apiDelete("/api/sessions/" + args[0]); err != nil {
			return err
		}
		fmt.Println("Session deleted.")
		return nil
	},
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

var sessionsExportCmd = &cobra.Command{
	Use:   "export [id]",
	Short: "Export session messages as JSON or Markdown",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		sessionID := args[0]

		msgs, err := apiGet("/api/sessions/" + sessionID + "/messages")
		if err != nil {
			return err
		}

		if format == "markdown" {
			return exportMarkdown(sessionID, msgs)
		}
		printJSON(msgs)
		return nil
	},
}

func exportMarkdown(sessionID string, data map[string]any) error {
	fmt.Printf("# Session %s\n\n", sessionID)
	messages, ok := data["messages"].([]any)
	if !ok {
		fmt.Println("No messages.")
		return nil
	}
	for _, m := range messages {
		mm, _ := m.(map[string]any)
		role, _ := mm["role"].(string)
		content := fmt.Sprintf("%v", mm["content"])
		fmt.Printf("## %s\n\n%s\n\n", role, content)
	}
	return nil
}

var sessionsCreateCmd = &cobra.Command{
	Use:   "create [agent-id]",
	Short: "Create a new session for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/sessions", map[string]any{
			"agent_id": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Printf("Session created: %v\n", data["id"])
		return nil
	},
}

var sessionsBackfillNicknamesCmd = &cobra.Command{
	Use:   "backfill-nicknames",
	Short: "Generate nicknames for sessions that lack them",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/sessions/backfill-nicknames", nil)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

func init() {
	sessionsExportCmd.Flags().String("format", "json", "output format (json/markdown)")
	sessionsCmd.AddCommand(sessionsListCmd, sessionsShowCmd, sessionsDeleteCmd, sessionsExportCmd,
		sessionsCreateCmd, sessionsBackfillNicknamesCmd)
	rootCmd.AddCommand(sessionsCmd)
}
