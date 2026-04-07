package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Manage cron jobs",
}

var cronListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all cron jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/cron/jobs")
		if err != nil {
			return err
		}
		jobs, _ := data["jobs"].([]any)
		if len(jobs) == 0 {
			fmt.Println("No cron jobs.")
			return nil
		}
		for _, j := range jobs {
			jm, _ := j.(map[string]any)
			enabled := "enabled"
			if e, ok := jm["enabled"].(bool); ok && !e {
				enabled = "disabled"
			}
			fmt.Printf("  %v  %v  schedule=%v  %s\n",
				jm["id"], jm["name"], jm["schedule_expr"], enabled)
		}
		return nil
	},
}

var cronCreateCmd = &cobra.Command{
	Use:   "create [name] [cron-expr]",
	Short: "Create a cron job",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/cron/jobs", map[string]any{
			"name":          args[0],
			"schedule_kind": "cron",
			"schedule_expr": args[1],
			"agent_id":      "default",
			"payload_json":  "{}",
		})
		if err != nil {
			return err
		}
		fmt.Printf("Created cron job: %v\n", data["id"])
		return nil
	},
}

var cronDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a cron job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := apiDelete("/api/cron/jobs/" + args[0]); err != nil {
			return err
		}
		fmt.Println("Cron job deleted.")
		return nil
	},
}

// resolveJobID resolves a job argument to a UUID. If the argument contains
// a dash it is assumed to be a UUID already; otherwise it is treated as a
// job name and resolved via the API.
func resolveJobID(arg string) (string, error) {
	if strings.Contains(arg, "-") {
		return arg, nil
	}
	// Treat as a name — look up from the jobs list.
	data, err := apiGet("/api/cron/jobs")
	if err != nil {
		return "", fmt.Errorf("failed to list jobs for name resolution: %w", err)
	}
	jobs, _ := data["jobs"].([]any)
	for _, j := range jobs {
		jm, _ := j.(map[string]any)
		if name, _ := jm["name"].(string); name == arg {
			if id, _ := jm["id"].(string); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("no cron job found with name %q", arg)
}

var cronRunCmd = &cobra.Command{
	Use:   "run [id-or-name]",
	Short: "Trigger a cron job immediately (accepts UUID or job name)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := resolveJobID(args[0])
		if err != nil {
			return err
		}
		data, err := apiPost("/api/cron/jobs/"+id+"/run", nil)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var cronHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show recent cron run history",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/cron/runs")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var cronRecoverCmd = &cobra.Command{
	Use:   "recover",
	Short: "Recover paused or failed cron jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/cron/jobs")
		if err != nil {
			return err
		}
		jobs, _ := data["jobs"].([]any)
		recovered := 0
		for _, j := range jobs {
			jm, _ := j.(map[string]any)
			enabled, _ := jm["enabled"].(bool)
			id, _ := jm["id"].(string)
			if !enabled && id != "" {
				_, err := apiPost("/api/cron/jobs/"+id+"/run", nil)
				if err == nil {
					recovered++
				}
			}
		}
		fmt.Printf("Recovered %d paused/failed job(s)\n", recovered)
		return nil
	},
}

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage scheduled jobs (alias for cron)",
}

func init() {
	cronCmd.AddCommand(cronListCmd, cronCreateCmd, cronDeleteCmd, cronRunCmd, cronHistoryCmd, cronRecoverCmd)
	rootCmd.AddCommand(cronCmd)

	// Register schedule as an alias command with duplicated subcommands.
	scheduleCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all scheduled jobs",
			RunE:  cronListCmd.RunE,
		},
		&cobra.Command{
			Use:   "create [name] [cron-expr]",
			Short: "Create a scheduled job",
			Args:  cobra.ExactArgs(2),
			RunE:  cronCreateCmd.RunE,
		},
		&cobra.Command{
			Use:   "delete [id]",
			Short: "Delete a scheduled job",
			Args:  cobra.ExactArgs(1),
			RunE:  cronDeleteCmd.RunE,
		},
		&cobra.Command{
			Use:   "run [id-or-name]",
			Short: "Trigger a scheduled job immediately (accepts UUID or job name)",
			Args:  cobra.ExactArgs(1),
			RunE:  cronRunCmd.RunE,
		},
		&cobra.Command{
			Use:   "history",
			Short: "Show recent scheduled job run history",
			RunE:  cronHistoryCmd.RunE,
		},
		&cobra.Command{
			Use:   "recover",
			Short: "Recover paused or failed scheduled jobs",
			RunE:  cronRecoverCmd.RunE,
		},
	)
	rootCmd.AddCommand(scheduleCmd)
}
