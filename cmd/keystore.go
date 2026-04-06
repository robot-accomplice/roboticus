package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"roboticus/internal/core"
)

var keystoreCmd = &cobra.Command{
	Use:   "keystore",
	Short: "Manage the encrypted keystore",
}

var keystoreStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show keystore lock/unlock status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Check if any providers have API key env vars that resolve.
		hasKeys := false
		for name, prov := range cfg.Providers {
			if prov.APIKeyEnv != "" {
				hasKeys = true
				fmt.Printf("  %-20s key env: %s\n", name, prov.APIKeyEnv)
			}
		}

		if hasKeys {
			fmt.Println("\nKeystore status: accessible (provider keys configured)")
		} else {
			fmt.Println("\nKeystore status: no keys configured")
		}

		return nil
	},
}

var keystoreListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored key names (not values)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("Stored Key Names:")
		count := 0
		for name, prov := range cfg.Providers {
			if prov.APIKeyEnv != "" {
				fmt.Printf("  %s (env: %s)\n", name, prov.APIKeyEnv)
				count++
			}
		}

		if count == 0 {
			fmt.Println("  (none)")
		}

		return nil
	},
}

var keystoreSetCmd = &cobra.Command{
	Use:   "set <KEY> [VALUE]",
	Short: "Set or update a provider API key",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		var value string
		if len(args) > 1 {
			value = args[1]
		} else {
			fmt.Printf("Enter value for %s: ", key)
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				value = strings.TrimSpace(scanner.Text())
			}
			if value == "" {
				return fmt.Errorf("no value provided")
			}
		}

		data, err := apiPut("/api/providers/"+key+"/key", map[string]any{
			"key": value,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Key %q set successfully.\n", key)
		if data != nil {
			printJSON(data)
		}
		return nil
	},
}

var keystoreGetCmd = &cobra.Command{
	Use:   "get <KEY>",
	Short: "Get the status of a specific key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		data, err := apiGet("/api/keystore/status")
		if err != nil {
			return err
		}

		// Look for the key in the status response.
		if keys, ok := data["keys"].(map[string]any); ok {
			if v, found := keys[key]; found {
				fmt.Printf("%s: %v\n", key, v)
				return nil
			}
		}
		// Also check providers map if present.
		if providers, ok := data["providers"].(map[string]any); ok {
			if v, found := providers[key]; found {
				fmt.Printf("%s: %v\n", key, v)
				return nil
			}
		}

		fmt.Printf("%s: not found\n", key)
		return nil
	},
}

var keystoreRemoveCmd = &cobra.Command{
	Use:   "remove <KEY>",
	Short: "Remove a provider API key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		if err := apiDelete("/api/providers/" + key + "/key"); err != nil {
			return err
		}
		fmt.Printf("Key %q removed.\n", key)
		return nil
	},
}

var keystoreImportCmd = &cobra.Command{
	Use:   "import <PATH>",
	Short: "Import keys from a JSON file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %q: %w", path, err)
		}

		var entries map[string]string
		if err := json.Unmarshal(raw, &entries); err != nil {
			return fmt.Errorf("invalid JSON in %q: %w", path, err)
		}

		for key, value := range entries {
			_, err := apiPut("/api/providers/"+key+"/key", map[string]any{
				"key": value,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  failed to set %q: %v\n", key, err)
				continue
			}
			fmt.Printf("  set %q\n", key)
		}

		fmt.Printf("Import complete (%d keys).\n", len(entries))
		return nil
	},
}

var keystoreRekeyCmd = &cobra.Command{
	Use:   "rekey",
	Short: "Re-encrypt the keystore with a new master key",
	RunE: func(cmd *cobra.Command, args []string) error {
		currentPass := strings.TrimSpace(os.Getenv("ROBOTICUS_MASTER_KEY"))
		newPass := strings.TrimSpace(os.Getenv("ROBOTICUS_NEW_MASTER_KEY"))

		if currentPass == "" {
			fmt.Print("Current passphrase: ")
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				currentPass = strings.TrimSpace(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("failed to read current passphrase: %w", err)
			}
		}
		if currentPass == "" {
			return fmt.Errorf("current passphrase cannot be empty")
		}

		if newPass == "" {
			fmt.Print("New passphrase: ")
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				newPass = strings.TrimSpace(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("failed to read new passphrase: %w", err)
			}
		}
		if newPass == "" {
			return fmt.Errorf("new passphrase cannot be empty")
		}

		confirmPass := strings.TrimSpace(os.Getenv("ROBOTICUS_NEW_MASTER_KEY_CONFIRM"))
		if confirmPass == "" {
			fmt.Print("Confirm new passphrase: ")
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				confirmPass = strings.TrimSpace(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("failed to confirm new passphrase: %w", err)
			}
		}
		if newPass != confirmPass {
			return fmt.Errorf("new passphrases do not match")
		}

		ks, err := core.OpenKeystore(core.KeystoreConfig{
			Path:       filepath.Join(core.ConfigDir(), "keystore.enc"),
			Passphrase: currentPass,
		})
		if err != nil {
			return fmt.Errorf("failed to open keystore: %w", err)
		}
		if err := ks.Rekey(newPass); err != nil {
			return fmt.Errorf("failed to rekey keystore: %w", err)
		}
		fmt.Println("Keystore re-encrypted with new passphrase.")
		return nil
	},
}

func init() {
	keystoreCmd.AddCommand(
		keystoreStatusCmd,
		keystoreListCmd,
		keystoreSetCmd,
		keystoreGetCmd,
		keystoreRemoveCmd,
		keystoreImportCmd,
		keystoreRekeyCmd,
	)
	rootCmd.AddCommand(keystoreCmd)
}
