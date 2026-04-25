package configcmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"roboticus/internal/core"
)

var setupCmd = &cobra.Command{
	Use:     "setup",
	Aliases: []string{"onboard"},
	Short:   "Interactive onboarding wizard",
	Long:    `Walks you through initial configuration: agent name, LLM provider, and API key.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		scanner := bufio.NewScanner(os.Stdin)

		// 1. Agent name.
		fmt.Print("Agent name [Roboticus]: ")
		agentName := "Roboticus"
		if scanner.Scan() {
			if v := strings.TrimSpace(scanner.Text()); v != "" {
				agentName = v
			}
		}

		// 2. LLM provider.
		fmt.Print("Primary LLM provider (ollama/openai/anthropic/google/moonshot/openrouter/deepseek) [ollama]: ")
		provider := "ollama"
		if scanner.Scan() {
			if v := strings.TrimSpace(strings.ToLower(scanner.Text())); v != "" {
				switch v {
				case "ollama", "openai", "anthropic", "google", "moonshot", "openrouter", "deepseek":
					provider = v
				default:
					return fmt.Errorf("unsupported provider %q — choose ollama, openai, anthropic, google, moonshot, openrouter, or deepseek", v)
				}
			}
		}

		// 3. API key (for cloud providers).
		var apiKey string
		if provider != "ollama" {
			keyName := provider + "_api_key"
			fmt.Printf("API key (will be stored in keystore entry %s): ", keyName)
			if scanner.Scan() {
				apiKey = strings.TrimSpace(scanner.Text())
			}
			if apiKey == "" {
				fmt.Fprintf(os.Stderr, "warning: no API key provided — set %s in the keystore later\n", keyName)
			}
		}

		// Write config.
		configDir := core.ConfigDir()
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			return fmt.Errorf("failed to create config dir: %w", err)
		}

		configPath := filepath.Join(configDir, "roboticus.toml")
		content := buildConfigTOML(agentName, provider, apiKey)

		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
		if apiKey != "" {
			ks, err := core.OpenKeystoreMachine()
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: API key not stored — failed to open keystore: %v\n", err)
			} else if err := ks.Set(provider+"_api_key", apiKey); err != nil {
				fmt.Fprintf(os.Stderr, "warning: API key not stored — failed to update keystore: %v\n", err)
			} else if err := ks.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: API key not stored — failed to save keystore: %v\n", err)
			}
		}

		fmt.Printf("\nConfiguration written to %s\n", configPath)
		fmt.Printf("Agent: %s | Provider: %s\n", agentName, provider)
		fmt.Println("Run 'roboticus serve' to start.")
		return nil
	},
}

func buildConfigTOML(name, provider, apiKey string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[agent]\nname = %q\n\n", name)
	sb.WriteString("[models]\n")

	switch provider {
	case "openai":
		sb.WriteString("primary = \"gpt-4o\"\n\n")
		sb.WriteString("[providers.openai]\n")
		sb.WriteString("url = \"https://api.openai.com\"\n")
		sb.WriteString("tier = \"flagship\"\n")
		sb.WriteString("format = \"openai\"\n")
		sb.WriteString("api_key_ref = \"openai_api_key\"\n")
	case "anthropic":
		sb.WriteString("primary = \"claude-sonnet-4-20250514\"\n\n")
		sb.WriteString("[providers.anthropic]\n")
		sb.WriteString("url = \"https://api.anthropic.com\"\n")
		sb.WriteString("tier = \"flagship\"\n")
		sb.WriteString("format = \"anthropic\"\n")
		sb.WriteString("api_key_ref = \"anthropic_api_key\"\n")
	case "google":
		sb.WriteString("primary = \"google/gemini-3.1-pro-preview\"\n\n")
		sb.WriteString("[providers.google]\n")
		sb.WriteString("url = \"https://generativelanguage.googleapis.com\"\n")
		sb.WriteString("tier = \"T3\"\n")
		sb.WriteString("format = \"google\"\n")
		sb.WriteString("api_key_ref = \"google_api_key\"\n")
	case "moonshot":
		sb.WriteString("primary = \"moonshot/kimi-k2.5\"\n\n")
		sb.WriteString("[providers.moonshot]\n")
		sb.WriteString("url = \"https://api.moonshot.ai\"\n")
		sb.WriteString("tier = \"T2\"\n")
		sb.WriteString("format = \"openai\"\n")
		sb.WriteString("chat_path = \"/v1/chat/completions\"\n")
		sb.WriteString("api_key_ref = \"moonshot_api_key\"\n")
	case "openrouter":
		sb.WriteString("primary = \"openrouter/google/gemini-3.1-pro-preview\"\n\n")
		sb.WriteString("[providers.openrouter]\n")
		sb.WriteString("url = \"https://openrouter.ai/api\"\n")
		sb.WriteString("tier = \"T2\"\n")
		sb.WriteString("format = \"openai\"\n")
		sb.WriteString("chat_path = \"/v1/chat/completions\"\n")
		sb.WriteString("auth_header = \"Authorization\"\n")
		sb.WriteString("api_key_ref = \"openrouter_api_key\"\n")
	case "deepseek":
		sb.WriteString("# Provider/model drift belongs in providers.toml; pin the model you intend to route here.\n")
		sb.WriteString("primary = \"deepseek/deepseek-v4-flash\"\n\n")
		sb.WriteString("[providers.deepseek]\n")
		sb.WriteString("url = \"https://api.deepseek.com\"\n")
		sb.WriteString("tier = \"T3\"\n")
		sb.WriteString("format = \"openai\"\n")
		sb.WriteString("chat_path = \"/chat/completions\"\n")
		sb.WriteString("api_key_ref = \"deepseek_api_key\"\n")
	default:
		sb.WriteString("primary = \"llama3.2\"\n\n")
		sb.WriteString("[providers.ollama]\n")
		sb.WriteString("url = \"http://localhost:11434\"\n")
		sb.WriteString("tier = \"local\"\n")
		sb.WriteString("format = \"ollama\"\n")
		sb.WriteString("is_local = true\n")
	}

	if apiKey != "" {
		fmt.Fprintf(&sb, "\n# API key provided during setup; it is stored in keystore entry %s_api_key.\n", provider)
	}

	return sb.String()
}

// setupPersonalityCmd is the 5-question quick personality setup.
// Matches Rust's run_quick_personality_setup().
var setupPersonalityCmd = &cobra.Command{
	Use:   "personality",
	Short: "Quick 5-question personality setup (generates OS.toml + FIRMWARE.toml)",
	RunE: func(cmd *cobra.Command, args []string) error {
		scanner := bufio.NewScanner(os.Stdin)

		cfgVal := core.DefaultConfig()
		workspaceDir := cfgVal.Agent.Workspace
		if workspaceDir == "" {
			workspaceDir = filepath.Join(core.ConfigDir(), "workspace")
		}

		// Q1: Agent name.
		fmt.Print("  Agent name [Roboticus]: ")
		agentName := "Roboticus"
		if scanner.Scan() {
			if v := strings.TrimSpace(scanner.Text()); v != "" {
				agentName = v
			}
		}

		// Q2: Formality.
		fmt.Print("  Communication style (formal/balanced/casual) [balanced]: ")
		formality := "balanced"
		if scanner.Scan() {
			if v := strings.TrimSpace(strings.ToLower(scanner.Text())); v != "" {
				switch v {
				case "formal", "balanced", "casual":
					formality = v
				default:
					fmt.Fprintf(os.Stderr, "  (using 'balanced')\n")
				}
			}
		}

		// Q3: Proactiveness.
		fmt.Print("  Proactiveness (wait/suggest/initiative) [suggest]: ")
		proactiveness := "suggest"
		if scanner.Scan() {
			if v := strings.TrimSpace(strings.ToLower(scanner.Text())); v != "" {
				switch v {
				case "wait", "suggest", "initiative":
					proactiveness = v
				default:
					fmt.Fprintf(os.Stderr, "  (using 'suggest')\n")
				}
			}
		}

		// Q4: Domain.
		fmt.Print("  Primary domain (general/developer/business/creative/research) [general]: ")
		domain := "general"
		if scanner.Scan() {
			if v := strings.TrimSpace(strings.ToLower(scanner.Text())); v != "" {
				switch v {
				case "general", "developer", "business", "creative", "research":
					domain = v
				default:
					fmt.Fprintf(os.Stderr, "  (using 'general')\n")
				}
			}
		}

		// Q5: Boundaries.
		fmt.Print("  Hard boundaries (topics/actions off-limits, or Enter to skip): ")
		var boundaries string
		if scanner.Scan() {
			boundaries = strings.TrimSpace(scanner.Text())
		}

		// Generate OS.toml.
		osContent := core.GenerateOsTOML(agentName, formality, proactiveness, domain)

		// Generate FIRMWARE.toml.
		fwContent := core.GenerateFirmwareTOML(boundaries)

		// Write to workspace.
		if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
			return fmt.Errorf("failed to create workspace dir: %w", err)
		}
		osPath := filepath.Join(workspaceDir, "OS.toml")
		if err := os.WriteFile(osPath, []byte(osContent), 0o644); err != nil {
			return fmt.Errorf("failed to write OS.toml: %w", err)
		}
		fwPath := filepath.Join(workspaceDir, "FIRMWARE.toml")
		if err := os.WriteFile(fwPath, []byte(fwContent), 0o644); err != nil {
			return fmt.Errorf("failed to write FIRMWARE.toml: %w", err)
		}

		fmt.Printf("\n  Personality configured for %s\n", agentName)
		fmt.Printf("  OS.toml:       %s\n", osPath)
		fmt.Printf("  FIRMWARE.toml: %s\n", fwPath)
		fmt.Println("\n  Run 'roboticus serve' to start with this personality.")
		return nil
	},
}

func init() {
	setupCmd.AddCommand(setupPersonalityCmd)
}
