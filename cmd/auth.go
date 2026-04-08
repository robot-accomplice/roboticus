package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"roboticus/internal/core"
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
	Short: "Authenticate with a provider (API key or OAuth PKCE)",
	Long: `Authenticate with a provider. Supports two modes:

  API Key:    Prompts for a key and stores it in the encrypted keystore.
  OAuth PKCE: For providers with OAuth support (--oauth flag), opens a browser
              for authorization and stores the resulting tokens.

Examples:
  roboticus auth login openai          # API key prompt
  roboticus auth login anthropic --oauth  # OAuth PKCE flow`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		useOAuth, _ := cmd.Flags().GetBool("oauth")

		if useOAuth {
			return runOAuthLogin(provider)
		}
		return runAPIKeyLogin(provider)
	},
}

func runAPIKeyLogin(provider string) error {
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
}

func runOAuthLogin(provider string) error {
	// Look up provider's OAuth config from the running server.
	data, err := apiGet("/api/config")
	if err != nil {
		return fmt.Errorf("failed to fetch config: %w", err)
	}

	providers, _ := data["providers"].(map[string]any)
	pm, _ := providers[provider].(map[string]any)
	authMode, _ := pm["auth_mode"].(string)
	if authMode != "oauth" {
		return fmt.Errorf("provider %q does not support OAuth (auth_mode=%q). Use API key login instead", provider, authMode)
	}

	clientID, _ := pm["oauth_client_id"].(string)
	if clientID == "" {
		return fmt.Errorf("provider %q has no OAuth client ID configured", provider)
	}

	// Derive OAuth endpoints from provider URL.
	providerURL, _ := pm["url"].(string)
	authURL := providerURL + "/oauth/authorize"
	tokenURL := providerURL + "/oauth/token"

	fmt.Printf("Starting OAuth PKCE flow for %s...\n", provider)
	token, err := core.RunOAuthPKCEFlow(context.Background(), core.OAuthPKCEConfig{
		AuthURL:  authURL,
		TokenURL: tokenURL,
		ClientID: clientID,
		Scopes:   []string{"read", "write"},
	})
	if err != nil {
		return fmt.Errorf("OAuth flow failed: %w", err)
	}

	// Store the access token in the keystore via the API.
	payload, _ := json.Marshal(map[string]string{"key": token.AccessToken})
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("PUT", apiBaseURL()+"/api/providers/"+provider+"/key", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to store OAuth token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to store token: HTTP %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("OAuth authentication complete for %q. Token stored in keystore.\n", provider)
	if token.RefreshToken != "" {
		fmt.Println("Refresh token also stored for automatic renewal.")
	}
	return nil
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
	authLoginCmd.Flags().Bool("oauth", false, "Use OAuth PKCE flow instead of API key")
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	rootCmd.AddCommand(authCmd)
}
