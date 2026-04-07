package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage provider authentication",
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which providers have API keys configured",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/config")
		if err != nil {
			return err
		}

		providers, ok := data["providers"].(map[string]any)
		if !ok {
			fmt.Println("No providers configured.")
			return nil
		}

		fmt.Println("Provider Authentication Status:")
		for name, v := range providers {
			pm, _ := v.(map[string]any)
			keyEnv, _ := pm["api_key_env"].(string)
			status := "not configured"
			if keyEnv != "" {
				status = fmt.Sprintf("env: %s", keyEnv)
			}
			fmt.Printf("  %-20s %s\n", name, status)
		}
		return nil
	},
}

var authLoginCmd = &cobra.Command{
	Use:   "login <provider>",
	Short: "Set API key for a provider (stores in encrypted keystore)",
	Long: `Set an API key for a provider. The key is stored in the encrypted keystore.

Note: The Go implementation uses API key authentication. For providers
that support OAuth (e.g. Anthropic), obtain an API key from the provider's
dashboard and enter it here. OAuth PKCE flows are not yet supported.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]

		var apiKey string
		fmt.Printf("Enter API key for %s: ", provider)
		if _, err := fmt.Scanln(&apiKey); err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			return fmt.Errorf("API key cannot be empty")
		}

		payload, _ := json.Marshal(map[string]string{"key": apiKey})
		client := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("PUT", apiBaseURL()+"/api/providers/"+provider+"/key", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed (is roboticus running?): %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to set key: HTTP %d: %s", resp.StatusCode, string(body))
		}

		fmt.Printf("API key set for provider %q.\n", provider)
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout <provider>",
	Short: "Remove API key for a provider",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		if err := apiDelete("/api/providers/" + provider + "/key"); err != nil {
			return err
		}
		fmt.Printf("API key removed for provider %q.\n", provider)
		return nil
	},
}

func init() {
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	rootCmd.AddCommand(authCmd)
}
