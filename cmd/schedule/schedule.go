package schedule

import (
	"fmt"
	"roboticus/cmd/internal/cmdutil"
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
		data, err := cmdutil.APIGet("/api/cron/jobs")
		if err != nil {
			return err
		}
		jobs, ok := data["jobs"].([]any)
		if !ok || len(jobs) == 0 {
			fmt.Println("No scheduled jobs.")
			return nil
		}

		fmt.Printf("%-8s %-20s %-10s %-20s %s\n", "STATUS", "NAME", "SCHEDULE", "LAST RUN", "NEXT RUN")
		fmt.Println("──────── ──────────────────── ────────── ──────────────────── ────────────────────")
		for _, j := range jobs {
			jm, _ := j.(map[string]any)
			name, _ := jm["name"].(string)
			enabled, _ := jm["enabled"].(bool)
			expr, _ := jm["schedule_expr"].(string)
			lastRun, _ := jm["last_run_at"].(string)
			nextRun, _ := jm["next_run_at"].(string)
			lastErr, _ := jm["last_error"].(string)

			status := "enabled"
			if !enabled {
				status = "paused"
			}
			if lastErr != "" {
				status = "error"
			}

			if lastRun == "" {
				lastRun = "never"
			}
			if nextRun == "" {
				nextRun = "—"
			}

			// Truncate timestamps.
			if len(lastRun) > 19 {
				lastRun = lastRun[:19]
			}
			if len(nextRun) > 19 {
				nextRun = nextRun[:19]
			}

			fmt.Printf("%-8s %-20s %-10s %-20s %s\n", status, name, expr, lastRun, nextRun)
			if lastErr != "" {
				fmt.Printf("         └─ error: %s\n", lastErr)
			}
		}
		return nil
	},
}

var cronCreateCmd = &cobra.Command{
	Use:   "create [name] [cron-expr]",
	Short: "Create a cron job",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIPost("/api/cron/jobs", map[string]any{
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
		if err := cmdutil.APIDelete("/api/cron/jobs/" + args[0]); err != nil {
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
	data, err := cmdutil.APIGet("/api/cron/jobs")
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
		data, err := cmdutil.APIPost("/api/cron/jobs/"+id+"/run", nil)
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var cronHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show recent cron run history",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/cron/runs")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var cronRecoverCmd = &cobra.Command{
	Use:   "recover",
	Short: "Recover paused or failed cron jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/cron/jobs")
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
				_, err := cmdutil.APIPost("/api/cron/jobs/"+id+"/run", nil)
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
}
