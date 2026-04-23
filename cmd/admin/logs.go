package admin

import (
	"bufio"
	"fmt"
	"net/http"
	"roboticus/cmd/internal/cmdutil"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream or fetch recent log entries",
	Long:  `Fetch recent log entries from the running roboticus server, optionally filtering by level or following in real time.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		lines, _ := cmd.Flags().GetInt("lines")
		level, _ := cmd.Flags().GetString("level")
		follow, _ := cmd.Flags().GetBool("follow")

		path := fmt.Sprintf("/api/logs?lines=%d", lines)
		if level != "" {
			path += "&level=" + level
		}

		if follow {
			return followLogs(path)
		}

		data, err := cmdutil.APIGet(path)
		if err != nil {
			return err
		}

		entries, ok := data["entries"].([]any)
		if !ok {
			cmdutil.PrintJSON(data)
			return nil
		}
		for _, entry := range entries {
			if line, ok := entry.(string); ok {
				fmt.Println(line)
			} else {
				em, _ := entry.(map[string]any)
				fmt.Printf("[%v] %v: %v\n", em["time"], em["level"], em["message"])
			}
		}
		return nil
	},
}

func followLogs(basePath string) error {
	url := cmdutil.APIBaseURL() + basePath + "&follow=true"
	client := &http.Client{Timeout: 0}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("connection failed (is roboticus running?): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	return scanner.Err()
}

func init() {
	logsCmd.Flags().Int("lines", 50, "number of log lines to fetch")
	logsCmd.Flags().String("level", "", "filter by log level (debug, info, warn, error)")
	logsCmd.Flags().Bool("follow", false, "follow log output in real time")
}
