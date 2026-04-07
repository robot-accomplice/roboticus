package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var channelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "Manage channel adapters and dead-letter queue",
}

var channelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channel adapter status",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/channels/status")
		if err != nil {
			return err
		}
		channels, ok := data["channels"].([]any)
		if !ok {
			printJSON(data)
			return nil
		}
		fmt.Println("Channels:")
		for _, c := range channels {
			cm, _ := c.(map[string]any)
			platform, _ := cm["platform"].(string)
			status, _ := cm["status"].(string)
			fmt.Printf("  %-15s %s\n", platform, status)
		}
		return nil
	},
}

var channelsTestCmd = &cobra.Command{
	Use:   "test [platform]",
	Short: "Send a test message through a channel adapter",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/channels/"+args[0]+"/test", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Test message sent through %q channel.\n", args[0])
		printJSON(data)
		return nil
	},
}

var channelsDeadLetterCmd = &cobra.Command{
	Use:   "dead-letter",
	Short: "View dead-letter queue entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/channels/dead-letter")
		if err != nil {
			return err
		}
		entries, ok := data["entries"].([]any)
		if !ok {
			printJSON(data)
			return nil
		}
		if len(entries) == 0 {
			fmt.Println("Dead-letter queue is empty.")
			return nil
		}
		for _, e := range entries {
			em, _ := e.(map[string]any)
			fmt.Printf("  [%v] platform=%v  error=%v\n",
				em["timestamp"], em["platform"], em["error"])
		}
		return nil
	},
}

var channelsReplayCmd = &cobra.Command{
	Use:   "replay [id]",
	Short: "Replay a dead-letter queue entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/channels/dead-letter/"+args[0]+"/replay", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Replayed dead-letter entry %s\n", args[0])
		printJSON(data)
		return nil
	},
}

var channelsGuideCmd = &cobra.Command{
	Use:   "guide <platform>",
	Short: "Show configuration guide for a channel adapter",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		guides := map[string]string{
			"telegram": `[channels]
TelegramTokenEnv = "TELEGRAM_BOT_TOKEN"
TelegramAllowedChatIDs = []

# Set your bot token:
#   roboticus keystore set telegram_bot_token YOUR_TOKEN`,

			"discord": `[channels]
DiscordTokenEnv = "DISCORD_BOT_TOKEN"
DiscordAllowedGuildIDs = []

# Set your bot token:
#   roboticus keystore set discord_bot_token YOUR_TOKEN`,

			"whatsapp": `[channels]
WhatsAppTokenEnv = "WHATSAPP_API_TOKEN"
WhatsAppPhoneNumberID = ""
WhatsAppVerifyToken = ""

# Set your API token:
#   roboticus keystore set whatsapp_api_token YOUR_TOKEN`,

			"signal": `[channels]
SignalCLIPath = "/usr/local/bin/signal-cli"
SignalPhoneNumber = "+1234567890"

# Register your Signal number:
#   signal-cli -u +1234567890 register
#   signal-cli -u +1234567890 verify CODE`,

			"email": `[channels]
EmailIMAPHost = "imap.example.com:993"
EmailSMTPHost = "smtp.example.com:587"
EmailAddress = "bot@example.com"
EmailPasswordEnv = "EMAIL_BOT_PASSWORD"

# Set your email password:
#   roboticus keystore set email_bot_password YOUR_PASSWORD`,

			"matrix": `[channels]
MatrixHomeserver = "https://matrix.example.com"
MatrixUserID = "@bot:example.com"
MatrixAccessTokenEnv = "MATRIX_ACCESS_TOKEN"

# Set your access token:
#   roboticus keystore set matrix_access_token YOUR_TOKEN`,
		}

		platform := strings.ToLower(args[0])
		guide, ok := guides[platform]
		if !ok {
			available := make([]string, 0, len(guides))
			for k := range guides {
				available = append(available, k)
			}
			return fmt.Errorf("no guide for platform %q (available: %s)", platform, strings.Join(available, ", "))
		}
		fmt.Printf("Configuration guide for %s:\n\n%s\n", platform, guide)
		return nil
	},
}

func init() {
	channelsCmd.AddCommand(channelsListCmd, channelsTestCmd, channelsDeadLetterCmd, channelsReplayCmd, channelsGuideCmd)
	rootCmd.AddCommand(channelsCmd)
}
