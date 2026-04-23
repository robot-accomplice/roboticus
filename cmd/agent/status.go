package agent

import (
	"fmt"
	"roboticus/cmd/internal/cmdutil"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display agent health and status",
	RunE: func(cmd *cobra.Command, args []string) error {
		health, err := cmdutil.APIGet("/api/health")
		if err != nil {
			return fmt.Errorf("agent is offline: %w", err)
		}

		// Agent name and status from /api/agent/status.
		agentName := "unknown"
		agentStatus := "unknown"
		model := ""
		agent, agentErr := cmdutil.APIGet("/api/agent/status")
		if agentErr == nil {
			if n, ok := agent["name"].(string); ok && n != "" {
				agentName = n
			}
			if s, ok := agent["status"].(string); ok && s != "" {
				agentStatus = s
			}
			if m, ok := agent["model"].(string); ok && m != "" {
				model = m
			}
		}

		fmt.Printf("Agent: %s (%s)\n", agentName, agentStatus)
		if model != "" {
			fmt.Printf("Model: %s\n", model)
		}
		if uptime, ok := health["uptime"].(string); ok && uptime != "" {
			fmt.Printf("Uptime: %s\n", uptime)
		}
		fmt.Println()

		// Sessions.
		if sessions, err := cmdutil.APIGet("/api/sessions"); err == nil {
			count := 0
			if list, ok := sessions["sessions"].([]any); ok {
				count = len(list)
			} else if c, ok := sessions["count"].(float64); ok {
				count = int(c)
			} else if c, ok := sessions["total"].(float64); ok {
				count = int(c)
			}
			fmt.Printf("Sessions: %d active\n", count)
		}

		// Skills.
		if skills, err := cmdutil.APIGet("/api/skills"); err == nil {
			enabled := 0
			total := 0
			if list, ok := skills["skills"].([]any); ok {
				total = len(list)
				for _, s := range list {
					sm, _ := s.(map[string]any)
					if e, ok := sm["enabled"].(bool); ok && e {
						enabled++
					}
				}
			}
			if total > 0 {
				fmt.Printf("Skills: %d/%d enabled\n", enabled, total)
			}
		}

		// Cron jobs.
		if cron, err := cmdutil.APIGet("/api/cron/jobs"); err == nil {
			total := 0
			failed := 0
			if jobs, ok := cron["jobs"].([]any); ok {
				total = len(jobs)
				for _, j := range jobs {
					jm, _ := j.(map[string]any)
					if s, ok := jm["status"].(string); ok && s == "failed" {
						failed++
					}
					if s, ok := jm["last_error"].(string); ok && s != "" {
						failed++
					}
				}
			}
			fmt.Printf("Cron: %d jobs (%d failed)\n", total, failed)
		}

		// Cache stats.
		if cache, err := cmdutil.APIGet("/api/stats/cache"); err == nil {
			hitRate := 0.0
			if hr, ok := cache["hit_rate"].(float64); ok {
				hitRate = hr * 100
			} else if hr, ok := cache["hit_rate_percent"].(float64); ok {
				hitRate = hr
			}
			fmt.Printf("Cache: %.1f%% hit rate\n", hitRate)
		}

		// Wallet balance.
		if wallet, err := cmdutil.APIGet("/api/wallet/balance"); err == nil {
			balance := "0.00"
			token := "USDC"
			if b, ok := wallet["balance"].(string); ok {
				balance = b
			} else if b, ok := wallet["balance"].(float64); ok {
				balance = fmt.Sprintf("%.2f", b)
			}
			if t, ok := wallet["token"].(string); ok && t != "" {
				token = t
			}
			fmt.Printf("Wallet: %s %s\n", balance, token)
		}

		fmt.Println()

		// Providers from health response.
		if providers, ok := health["providers"].([]any); ok {
			fmt.Printf("Providers: %d configured\n", len(providers))
			for _, p := range providers {
				pm, _ := p.(map[string]any)
				name, _ := pm["name"].(string)
				state := "unknown"
				if s, ok := pm["state"].(string); ok {
					state = s
				}
				fmt.Printf("  %s: %s\n", name, state)
			}
		}

		fmt.Println()

		// Channels.
		if chData, err := cmdutil.APIGet("/api/channels/status"); err == nil {
			if channels, ok := chData["channels"].([]any); ok {
				fmt.Println("Channels:")
				for _, c := range channels {
					cm, _ := c.(map[string]any)
					platform, _ := cm["platform"].(string)
					status, _ := cm["status"].(string)
					fmt.Printf("  %s: %s\n", platform, status)
				}
			}
		}

		return nil
	},
}
